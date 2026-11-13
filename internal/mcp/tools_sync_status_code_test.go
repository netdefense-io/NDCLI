package mcp

import (
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// ndcli.sync.status returns the code of a device's sync error beside the
// error, null when there is none.
func TestSyncStatusItemCarriesTheCode(t *testing.T) {
	msg, code := "Undefined variables in snippet content", "UNDEFINED_VARIABLES"
	got := syncStatusItem(&models.SyncStatusItem{DeviceName: "fw-02", Error: &msg, Code: &code})
	if c, ok := got["code"].(*string); !ok || c == nil || *c != code {
		t.Errorf("code = %v, want %s", got["code"], code)
	}
	clean := syncStatusItem(&models.SyncStatusItem{DeviceName: "fw-01", InSync: true})
	if c, present := clean["code"]; !present || c.(*string) != nil {
		t.Errorf("a device without an error: code = %v, want a null key", clean["code"])
	}
}
