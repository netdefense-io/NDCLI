package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestStatusStyleColoursEveryTaskStatus: statusStyle matches on literals, and a
// literal that names no status the server sends colours nothing and fails
// quietly — CANCELLED did exactly that for CANCELED. Every canonical status has
// to land on a colour.
func TestStatusStyleColoursEveryTaskStatus(t *testing.T) {
	plain := lipgloss.NewStyle()
	for _, s := range models.TaskStatuses {
		if reflect.DeepEqual(statusStyle(s), plain) {
			t.Errorf("task status %q has no colour in statusStyle", s)
		}
	}
}
