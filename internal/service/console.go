package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

// ConsoleConnectResult is the resolved pathfinder session ready to dial.
type ConsoleConnectResult struct {
	// PathfinderSession is the session key for the relay, extracted from the
	// completed CONNECT task payload.
	PathfinderSession string
	// DeviceName is the resolved device name, echoed for the caller's records.
	DeviceName string
}

// ConsoleConnect runs the control-plane connect flow for a device:
//  1. POST /api/v1/organizations/{org}/devices/{device}/connect  → get task ID
//  2. Poll GET /api/v1/tasks/{task}/connect-status until COMPLETED or terminal
//  3. Parse ConnectPayload and return PathfinderSession
//
// connectTimeout controls the overall poll window (default 5m if ≤0).
// The returned result contains the pathfinder_session key; callers use it
// to dial via pathfinder.NewClient + ConnectExec.
func (s *Service) ConsoleConnect(ctx context.Context, org, deviceName string, connectTimeout time.Duration) (*ConsoleConnectResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}

	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Minute
	}

	// Step 1: initiate connect
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect", org, deviceName), nil)
	if err != nil {
		return nil, wrapAPI("initiate connect: %v", err)
	}

	var initResp models.ConnectInitResponse
	if err := api.ParseResponse(resp, &initResp); err != nil {
		return nil, wrapAPI("parse connect init response: %v", err)
	}
	if initResp.Task == "" {
		return nil, &Error{Code: CodeAPIError, Message: "connect response missing task ID"}
	}

	// Step 2: poll for completion
	pollInterval := 3 * time.Second
	deadline := time.Now().Add(connectTimeout)

	for {
		if time.Now().After(deadline) {
			return nil, &Error{
				Code:    CodeAPIError,
				Message: fmt.Sprintf("connect timeout after %s", connectTimeout),
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		statusResp, pollErr := s.api.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s/connect-status", initResp.Task), nil)
		if pollErr != nil {
			return nil, wrapAPI("poll connect status: %v", pollErr)
		}

		var status models.ConnectStatusResponse
		if err := api.ParseResponse(statusResp, &status); err != nil {
			return nil, wrapAPI("parse connect status: %v", err)
		}

		switch status.Status {
		case models.TaskStatusCompleted:
			if status.Payload == "" {
				return nil, &Error{Code: CodeAPIError, Message: "connect completed but payload is empty"}
			}
			var payload models.ConnectPayload
			if err := json.Unmarshal([]byte(status.Payload), &payload); err != nil {
				return nil, &Error{
					Code:    CodeAPIError,
					Message: fmt.Sprintf("failed to parse connect payload: %v", err),
					Err:     err,
				}
			}
			if payload.PathfinderSession == "" {
				return nil, &Error{Code: CodeAPIError, Message: "connect payload has no pathfinder_session"}
			}
			return &ConsoleConnectResult{
				PathfinderSession: payload.PathfinderSession,
				DeviceName:        deviceName,
			}, nil

		case models.TaskStatusFailed:
			msg := status.Message
			if msg == "" {
				msg = "unknown error"
			}
			return nil, &Error{Code: CodeAPIError, Message: "connect task failed: " + msg}

		case models.TaskStatusCancelled:
			return nil, &Error{Code: CodeAPIError, Message: "connect task was cancelled"}

		case models.TaskStatusExpired:
			return nil, &Error{Code: CodeAPIError, Message: "connect task expired"}
		}
		// PENDING / IN_PROGRESS: continue polling
	}
}

// ConsoleExecOpts controls a single command execution over an open console session.
type ConsoleExecOpts struct {
	Command        string
	TimeoutSeconds int
}

// ConsoleExecResult is the structured output of a single exec command.
type ConsoleExecResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

// ExecOnHandle runs a single command on an already-established ExecHandle.
// This is a thin service-layer wrapper so tests can verify flag handling
// without needing a live pathfinder connection.
func (s *Service) ExecOnHandle(ctx context.Context, handle *pathfinder.ExecHandle, opts ConsoleExecOpts) (*ConsoleExecResult, error) {
	if opts.Command == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "command is required"}
	}

	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if opts.TimeoutSeconds <= 0 {
		timeout = 60 * time.Second
	}

	stdout, stderr, exitCode, truncated, err := handle.Exec(ctx, opts.Command, timeout)
	if err != nil {
		return nil, &Error{
			Code:    CodeAPIError,
			Message: fmt.Sprintf("exec failed: %v", err),
			Err:     err,
		}
	}

	return &ConsoleExecResult{
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  exitCode,
		Truncated: truncated,
	}, nil
}
