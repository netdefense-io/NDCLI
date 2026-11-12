package output

import (
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// FormatTaskCancelled renders what a task cancel did.
func (f *TableFormatter) FormatTaskCancelled(r *models.TaskCancelResult) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintln(f.Writer, r.Summary())
	return nil
}

// FormatTaskCancelled renders what a task cancel did (simple).
func (f *SimpleFormatter) FormatTaskCancelled(r *models.TaskCancelResult) error {
	fmt.Fprintln(f.Writer, r.Summary())
	return nil
}

// FormatTaskCancelled renders what a task cancel did (detailed).
func (f *DetailedFormatter) FormatTaskCancelled(r *models.TaskCancelResult) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintln(f.Writer, r.Summary())
	return nil
}

// FormatTaskCancelled renders what a task cancel did as JSON; outcome is null
// when the control plane did not send one.
func (f *JSONFormatter) FormatTaskCancelled(r *models.TaskCancelResult) error {
	var outcome interface{}
	if r.Outcome != "" {
		outcome = r.Outcome
	}
	return f.output(map[string]interface{}{
		"task":    r.Task,
		"outcome": outcome,
		"message": r.Message,
	})
}
