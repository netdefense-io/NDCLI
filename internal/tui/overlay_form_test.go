package tui

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// TestFormModel_MaskedFieldNeverShowsTheTypedValue guards a Mask field
// (e.g. a variable's "value") against ever rendering the literal typed
// text — the whole point of masking a secret input is that it never
// reaches the screen, terminal scrollback or a session recording.
func TestFormModel_MaskedFieldNeverShowsTheTypedValue(t *testing.T) {
	act := registry.Action{
		Label: "new",
		Form: []registry.FormField{
			{Key: "value", Label: "Value", Required: true, Mask: true},
		},
	}
	f := newForm(act, "")
	for _, r := range "hunter2" {
		f.handleKey(string(r))
	}

	view := f.View()
	if strings.Contains(view, "hunter2") {
		t.Fatalf("masked field leaked the typed value into the rendered form:\n%s", view)
	}
	if !strings.Contains(view, strings.Repeat("•", len("hunter2"))) {
		t.Errorf("masked field should render bullets for its length, got:\n%s", view)
	}
}

// TestFormModel_UnmaskedFieldStillShowsTheTypedValue is the negative-space
// check: masking must be opt-in per field, not a side effect of adding the
// Mask option to the type.
func TestFormModel_UnmaskedFieldStillShowsTheTypedValue(t *testing.T) {
	act := registry.Action{
		Label: "new",
		Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
		},
	}
	f := newForm(act, "")
	for _, r := range "corp-ad" {
		f.handleKey(string(r))
	}

	view := f.View()
	if !strings.Contains(view, "corp-ad") {
		t.Errorf("unmasked field should render the typed value, got:\n%s", view)
	}
}
