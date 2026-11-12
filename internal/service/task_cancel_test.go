package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The cancel's answer says what happened, so TaskCancel hands on NDManager's
// outcome and sentence: a COMPLETED CONNECT task is not cancelled, its relay
// session is closed. A control plane that predates outcome leaves it empty.
func TestTaskCancelReturnsNDManagersOutcome(t *testing.T) {
	for _, tc := range []struct{ body, outcome, message string }{
		{`{"message": "Task cancelled successfully", "outcome": "CANCELED"}`, "CANCELED", "Task cancelled successfully"},
		{`{"message": "Remote session closed", "outcome": "SESSION_CLOSED"}`, "SESSION_CLOSED", "Remote session closed"},
		{`{"message": "Remote session closed"}`, "", "Remote session closed"},
		{`{"task": "Other999", "message": "Task cancelled successfully", "outcome": "CANCELED"}`, "CANCELED", "Task cancelled successfully"},
		{`{}`, "", "Task cancelled"},
		{``, "", "Task cancelled"},
		{`<html>proxy page</html>`, "", "Task cancelled"},
		{`{"message": "Remote session closed\u001b[2J", "outcome": "SESSION_CLOSED\u001b[2J"}`, "SESSION_CLOSED[2J", "Remote session closed[2J"},
		{`{"message": "Remote session closed\u001b[2J", "outcome": 1}`, "", "Task cancelled"},
		{`{"message": "Remote session closed\u001b[2J", "task": 5}`, "", "Remote session closed[2J"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/api/v1/tasks/Tk123456/cancel" {
				t.Errorf("sent %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, tc.body)
		}))
		got, err := newTestService(t, srv).TaskCancel(t.Context(), "Tk123456")
		srv.Close()
		if err != nil || got.Task != "Tk123456" || got.Outcome != tc.outcome || got.Message != tc.message {
			t.Errorf("body %s: TaskCancel = %+v, %v; want outcome %q, message %q", tc.body, got, err, tc.outcome, tc.message)
		}
	}
}
