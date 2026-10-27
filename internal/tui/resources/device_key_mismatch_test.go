package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/service"
)

func TestDeviceRowsAndDescribeShowKeyMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bad := map[string]interface{}{"name": "bt007", "status": "ENABLED", "key_state": "mismatch",
			"key_state_detail": map[string]string{"bound_kid": "aa", "presented_kid": "bb", "source": "auth", "since": "2026-10-27T10:00:00Z"}}
		ok := map[string]interface{}{"name": "fine", "status": "ENABLED", "key_state": nil, "key_state_detail": nil}
		if r.URL.Path == "/api/v1/organizations/acme/devices" {
			json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{bad, ok}, "total": 2})
			return
		}
		json.NewEncoder(w).Encode(bad)
	}))
	defer srv.Close()
	svc := service.New(api.NewClient(srv.URL, false, auth.NewStaticTokenProvider("ndpat_test")), nil, nil)

	rows, _, err := deviceResource{}.Fetch(context.Background(), svc, "acme", 1, 20)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(rows) != 2 || rows[0].Cells[0] != "[KEY] bt007" || rows[0].ID != "bt007" || rows[1].Cells[0] != "fine" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	secs, err := deviceResource{}.Describe(context.Background(), svc, "acme", "bt007")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	found := false
	for _, f := range secs[0].Fields {
		if f.Label == "Key" && f.Value == "KEY MISMATCH - rebind required: ndcli device rebind-token bt007" {
			found = true
		}
	}
	if !found {
		t.Fatalf("describe lacks the key mismatch field: %+v", secs[0].Fields)
	}
}
