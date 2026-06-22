package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/pathfinder"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// registerConsoleTools registers the persistent device console MCP tools.
//
// These four tools implement the MCP-driven device console:
//   - ndcli.device.console_open   — establish a persistent exec-stream session
//   - ndcli.device.console_exec   — run a command on an open session
//   - ndcli.device.console_close  — tear down a session
//   - ndcli.device.console_list   — list all open sessions in this MCP process
//
// The console tools live in the same ndcli.device.* namespace but operate on
// an in-memory session store (ConsoleSessionManager on the Server). They share
// the same connect flow as the interactive `device connect` CLI command but do
// NOT start an interactive PTY: instead they open the agent's exec stream and
// hold it open across multiple tool calls.
func (s *Server) registerConsoleTools() {
	// ndcli.device.console_open
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.device.console_open",
		Description: "Open a persistent console session to a device. " +
			"The session stays alive in the MCP server process until explicitly closed or idle for 30 minutes. " +
			"Returns a session_id that must be supplied to console_exec and console_close.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":    organizationProperty(),
				"device":          stringProperty("Device name to open a console session to"),
				"connect_timeout": intProperty("Seconds to wait for the device to come online (default 300)", 300),
			},
			"required": []string{"device"},
		},
	}, s.handleConsoleOpen)

	// ndcli.device.console_exec
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.device.console_exec",
		Description: "Run a shell command on an open console session. " +
			"Commands are serialised (one at a time per session). " +
			"Returns structured stdout, stderr, exit_code, and truncated flag. " +
			"Set binary=true to receive stdout/stderr as standard base64 (RFC 4648 StdEncoding) " +
			"strings with an encoding='base64' field added to the response; " +
			"use this when the command may produce non-UTF-8 output (e.g. binary files, raw device data).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id":      stringProperty("Session ID returned by console_open"),
				"command":         stringProperty("Shell command to execute on the device"),
				"timeout_seconds": intProperty("Per-command timeout in seconds (default 60, max 3600)", 60),
				"binary":          boolProperty("Return stdout/stderr as standard base64 (StdEncoding) strings and add encoding='base64' to the response. Use when the command may produce non-UTF-8 output."),
			},
			"required": []string{"session_id", "command"},
		},
	}, s.handleConsoleExec)

	// ndcli.device.console_close
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.console_close",
		Description: "Close an open console session and release the relay connection. Idempotent.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": stringProperty("Session ID to close"),
			},
			"required": []string{"session_id"},
		},
	}, s.handleConsoleClose)

	// ndcli.device.console_list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.console_list",
		Description: "List all open console sessions in this MCP server process.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleConsoleList)
}

// consoleOpenInput is the parsed input for console_open.
type consoleOpenInput struct {
	Organization   string `json:"organization,omitempty"`
	Device         string `json:"device"`
	ConnectTimeout int    `json:"connect_timeout,omitempty"`
}

// consoleExecInput is the parsed input for console_exec.
type consoleExecInput struct {
	SessionID      string `json:"session_id"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Binary         bool   `json:"binary,omitempty"`
}

// consoleCloseInput is the parsed input for console_close.
type consoleCloseInput struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleConsoleOpen(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[consoleOpenInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	connectTimeout := time.Duration(input.ConnectTimeout) * time.Second
	if input.ConnectTimeout <= 0 {
		connectTimeout = 5 * time.Minute
	}

	// Run the control-plane connect flow (POST + poll) to get the pathfinder session key.
	// Use a slightly larger context timeout to allow the full poll window to complete.
	apiCtx, cancel := context.WithTimeout(ctx, connectTimeout+30*time.Second)
	defer cancel()

	connResult, connErr := s.svc.ConsoleConnect(apiCtx, org, input.Device, connectTimeout)
	if connErr != nil {
		return s.errorResult(connErr)
	}

	// Authoritative read-only gate. NDManager derives read_only from the
	// caller's role and returns it in the connect-status response. When it is
	// explicitly true, terminal/console exec is not permitted for this session,
	// so fail console_open EARLY with a clear, self-explanatory message — before
	// opening any exec stream. A nil flag (older NDManager that omits the field)
	// means "unknown": proceed as before and rely on the agent's server-side
	// enforcement. The agent block remains the authoritative guarantee; this is
	// the clarity layer.
	if connResult.ReadOnly != nil && *connResult.ReadOnly {
		return s.errorResult(&ToolError{
			Code:    "READ_ONLY_SESSION",
			Message: pathfinder.ErrReadOnlySession.Message,
		})
	}

	// Dial the Pathfinder relay (non-blocking; opens exec stream, no PTY).
	pfClient, pfErr := pathfinder.NewClient(pathfinder.ClientConfig{
		SessionID:       connResult.PathfinderSession,
		WebAdminEnabled: false,
	})
	if pfErr != nil {
		return s.errorResult(&service.Error{
			Code:    service.CodeAPIError,
			Message: "pathfinder client init failed: " + pfErr.Error(),
			Err:     pfErr,
		})
	}

	handle, pfErr := pfClient.ConnectExec()
	if pfErr != nil {
		return s.errorResult(&service.Error{
			Code:    service.CodeAPIError,
			Message: "pathfinder connect failed: " + pfErr.Error(),
			Err:     pfErr,
		})
	}

	sessionID, genErr := newConsoleSessionID()
	if genErr != nil {
		handle.Close()
		return s.errorResult(&service.Error{
			Code:    "INTERNAL",
			Message: "failed to generate session ID: " + genErr.Error(),
			Err:     genErr,
		})
	}

	now := time.Now()
	sess := &consoleSession{
		sessionID:    sessionID,
		org:          org,
		device:       input.Device,
		openedAt:     now,
		lastActivity: now,
		handle:       handle,
	}
	s.consoleSessions.Add(sess)

	expiresAt := now.Add(consoleSessionIdleTimeout)

	return s.successResult(map[string]interface{}{
		"session_id": sessionID,
		"device":     input.Device,
		"org":        org,
		"expires_at": expiresAt.Format(time.RFC3339),
	}, fmt.Sprintf("Console session opened for device '%s'", input.Device))
}

func (s *Server) handleConsoleExec(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[consoleExecInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.SessionID == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "session_id is required"})
	}
	if input.Command == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "command is required"})
	}

	sess, lookupErr := s.consoleSessions.Get(input.SessionID)
	if lookupErr != nil {
		return s.errorResult(lookupErr)
	}

	// Serialise commands within the session — one in-flight at a time.
	sess.mu.Lock()
	defer sess.mu.Unlock()

	execResult, execErr := s.svc.ExecOnHandle(ctx, sess.handle, service.ConsoleExecOpts{
		Command:        input.Command,
		TimeoutSeconds: input.TimeoutSeconds,
	})
	if execErr != nil {
		return s.errorResult(execErr)
	}

	sess.updateActivity()

	if input.Binary {
		return s.successResult(map[string]interface{}{
			"stdout":    base64.StdEncoding.EncodeToString([]byte(execResult.Stdout)),
			"stderr":    base64.StdEncoding.EncodeToString([]byte(execResult.Stderr)),
			"exit_code": execResult.ExitCode,
			"truncated": execResult.Truncated,
			"encoding":  "base64",
		}, "")
	}
	return s.successResult(map[string]interface{}{
		"stdout":    execResult.Stdout,
		"stderr":    execResult.Stderr,
		"exit_code": execResult.ExitCode,
		"truncated": execResult.Truncated,
	}, "")
}

func (s *Server) handleConsoleClose(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[consoleCloseInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.SessionID == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "session_id is required"})
	}

	// Idempotent: Remove returns false if not found, which is fine — we always
	// return closed:true so callers don't need to distinguish.
	removed := s.consoleSessions.Remove(input.SessionID)

	msg := fmt.Sprintf("Console session '%s' closed", input.SessionID)
	if !removed {
		msg = fmt.Sprintf("Console session '%s' was already closed or not found", input.SessionID)
	}

	return s.successResult(map[string]interface{}{
		"session_id": input.SessionID,
		"closed":     true,
	}, msg)
}

func (s *Server) handleConsoleList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessions := s.consoleSessions.List()

	list := make([]map[string]interface{}, 0, len(sessions))
	for _, sess := range sessions {
		list = append(list, map[string]interface{}{
			"session_id":    sess.sessionID,
			"device":        sess.device,
			"org":           sess.org,
			"opened_at":     sess.openedAt.Format(time.RFC3339),
			"last_activity": sess.lastActivity.Format(time.RFC3339),
		})
	}

	return s.successResult(map[string]interface{}{
		"sessions": list,
		"total":    len(list),
	}, "")
}

// newConsoleSessionID generates a random UUID v4 string for session identifiers.
func newConsoleSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
