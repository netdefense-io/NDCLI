package tui

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/resources"
)

// deviceRemoveAction finds the TUI's remove action the way the app does.
func deviceRemoveAction(t *testing.T) registry.Action {
	t.Helper()
	reg := registry.New()
	resources.RegisterAll(reg)
	res, ok := reg.Get("device")
	if !ok {
		t.Fatal("device resource is not registered")
	}
	for _, a := range res.Actions() {
		if a.Label == "remove" {
			return a
		}
	}
	t.Fatal("device resource has no remove action")
	return registry.Action{}
}

// TestDeviceRemoveConfirm_WarnsAboutSelfDecommission keeps the third front-end
// in step with the CLI prompt and the MCP tool description. Removal is final
// and wipes the box, so the TUI's modal has to say so and demand a typed
// confirmation rather than a single keypress.
func TestDeviceRemoveConfirm_WarnsAboutSelfDecommission(t *testing.T) {
	act := deviceRemoveAction(t)
	if act.BlastRadius != service.DeviceRemoveConsequence {
		t.Fatalf("remove must carry the shared consequence wording, got %q", act.BlastRadius)
	}

	c := newConfirm(act, "clarence")
	if !c.needsType() {
		t.Error("removal must require typing yes, not a single keypress")
	}
	view := c.View()
	// The modal wraps and pads, so match against the text with every run of
	// whitespace (and the box borders) collapsed to a single space.
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(view)), " ")
	for _, want := range []string{"clarence", "NetDefense-managed configuration", "package repositories", "fresh installation"} {
		if !strings.Contains(flat, want) {
			t.Errorf("confirm modal is missing %q; got:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 100 {
			t.Errorf("modal line runs off the terminal (%d cols): %s", len([]rune(line)), line)
		}
	}
}
