package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP parity for `ndcli device health` — see CLAUDE.md "MCP-parity policy".
// Read-only and cheap, so no confirm gate. (The org-level dashboard roll-up
// is no longer an MCP tool: it moved into the standalone `netdefense` TUI.)

type deviceHealthInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
}

func (s *Server) registerDeviceHealthTool() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.health",
		Description: "Per-device drill-down with the full agent telemetry snapshot — uptime, load, mem/swap, disks per mountpoint, service running state, pending OS/plugin updates, certificate expiries. No history; this is the instant snapshot.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name (defaults to configured org)"),
				"device":       stringProperty("Device name (exact)"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceHealth)
}

func (s *Server) handleDeviceHealth(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[deviceHealthInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.DeviceHealth(apiCtx, org, input.Device)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(result, "")
}
