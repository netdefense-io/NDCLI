package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// writeActionResult is the human view of a state change: the lines the command
// has always printed, each in the success colour, then its notes, then a blank
// line and its warnings in the warning colour. A result with no message prints
// no line of its own, for a command that has never printed one.
func writeActionResult(w io.Writer, r *models.ActionResult) {
	if r.Message != "" {
		for _, line := range strings.Split(r.Message, "\n") {
			_, _ = ColorSuccess.Fprintln(w, line)
		}
	}
	for _, note := range r.Notes {
		fmt.Fprintln(w, note)
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w)
	}
	for _, warning := range r.Warnings {
		_, _ = ColorWarning.Fprintln(w, warning)
	}
}

// writeApproveAll is the human view of approve-all: a line per device in the
// order they were tried, then the totals.
func writeApproveAll(w io.Writer, r *models.ApproveAllResult) {
	if len(r.Results) == 0 {
		fmt.Fprintln(w, "No pending devices found")
		return
	}
	for _, o := range r.Results {
		if !o.Failed {
			_, _ = ColorSuccess.Fprintf(w, "✓ Approved: %s\n", o.Device)
			continue
		}
		_, _ = ColorError.Fprintf(w, "✗ Failed to approve %s: %s\n", o.Device, WithCode(o.Code, o.Error))
	}
	approved, failed := r.Counts()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Approved: %d, Failed: %d\n", approved, failed)
}

func (f *TableFormatter) FormatActionResult(r *models.ActionResult) error {
	writeActionResult(f.Writer, r)
	return nil
}
func (f *SimpleFormatter) FormatActionResult(r *models.ActionResult) error {
	writeActionResult(f.Writer, r)
	return nil
}
func (f *DetailedFormatter) FormatActionResult(r *models.ActionResult) error {
	writeActionResult(f.Writer, r)
	return nil
}
func (f *JSONFormatter) FormatActionResult(r *models.ActionResult) error { return f.output(r) }

func (f *TableFormatter) FormatApproveAll(r *models.ApproveAllResult) error {
	writeApproveAll(f.Writer, r)
	return nil
}
func (f *SimpleFormatter) FormatApproveAll(r *models.ApproveAllResult) error {
	writeApproveAll(f.Writer, r)
	return nil
}
func (f *DetailedFormatter) FormatApproveAll(r *models.ApproveAllResult) error {
	writeApproveAll(f.Writer, r)
	return nil
}
func (f *JSONFormatter) FormatApproveAll(r *models.ApproveAllResult) error { return f.output(r) }
