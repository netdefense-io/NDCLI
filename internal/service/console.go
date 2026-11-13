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
	// Task is the CONNECT task's code. A caller that gives the session up
	// before handing it on passes it to CloseConnectSession.
	Task string
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
// always retried, which is safe for POST /connect only while NDManager's 429s
// come before its handler (see the comment in ConsoleConnect's step 1). A
// 500/502/503/504 or a transport error is retried only for a
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
			if !api.IsAPIError(err) {
				return nil, wrapAPI("parse connect init response: %v", err)
			}
		}
		// Retrying POST /connect on a 429 is safe only because every 429
		// NDManager sends comes before the handler runs (its rate-limit
		// middleware and route dependency), so no CONNECT task exists yet. A
		// quota checked inside the handler would need its own error code,
		// excluded here, or a 429 would start a second session.
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

	// A connect given up before it completes (a timeout, ctx done, a failed
	// poll) cancels the task, or a device that picks it up later would wait on
	// the relay for a session nobody joins. A task that ended on its own, or a
	// session handed to the caller, needs nothing here.
	settled := false
	defer func() {
		if !settled {
			_ = s.CloseConnectSession(initResp.Task)
		}
	}()

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
			if pollErr != nil && !api.IsAPIError(pollErr) {
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
			settled = true
			return &ConsoleConnectResult{
				Task:              initResp.Task,
				PathfinderSession: payload.PathfinderSession,
				DeviceName:        deviceName,
				// Authoritative read-only flag from NDManager (nil on older
				// servers that omit it).
				ReadOnly: status.ReadOnly,
			}, nil

		case models.TaskStatusFailed:
			settled = true
			msg := status.Message
			if msg == "" {
				msg = "unknown error"
			}
			// The device's own relay dial may have been refused; its report
			// carries the relay's status and Retry-After.
			return nil, &Error{Code: CodeAPIError, Message: "connect task failed: " + msg, Err: pathfinder.AgentRelayRefusal(msg)}

		case models.TaskStatusCanceled:
			settled = true
			return nil, &Error{Code: CodeAPIError, Message: "connect task was cancelled"}

		case models.TaskStatusExpired:
			settled = true
			return nil, &Error{Code: CodeAPIError, Message: "connect task expired"}
		}
		// PENDING / IN_PROGRESS: continue polling
		progress(fmt.Sprintf("Waiting for %s to pick up the connect task (%s, %ds left)", deviceName, status.Status, int(time.Until(deadline).Seconds())))
	}
}

// connectCloseTimeout bounds CloseConnectSession, retry included.
const connectCloseTimeout = 10 * time.Second

// CloseConnectSession cancels a CONNECT task the caller gives up before
// handing its session on. A task not yet completed (a timeout, an interrupt, a
// failed poll) is cancelled, so the device never joins a session nobody uses.
// For a completed one (a refused or failed relay connection, a read-only
// console, a relay client that could not be built) NDManager deletes the
// relay session, which closes the device's leg at once instead of leaving it
// to wait out the session's TTL (NDPathFinder #30). A session that is already
// gone counts as closed. It is best effort, so the caller reports
// its own failure, not this one, and it runs on its own deadline because the
// caller's context may be what ended the connect. A relay that could not close
// the session (502 REMOTE_SESSION_UNAVAILABLE) gets one more try.
func (s *Service) CloseConnectSession(task string) error {
	if task == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectCloseTimeout)
	defer cancel()
	_, err := s.TaskCancel(ctx, task)
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadGateway && apiErr.Code == "REMOTE_SESSION_UNAVAILABLE" {
		if waitErr := consoleWait(ctx, time.Second); waitErr != nil {
			return err
		}
		_, err = s.TaskCancel(ctx, task)
	}
	return err
}

// MaxConsoleCommandBytes is the longest console command: its exec request
// travels as one relay frame, and the relay takes at most
// pathfinder.MaxExecFrameBytes.
const MaxConsoleCommandBytes = 32 * 1024

// CheckConsoleCommand refuses a console command that is empty or longer than
// MaxConsoleCommandBytes, before anything is sent.
func CheckConsoleCommand(command string) error {
	if command == "" {
		return &Error{Code: CodeInvalidInput, Message: "command is required"}
	}
	if len(command) > MaxConsoleCommandBytes {
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("command must be at most %d bytes (32 KiB); it has %d", MaxConsoleCommandBytes, len(command))}
	}
	return nil
}

// MaxConsoleExecTimeoutSeconds is the longest a single console command may run;
// a longer timeout is clamped to it.
const MaxConsoleExecTimeoutSeconds = 3600

// consoleExecTimeout is a command's timeout: 60 s unless set, and clamped to
// MaxConsoleExecTimeoutSeconds before multiplying, so a huge timeout_seconds
// cannot overflow the duration and wrap to an arbitrary or negative timeout.
func consoleExecTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(min(seconds, MaxConsoleExecTimeoutSeconds)) * time.Second
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
	if err := CheckConsoleCommand(opts.Command); err != nil {
		return nil, err
	}

	stdout, stderr, exitCode, truncated, err := handle.Exec(ctx, opts.Command, consoleExecTimeout(opts.TimeoutSeconds))
	var tooLarge *pathfinder.ExecRequestTooLargeError
	if errors.As(err, &tooLarge) {
		return nil, &Error{Code: CodeInvalidInput, Message: tooLarge.Error(), Err: err}
	}
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
