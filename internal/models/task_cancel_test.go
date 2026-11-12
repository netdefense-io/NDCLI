package models

import "testing"

// The line shown for a cancel comes from the outcome when it is one ndcli
// knows, and from NDManager's sentence otherwise.
func TestTaskCancelSummary(t *testing.T) {
	for _, tc := range []struct {
		r    TaskCancelResult
		want string
	}{
		{TaskCancelResult{Task: "Tk123456", Outcome: TaskCancelOutcomeCanceled, Message: "Task cancelled successfully"}, "Task cancelled: Tk123456"},
		{TaskCancelResult{Task: "Tk123456", Outcome: TaskCancelOutcomeSessionClosed, Message: "Remote session closed"}, "Remote session closed: Tk123456 (the task stays COMPLETED)"},
		{TaskCancelResult{Task: "Tk123456", Message: "Remote session closed"}, "Remote session closed: Tk123456"},
		{TaskCancelResult{Task: "Tk123456", Outcome: "SOMETHING_NEW", Message: "Partly cancelled"}, "Partly cancelled: Tk123456"},
	} {
		if got := tc.r.Summary(); got != tc.want {
			t.Errorf("%+v: Summary = %q, want %q", tc.r, got, tc.want)
		}
	}
}
