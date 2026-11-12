package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/pathfinder"
	"github.com/netdefense-io/NDCLI/internal/service"
)

func decodeToolResponse(t *testing.T, res *mcp.CallToolResult) ToolResponse {
	t.Helper()
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	var tr ToolResponse
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &tr); err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestRelayConnectErrorResult_RateLimited(t *testing.T) {
	s := &Server{}
	cause := &pathfinder.RateLimitedError{RetryAfter: 7 * time.Second}
	err := &pathfinder.PathfinderError{Message: "failed to connect to relay: " + cause.Error(), Err: cause}

	res, _ := s.relayConnectErrorResult(err)
	tr := decodeToolResponse(t, res)
	if tr.Error.Code != "RATE_LIMITED" {
		t.Errorf("code = %q", tr.Error.Code)
	}
	data, _ := tr.Data.(map[string]interface{})
	if data["retry_after_seconds"] != float64(7) {
		t.Errorf("data = %#v", tr.Data)
	}

	// A sub-second Retry-After (the HTTP-date form) must not read as 0, the
	// value for a relay that gave none.
	cause = &pathfinder.RateLimitedError{RetryAfter: 400 * time.Millisecond}
	res, _ = s.relayConnectErrorResult(&pathfinder.PathfinderError{Message: "failed to connect to relay: " + cause.Error(), Err: cause})
	tr = decodeToolResponse(t, res)
	data, _ = tr.Data.(map[string]interface{})
	if data["retry_after_seconds"] != float64(1) {
		t.Errorf("400ms Retry-After: data = %#v, want retry_after_seconds 1", data)
	}
	if !strings.Contains(tr.Error.Message, "retry in 1s") {
		t.Errorf("400ms Retry-After: message %q, want it to say retry in 1s like the data", tr.Error.Message)
	}
}

func TestRelayConnectErrorResult_Other(t *testing.T) {
	s := &Server{}
	res, _ := s.relayConnectErrorResult(&pathfinder.PathfinderError{Message: "pairing failed: pairing timeout"})
	tr := decodeToolResponse(t, res)
	if tr.Error.Code != service.CodeAPIError || tr.Error.Message != "pairing failed: pairing timeout" {
		t.Errorf("got %+v", tr.Error)
	}
}

func TestConsoleExecErrorResult_SessionEnded(t *testing.T) {
	s := &Server{}
	wrapped := &service.Error{
		Code: service.CodeAPIError, Message: "exec failed",
		Err: fmt.Errorf("x: %w", &pathfinder.SessionEndedError{Reason: "device went offline"}),
	}
	res, _ := s.consoleExecErrorResult("sess-1", wrapped)
	tr := decodeToolResponse(t, res)
	if tr.Error.Code != "SESSION_CLOSED" {
		t.Errorf("code = %q", tr.Error.Code)
	}
	if want := `console session "sess-1" ended: device went offline. Open a new session with console_open.`; tr.Error.Message != want {
		t.Errorf("message = %q", tr.Error.Message)
	}
}
