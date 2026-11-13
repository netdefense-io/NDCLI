package output

import (
	"fmt"
	"io"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// PolicyWarningLines is the server's conflict notices as the human formats
// show them: a count, then a bullet per notice. The write already succeeded;
// the notices say that this policy and another one landing on the same device
// disagree, which fails that device's sync. They are printed in the warning
// colour, never as an error.
func PolicyWarningLines(warnings []string) []string {
	lines := []string{fmt.Sprintf("⚠ %d conflict warning(s) — the change was saved, but sync will fail until they are resolved:", len(warnings))}
	for _, w := range warnings {
		lines = append(lines, "  • "+w)
	}
	return lines
}

func writePolicyWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, line := range PolicyWarningLines(warnings) {
		_, _ = ColorWarning.Fprintln(w, line)
	}
}

// writePackageEdit prints a line per package, then the count of changes, then
// the conflict notices.
func writePackageEdit(w io.Writer, r *models.PackageEditResult) {
	for _, o := range r.Outcomes {
		switch o.Action {
		case "required":
			_, _ = ColorSuccess.Fprintf(w, "✓ Required: %s\n", o.Package)
		case "blocked":
			_, _ = ColorSuccess.Fprintf(w, "✓ Blocked: %s\n", o.Package)
		case "waived":
			_, _ = ColorSuccess.Fprintf(w, "✓ Waived: %s (was: %s)\n", o.Package, o.From)
		case "moved":
			newState := "Required"
			if o.From == models.PackageStateRequired {
				newState = "Blocked"
			}
			_, _ = ColorWarning.Fprintf(w, "↻ %s: %s (was: %s)\n", newState, o.Package, o.From)
		case "no-change":
			if o.From != "" {
				_, _ = ColorNoChange.Fprintf(w, "ℹ %s: already %s (no change)\n", o.Package, o.From)
			} else {
				_, _ = ColorNoChange.Fprintf(w, "ℹ %s: not specified (no change)\n", o.Package)
			}
		}
	}
	changed, moved, noop := r.Counts()
	switch {
	case changed+moved+noop == 0:
	case changed+moved == 0:
		fmt.Fprintln(w, "No changes.")
	case noop > 0:
		fmt.Fprintf(w, "Applied %d change(s) (%d no-op).\n", changed+moved, noop)
	default:
		fmt.Fprintf(w, "Applied %d change(s).\n", changed+moved)
	}
	writePolicyWarnings(w, r.Warnings)
}

// writeEntryEdit prints the repository's or external package's line, then the
// conflict notices.
func writeEntryEdit(w io.Writer, r *models.EntryEditResult) {
	kind := "Repository"
	if r.Kind == "external" {
		kind = "External package"
	}
	switch r.Outcome.Action {
	case "added", "updated", "removed":
		_, _ = ColorSuccess.Fprintf(w, "✓ %s %s: %s\n", kind, r.Outcome.Action, r.Outcome.Name)
	default:
		_, _ = ColorNoChange.Fprintf(w, "ℹ %s %s: no change\n", kind, r.Outcome.Name)
	}
	writePolicyWarnings(w, r.Warnings)
}

func (f *TableFormatter) FormatPackageEdit(r *models.PackageEditResult) error {
	writePackageEdit(f.Writer, r)
	return nil
}
func (f *SimpleFormatter) FormatPackageEdit(r *models.PackageEditResult) error {
	writePackageEdit(f.Writer, r)
	return nil
}
func (f *DetailedFormatter) FormatPackageEdit(r *models.PackageEditResult) error {
	writePackageEdit(f.Writer, r)
	return nil
}
func (f *JSONFormatter) FormatPackageEdit(r *models.PackageEditResult) error { return f.output(r) }

func (f *TableFormatter) FormatEntryEdit(r *models.EntryEditResult) error {
	writeEntryEdit(f.Writer, r)
	return nil
}
func (f *SimpleFormatter) FormatEntryEdit(r *models.EntryEditResult) error {
	writeEntryEdit(f.Writer, r)
	return nil
}
func (f *DetailedFormatter) FormatEntryEdit(r *models.EntryEditResult) error {
	writeEntryEdit(f.Writer, r)
	return nil
}
func (f *JSONFormatter) FormatEntryEdit(r *models.EntryEditResult) error { return f.output(r) }
