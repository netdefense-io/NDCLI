package mcp

import (
	"net/http"
	"testing"
)

// ndcli.task.cancel returns NDManager's outcome, which tells a cancelled task
// from a CONNECT task whose relay session was closed, and null for it when the
// control plane predates it; message is NDManager's sentence either way.
func TestTaskCancelToolReturnsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		body             string
		outcome          interface{}
		message, summary string
	}{
		{`{"message": "Remote session closed", "outcome": "SESSION_CLOSED"}`, "SESSION_CLOSED", "Remote session closed", "Remote session closed: Tk123456 (the task stays COMPLETED)"},
		{`{"message": "Task cancelled successfully", "outcome": "CANCELED"}`, "CANCELED", "Task cancelled successfully", "Task cancelled: Tk123456"},
		{`{"message": "Remote session closed"}`, nil, "Remote session closed", "Remote session closed: Tk123456"},
	} {
		cs, seen := firmwareClient(t, http.StatusOK, tc.body)
		resp := callTool(t, cs, "ndcli.task.cancel", `{"task":"Tk123456","confirm":true}`)
		if !resp.Success || seen.method != http.MethodPut || seen.path != "/api/v1/tasks/Tk123456/cancel" {
			t.Fatalf("body %s: response %+v after %s %s", tc.body, resp, seen.method, seen.path)
		}
		data := resp.Data.(map[string]interface{})
		outcome, present := data["outcome"]
		if !present || outcome != tc.outcome || data["task"] != "Tk123456" || data["action"] != "cancelled" || data["message"] != tc.message {
			t.Errorf("body %s: data = %v, want outcome %v", tc.body, data, tc.outcome)
		}
		if resp.Message != tc.summary {
			t.Errorf("body %s: message = %q, want %q", tc.body, resp.Message, tc.summary)
		}
	}
}
