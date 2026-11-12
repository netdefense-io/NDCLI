package models

import "fmt"

// The outcomes of a successful cancel, as NDManager names them in the 200
// body's outcome.
const (
	TaskCancelOutcomeCanceled      = "CANCELED"
	TaskCancelOutcomeSessionClosed = "SESSION_CLOSED"
)

// TaskCancelResult is what a cancel did. Outcome is NDManager's value, empty
// when the control plane predates it; Message is its sentence.
type TaskCancelResult struct {
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
}

// Summary is the line every front-end shows: ndcli's words for an outcome it
// knows, NDManager's sentence for any other.
func (r TaskCancelResult) Summary() string {
	switch r.Outcome {
	case TaskCancelOutcomeCanceled:
		return "Task cancelled: " + r.Task
	case TaskCancelOutcomeSessionClosed:
		return fmt.Sprintf("Remote session closed: %s (the task stays COMPLETED)", r.Task)
	}
	return fmt.Sprintf("%s: %s", r.Message, r.Task)
}
