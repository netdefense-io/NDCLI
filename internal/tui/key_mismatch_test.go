package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

func mismatchDevice() *models.Device {
	return &models.Device{
		Name: "bt007", KeyState: models.KeyStateMismatch,
		KeyStateDetail: &models.KeyStateDetail{BoundKID: "aa11", PresentedKID: "bb22", Source: "auth", Since: "2026-10-27T10:00:00Z"},
	}
}

func TestHealthSummaryShowsKeyMismatch(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	h := newHealthScreen(a.ctx, "fw-a")
	h.data = sampleTelemetry()

	h.assoc = &deviceAssoc{device: &models.Device{Name: "fw-a"}}
	if out := h.summary(); strings.Contains(out, "KEY MISMATCH") {
		t.Fatalf("unflagged device must render nothing new:\n%s", out)
	}
	h.assoc = nil
	if out := h.summary(); strings.Contains(out, "KEY MISMATCH") {
		t.Fatalf("no association data must render nothing new:\n%s", out)
	}

	d := mismatchDevice()
	d.Name = "fw-a"
	h.assoc = &deviceAssoc{device: d}
	out := h.summary()
	for _, want := range []string{"KEY MISMATCH - rebind required: ndcli device rebind-token fw-a", "bb22"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in summary:\n%s", want, out)
		}
	}
}

func TestKeyMismatchHelpers(t *testing.T) {
	if got := uihelp.KeyMismatchPrefix(&models.Device{Name: "x"}); got != "x" {
		t.Fatalf("unflagged badge = %q", got)
	}
	if got := uihelp.KeyMismatchPrefix(mismatchDevice()); got != "[KEY] bt007" {
		t.Fatalf("flagged badge = %q", got)
	}
	if uihelp.KeyMismatchLines(nil) != nil || uihelp.KeyMismatchLines(&models.Device{Name: "x"}) != nil {
		t.Fatal("unflagged device must have no lines")
	}
}
