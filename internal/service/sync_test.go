package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testESC/testBEL are built via rune(...) rather than a Go escape literal
// so the test source itself never contains a raw control byte.
var (
	syncTestESC = string(rune(0x1b)) // ESC
	syncTestBEL = string(rune(0x07)) // BEL
)

// TestSyncApply_SanitizesResponse is REVERT-SENSITIVE: it fails against the
// pre-amendment SyncApply, which decoded resp.Body directly via
// json.NewDecoder without routing through api.DecodeJSON's sanitize.Struct
// pass. DeviceName/Error strings in the 200/207/400 sync envelope reach the
// terminal via formatter.FormatSyncApply, so a hostile/misbehaving
// NDManager embedding a terminal escape sequence in either field must have
// it scrubbed before it gets this far.
func TestSyncApply_SanitizesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":          "sync triggered",
			"devices_affected": 1,
			"skipped":          0,
			"tasks": []map[string]interface{}{
				{
					"task":              "t-1",
					"device_name":       "evil" + syncTestESC + "[2Jfw-a",
					"snippet_count":     1,
					"vpn_network_count": 0,
					"payload_hash":      "abc123",
				},
			},
			"errors": []map[string]interface{}{
				{
					"device_name": "fw-b",
					"error":       "boom" + syncTestBEL + "detail",
				},
			},
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	result, err := svc.SyncApply(context.Background(), "acme", SyncFilter{}, false)
	if err != nil {
		t.Fatalf("SyncApply returned error: %v", err)
	}

	if len(result.Response.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Response.Tasks))
	}
	if got := result.Response.Tasks[0].DeviceName; strings.ContainsAny(got, syncTestESC+syncTestBEL) {
		t.Errorf("Tasks[0].DeviceName still contains a control byte: %q", got)
	}
	if len(result.Response.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Response.Errors))
	}
	if got := result.Response.Errors[0].Error; strings.ContainsAny(got, syncTestESC+syncTestBEL) {
		t.Errorf("Errors[0].Error still contains a control byte: %q", got)
	}
}

// TestSyncRegisterSpec_SanitizesResponse covers the sibling dual-shape
// decode in SyncRegisterSpec (schedule registration), which shares the
// same defect class as SyncApply.
func TestSyncRegisterSpec_SanitizesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":          "sync-nightly",
			"kind":          "sync",
			"request":       map[string]interface{}{},
			"enabled":       true,
			"schedule_name": "nightly" + syncTestESC + "[2J",
			"created_by":    "admin@acme.com",
			"created_at":    "2026-05-01T00:00:00+00:00",
			"updated_at":    "2026-05-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	spec, err := svc.SyncRegisterSpec(context.Background(), "acme", SyncFilter{Schedule: "nightly"}, false)
	if err != nil {
		t.Fatalf("SyncRegisterSpec returned error: %v", err)
	}
	if strings.ContainsAny(spec.ScheduleName, syncTestESC+syncTestBEL) {
		t.Errorf("ScheduleName still contains a control byte: %q", spec.ScheduleName)
	}
}
