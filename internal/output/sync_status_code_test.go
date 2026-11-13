package output

import (
	"encoding/json"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func strp(s string) *string { return &s }

// A device whose sync payload the server cannot build carries the error and
// its code, and every format shows the code as [CODE] beside it.
func TestSyncStatusShowsTheErrorsCode(t *testing.T) {
	items := []models.SyncStatusItem{
		{DeviceName: "fw-01", InSync: true},
		{DeviceName: "fw-02", Error: strp("Undefined variables in snippet content"), Code: strp("UNDEFINED_VARIABLES")},
	}
	mustContain(t, "detailed", render(t, "detailed", "FormatSyncStatus", items, 2), "[UNDEFINED_VARIABLES] Undefined variables in snippet content")
	mustContain(t, "simple", render(t, "simple", "FormatSyncStatus", items, 2), "[UNDEFINED_VARIABLES] Undefined variables in snippet content")
	mustContain(t, "table", render(t, "table", "FormatSyncStatus", items, 2), "[UNDEFINED_VARIABLES] ERROR")

	var doc struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal([]byte(render(t, "json", "FormatSyncStatus", items, 2)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Items[1]["code"] != "UNDEFINED_VARIABLES" {
		t.Errorf("json items %v", doc.Items)
	}
	if _, present := doc.Items[0]["code"]; present {
		t.Errorf("a device without an error carries a code: %v", doc.Items[0])
	}
}

// From a server that predates the code, the error shows as it did.
func TestSyncStatusErrorWithoutACode(t *testing.T) {
	items := []models.SyncStatusItem{{DeviceName: "fw-02", Error: strp("Could not build the sync payload")}}
	out := render(t, "detailed", "FormatSyncStatus", items, 1)
	mustContain(t, "detailed", out, "Could not build the sync payload")
	mustNotContain(t, "detailed", out, "[")
}
