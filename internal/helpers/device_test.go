package helpers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// deviceTestESC is built via rune(...) rather than a Go escape literal so
// the test source itself never contains a raw control byte.
var deviceTestESC = string(rune(0x1b)) // ESC

// TestFindDeviceByName_SanitizesResult is REVERT-SENSITIVE: it fails
// against the pre-amendment FindDeviceByName, which decoded resp.Body
// directly via json.NewDecoder with no size cap or sanitize.Struct pass.
// This helper currently has no callers, but it's live code sharing the
// same defect class as the rest of the decode sites: a returned device
// name can end up on the terminal (name resolution errors, echoed
// confirmations), so a hostile/misbehaving NDManager embedding a terminal
// escape sequence in the name must have it scrubbed.
func TestFindDeviceByName_SanitizesResult(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				// The search term below is the clean "fw-a"; the server
				// tacks a trailing ESC onto the returned name. Without
				// sanitization the raw name ("fw-a\x1b") no longer equals
				// the clean search term, so exact match fails outright —
				// sanitization both cleans the string AND restores correct
				// device resolution.
				{"uuid": "u-1", "name": "fw-a" + deviceTestESC},
			},
			"total": 1,
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := api.NewClient(srv.URL, false, nil)
	name, err := FindDeviceByName(context.Background(), client, "acme", "fw-a")
	if err != nil {
		t.Fatalf("FindDeviceByName returned error: %v", err)
	}
	if strings.ContainsRune(name, '\x1b') {
		t.Errorf("resolved name still contains ESC byte: %q", name)
	}
	if name != "fw-a" {
		t.Errorf("resolved name = %q, want %q", name, "fw-a")
	}
}
