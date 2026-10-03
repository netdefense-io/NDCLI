package output

import (
	"fmt"
	"io"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// writeSnippetPull renders a pull for the human formats: the task that was
// created, or once it has completed, the snippet it wrote and its content.
func writeSnippetPull(w io.Writer, pull *models.SnippetPull) {
	if pull.Status != models.TaskStatusCompleted {
		ColorSuccess.Fprintf(w, "✓ Pull task created: %s\n", pull.Task)
		fmt.Fprintf(w, "Check task status with: ndcli task describe %s\n", pull.Task)
		return
	}
	ColorSuccess.Fprintf(w, "✓ Snippet pulled successfully: %s\n", pull.Snippet)
	if pull.Content != "" {
		writeContent(w, pull.Content)
	}
}

// FormatSnippetPull formats a snippet pull
func (f *TableFormatter) FormatSnippetPull(pull *models.SnippetPull) error {
	writeSnippetPull(f.Writer, pull)
	return nil
}

// FormatSnippetPull formats a snippet pull
func (f *SimpleFormatter) FormatSnippetPull(pull *models.SnippetPull) error {
	writeSnippetPull(f.Writer, pull)
	return nil
}

// FormatSnippetPull formats a snippet pull
func (f *DetailedFormatter) FormatSnippetPull(pull *models.SnippetPull) error {
	writeSnippetPull(f.Writer, pull)
	return nil
}

// FormatSnippetPull formats a snippet pull as JSON
func (f *JSONFormatter) FormatSnippetPull(pull *models.SnippetPull) error {
	return f.output(pull)
}
