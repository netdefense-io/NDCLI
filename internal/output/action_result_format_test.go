package output

import (
	"bytes"
	"testing"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// A message of several lines, as template update prints one per change, is
// coloured line by line: no colour runs across a newline.
func TestActionResultColoursEachMessageLine(t *testing.T) {
	restore := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = restore })

	var buf bytes.Buffer
	writeActionResult(&buf, &models.ActionResult{Resource: "template", Name: "a", Action: "updated",
		Message: "✓ Template renamed: a -> b\n✓ Template description updated: b", Notes: []string{"  note"}})
	want := "\x1b[32m✓ Template renamed: a -> b\x1b[0m\n\x1b[32m✓ Template description updated: b\x1b[0m\n  note\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

// Warnings come after the notes, set off by an uncoloured blank line, each in
// the warning colour.
func TestActionResultPutsWarningsAfterABlankLine(t *testing.T) {
	restore := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = restore })

	var buf bytes.Buffer
	writeActionResult(&buf, &models.ActionResult{Resource: "software_policy", Name: "a", Action: "created",
		Message: "✓ Software policy created: a", Notes: []string{"  note"}, Warnings: []string{"⚠ 1 conflict", "  • x"}})
	want := "\x1b[32m✓ Software policy created: a\x1b[0m\n  note\n\n\x1b[33m⚠ 1 conflict\x1b[0m\n\x1b[33m  • x\x1b[0m\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
