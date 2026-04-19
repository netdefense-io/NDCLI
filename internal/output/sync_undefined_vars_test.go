package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Covers NDCLI-go#27: the sync-apply error output names each snippet when the
// server returned undefined variables grouped by snippet, and falls back to
// a flat list when it didn't.

func makeSyncResponse(err models.SyncError) *models.SyncApplyResponse {
	return &models.SyncApplyResponse{
		Message:         "Sync triggered for 0 device(s); 1 error(s)",
		DevicesAffected: 0,
		Skipped:         0,
		Errors:          []models.SyncError{err},
	}
}

func TestSimpleFormatter_SyncApply_UndefinedBySnippet(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := models.SyncError{
		DeviceName: "murphy01",
		Error:      "Undefined variables in snippet content",
		Code:       "UNDEFINED_VARIABLES",
		UndefinedVariables: []string{"interface_name", "vlan_id"},
		UndefinedVariablesBySnippet: map[string][]string{
			"wan-rules":  {"interface_name"},
			"vlan-setup": {"vlan_id"},
		},
	}
	if ferr := f.FormatSyncApply(makeSyncResponse(err)); ferr != nil {
		t.Fatalf("format failed: %v", ferr)
	}
	out := buf.String()
	for _, want := range []string{
		`Snippet "vlan-setup": ${vlan_id}`,
		`Snippet "wan-rules": ${interface_name}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Undefined: ") {
		t.Errorf("should not emit flat 'Undefined:' line when grouped info is present:\n%s", out)
	}
}

func TestSimpleFormatter_SyncApply_UndefinedFlatFallback(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := models.SyncError{
		DeviceName:         "murphy01",
		Error:              "Undefined variables in snippet content",
		Code:               "UNDEFINED_VARIABLES",
		UndefinedVariables: []string{"not_a_variable"},
		// UndefinedVariablesBySnippet intentionally empty — older NDManager.
	}
	if ferr := f.FormatSyncApply(makeSyncResponse(err)); ferr != nil {
		t.Fatalf("format failed: %v", ferr)
	}
	out := buf.String()
	if !strings.Contains(out, "Undefined: ${not_a_variable}") {
		t.Errorf("expected flat 'Undefined:' fallback when no grouped info, got:\n%s", out)
	}
	if strings.Contains(out, `Snippet "`) {
		t.Errorf("should not emit snippet-prefixed lines when no grouped info:\n%s", out)
	}
}
