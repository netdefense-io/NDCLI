package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	// ReadOnly is the authoritative read-only flag from the connect-status
	// response, derived by NDManager from the caller's role. nil when an older
	// NDManager omits the field — callers then proceed as before and rely on
	// the agent's server-side enforcement.
	ReadOnly *bool
}

// consoleTransientRetries is how many waits in a row ConsoleConnect spends on
// a control-plane refusal it can retry before it returns the error.
const consoleTransientRetries = 3

// maxConsoleRetryWait is the longest Retry-After ConsoleConnect waits out; a
// longer one is returned as is.
const maxConsoleRetryWait = 30 * time.Second

// consoleWait pauses for d or until ctx ends. A var so tests can record the
// waits instead of sleeping.
var consoleWait = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// consoleRetryWait says how long to wait before trying again after err, the
// attempt-th failure in a row, or ok=false when err is not retried. A 429 is
// always retried. A 500/502/503/504 or a transport error is retried only for a
// request that can be repeated safely: a POST /connect that got one may have
// created the CONNECT task already. A context error, which a per-request
// client timeout also is, is never retried.
func consoleRetryWait(err error, attempt int, repeatable bool) (wait time.Duration, ok bool) {
	if attempt >= consoleTransientRetries || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, false
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		// Only a request that failed in transit is retried. A failed token
		// refresh never is: the refresh token rotates, and repeating a refresh
		// whose answer was lost could get the session revoked.
		if !repeatable || !api.IsNetworkError(err) {
			return 0, false
		}
		return time.Second << attempt, true
	}
	switch apiErr.StatusCode {
	case http.StatusTooManyRequests:
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		if !repeatable {
			return 0, false
		}
	default:
		return 0, false
	}
	if apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter, apiErr.RetryAfter <= maxConsoleRetryWait
	}
	return time.Second << attempt, true
}

// isAPIError reports whether err is a control-plane answer (an HTTP error
// status) rather than a transport or decode failure.
func isAPIError(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr)
}

// ConsoleConnect runs the control-plane connect flow for a device:
//  1. POST /api/v1/organizations/{org}/devices/{device}/connect  → get task ID
//  2. Poll GET /api/v1/tasks/{task}/connect-status until COMPLETED or terminal
//  3. Parse ConnectPayload and return PathfinderSession
//
// connectTimeout bounds the whole flow, the connect retries included
// (default 5m if ≤0).
// The returned result contains the pathfinder_session key; callers use it
// to dial via pathfinder.NewClient + ConnectExec.
func (s *Service) ConsoleConnect(ctx context.Context, org, deviceName string, connectTimeout time.Duration) (*ConsoleConnectResult, error) {
	return s.ConsoleConnectWithProgress(ctx, org, deviceName, connectTimeout, nil)
}

// ConsoleConnectWithProgress is ConsoleConnect that reports each step and each
// poll to progress (nil reports nothing), so a caller can show the connect is
// alive while the device takes its time to pick up the task.
func (s *Service) ConsoleConnectWithProgress(ctx context.Context, org, deviceName string, connectTimeout time.Duration, progress func(string)) (*ConsoleConnectResult, error) {
	if progress == nil {
		progress = func(string) {}
	}
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}

	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Minute
	}

	// The deadline covers the whole flow, the step-1 retries included. A retry
	// is also not started past the caller's own deadline, so the refusal that
	// caused it is what the caller gets back.
	deadline := time.Now().Add(connectTimeout)
	retryBy := deadline
	if d, ok := ctx.Deadline(); ok && d.Before(retryBy) {
		retryBy = d
	}

	// Step 1: initiate connect
	progress(fmt.Sprintf("Requesting a connect task for %s", deviceName))
	var initResp models.ConnectInitResponse
	for attempt := 0; ; attempt++ {
		resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect", org, deviceName), nil)
		if err == nil {
			err = api.ParseResponse(resp, &initResp)
			if err == nil {
				break
			}
			if !isAPIError(err) {
				return nil, wrapAPI("parse connect init response: %v", err)
			}
		}
		wait, ok := consoleRetryWait(err, attempt, false)
		if !ok || time.Now().Add(wait).After(retryBy) {
			return nil, wrapAPI("initiate connect: %v", err)
		}
		progress(fmt.Sprintf("The control plane refused the connect request; retrying in %ds", pathfinder.RetryAfterSeconds(wait)))
		if err := consoleWait(ctx, wait); err != nil {
			return nil, err
		}
	}
	if initResp.Task == "" {
		return nil, &Error{Code: CodeAPIError, Message: "connect response missing task ID"}
	}

	// Step 2: poll for completion
	pollInterval := 3 * time.Second
	wait := pollInterval
	refusals := 0

	for {
		if time.Now().After(deadline) {
			return nil, &Error{
				Code:    CodeAPIError,
				Message: fmt.Sprintf("connect timeout after %s", connectTimeout),
			}
		}

		if err := consoleWait(ctx, wait); err != nil {
			return nil, err
		}
		wait = pollInterval

		var status models.ConnectStatusResponse
		statusResp, pollErr := s.api.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s/connect-status", initResp.Task), nil)
		if pollErr == nil {
			pollErr = api.ParseResponse(statusResp, &status)
			if pollErr != nil && !isAPIError(pollErr) {
				return nil, wrapAPI("parse connect status: %v", pollErr)
			}
		}
		if pollErr != nil {
			retryWait, ok := consoleRetryWait(pollErr, refusals, true)
			// Never poll a struggling control plane faster than a healthy one.
			next := max(pollInterval, retryWait)
			if !ok || time.Now().Add(next).After(retryBy) {
				return nil, wrapAPI("poll connect status: %v", pollErr)
			}
			progress(fmt.Sprintf("The control plane did not answer the status poll; retrying in %ds", pathfinder.RetryAfterSeconds(next)))
			refusals++
			wait = next
			continue
		}
		refusals = 0

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
				return nil, &Error{Code: CodeAPIError, Message: "connect payload has no relay session (pathfinder_session)"}
			}
			return &ConsoleConnectResult{
				PathfinderSession: payload.PathfinderSession,
				DeviceName:        deviceName,
				// Authoritative read-only flag from NDManager (nil on older
				// servers that omit it).
				ReadOnly: status.ReadOnly,
			}, nil

		case models.TaskStatusFailed:
			msg := status.Message
			if msg == "" {
				msg = "unknown error"
			}
			return nil, &Error{Code: CodeAPIError, Message: "connect task failed: " + msg}

		case models.TaskStatusCanceled:
			return nil, &Error{Code: CodeAPIError, Message: "connect task was cancelled"}

		case models.TaskStatusExpired:
			return nil, &Error{Code: CodeAPIError, Message: "connect task expired"}
		}
		// PENDING / IN_PROGRESS: continue polling
		progress(fmt.Sprintf("Waiting for %s to pick up the connect task (%s, %ds left)", deviceName, status.Status, int(time.Until(deadline).Seconds())))
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
