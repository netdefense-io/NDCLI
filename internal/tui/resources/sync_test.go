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
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// The sync device filter is an unanchored regex on the server, so a row's
// bare name would also sync every device whose name contains it.
func TestRowSyncTargetsOnlyThatDevice(t *testing.T) {
	var queries []map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sync" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		queries = append(queries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"devices_affected": 0, "tasks": []interface{}{}, "errors": []interface{}{}})
	}))
	defer srv.Close()
	svc := service.New(api.NewClient(srv.URL, false, auth.NewStaticTokenProvider("ndpat_test")), nil, nil)

	for _, tc := range []struct {
		name string
		res  registry.Resource
	}{
		{"device list", deviceResource{}},
		{"sync status list", syncResource{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries = nil
			if _, err := tc.res.Execute(context.Background(), svc, "acme", "fw.01", "s", nil); err != nil {
				t.Fatalf("sync: %v", err)
			}
			if len(queries) != 1 {
				t.Fatalf("%d sync requests, want 1", len(queries))
			}
			if got := queries[0]["device"]; len(got) != 1 || got[0] != `^fw\.01$` {
				t.Errorf("device = %v, want [^fw\\.01$]", got)
			}
			if got := queries[0]["organization"]; len(got) != 1 || got[0] != "acme" {
				t.Errorf("organization = %v, want [acme]", got)
			}
		})
	}

	t.Run("sync all", func(t *testing.T) {
		queries = nil
		if _, err := (syncResource{}).Execute(context.Background(), svc, "acme", "", "S", nil); err != nil {
			t.Fatalf("sync all: %v", err)
		}
		if len(queries) != 1 {
			t.Fatalf("%d sync requests, want 1", len(queries))
		}
		if _, ok := queries[0]["device"]; ok {
			t.Errorf("sync all sent a device filter: %v", queries[0]["device"])
		}
	})
}
