package models

import "fmt"

// The outcomes of a successful cancel, as NDManager names them in the 200
// body's outcome.
const (
	TaskCancelOutcomeCanceled      = "CANCELED"
	TaskCancelOutcomeSessionClosed = "SESSION_CLOSED"
	// TaskCancelOutcomeSessionClosing: a direct-peer session that may still
	// be open. The relay no longer admits it and the device has been told to
	// close; a path that runs peer to peer stays up until the device hears.
	TaskCancelOutcomeSessionClosing = "SESSION_CLOSING"
)

// TaskCancelResult is what a cancel did. Outcome is NDManager's value, empty
// when the control plane predates it; Message is its sentence.
type TaskCancelResult struct {
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
	// CloseTask is the task that tells the device to close a direct-peer
	// session, when the cancel issued one.
	CloseTask string `json:"close_task,omitempty"`
}

// Summary is the line every front-end shows: ndcli's words for an outcome it
// knows, NDManager's sentence for any other.
func (r TaskCancelResult) Summary() string {
	switch r.Outcome {
	case TaskCancelOutcomeCanceled:
		return "Task cancelled: " + r.Task
	case TaskCancelOutcomeSessionClosed:
		return fmt.Sprintf("Remote session closed: %s (the task stays COMPLETED)", r.Task)
	case TaskCancelOutcomeSessionClosing:
		line := fmt.Sprintf("Remote session closing: %s (the task stays COMPLETED). The relay no longer admits it and the device has been told to close", r.Task)
		if r.CloseTask != "" {
			line += " (task " + r.CloseTask + ")"
		}
		return line
	}
	return fmt.Sprintf("%s: %s", r.Message, r.Task)
}
