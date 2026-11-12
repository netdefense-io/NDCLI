package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
)

// task cancel says what happened from NDManager's outcome, so closing a
// CONNECT task's session reads differently from cancelling a waiting task. An
// older control plane sends only its sentence, which is printed instead.
func TestTaskCancelPrintsWhatHappened(t *testing.T) {
	cmd := findCommand(t, "task", "cancel")
	for _, tc := range []struct{ body, table string }{
		{`{"message": "Task cancelled successfully", "outcome": "CANCELED"}`, "✓ Task cancelled: Tk123456\n"},
		{`{"message": "Remote session closed", "outcome": "SESSION_CLOSED"}`, "✓ Remote session closed: Tk123456 (the task stays COMPLETED)\n"},
		{`{"message": "Remote session closed"}`, "✓ Remote session closed: Tk123456\n"},
	} {
		stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, tc.body)
		}, "table", "", func() error {
			return runTaskCancel(cmd, []string{"Tk123456"})
		})
		if stdout != tc.table {
			t.Errorf("body %s: stdout = %q, want %q", tc.body, stdout, tc.table)
		}
	}
}

// Under -f json the cancel is one object, outcome null when not sent.
func TestTaskCancelJSON(t *testing.T) {
	cmd := findCommand(t, "task", "cancel")
	for body, want := range map[string]map[string]any{
		`{"message": "Remote session closed", "outcome": "SESSION_CLOSED"}`: {"task": "Tk123456", "outcome": "SESSION_CLOSED", "message": "Remote session closed"},
		`{"message": "Task cancelled successfully"}`:                        {"task": "Tk123456", "outcome": nil, "message": "Task cancelled successfully"},
	} {
		stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}, "json", "", func() error {
			return runTaskCancel(cmd, []string{"Tk123456"})
		})
		var got map[string]any
		if err := json.Unmarshal([]byte(stdout), &got); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("body %s: stdout %q (%v), want %v", body, stdout, err, want)
		}
	}
}
