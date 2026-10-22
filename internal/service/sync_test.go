package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// TestSyncFilters_SendOrgAndOUAsBareNames: NDManager compares organization and
// ou to names with equality, so they go out exactly as given. An anchored or
// escaped form (^acme$) would match nothing.
func TestSyncFilters_SendOrgAndOUAsBareNames(t *testing.T) {
	var queries []map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}, "total": 0, "tasks": []interface{}{}})
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	filter := SyncFilter{Organization: "acme.corp", OU: "branch (east)", Device: "^fw-0[12]$"}
	if _, err := svc.SyncStatus(context.Background(), "default-org", filter); err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if _, err := svc.SyncApply(context.Background(), "default-org", filter, false); err != nil {
		t.Fatalf("SyncApply: %v", err)
	}
	if _, err := svc.SyncStatus(context.Background(), "default-org", SyncFilter{}); err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}

	if len(queries) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(queries))
	}
	for i, q := range queries[:2] {
		for key, want := range map[string]string{"organization": "acme.corp", "ou": "branch (east)", "device": "^fw-0[12]$"} {
			if got := q[key]; len(got) != 1 || got[0] != want {
				t.Errorf("request %d sent %s=%v, want %q", i, key, got, want)
			}
		}
	}
	if got := queries[2]["organization"]; len(got) != 1 || got[0] != "default-org" {
		t.Errorf("an empty organization must fall back to the default org as given, got %v", got)
	}
}

// TestExactNamePatternSelectsOnlyThatName: NDManager compiles the sync device
// filter with RE2, case-insensitive, and searches it unanchored. Go's regexp is
// RE2 too, so it stands in for the server here.
func TestExactNamePatternSelectsOnlyThatName(t *testing.T) {
	if got, want := ExactNamePattern("fw.01"), `^fw\.01$`; got != want {
		t.Fatalf("ExactNamePattern(%q) = %q, want %q", "fw.01", got, want)
	}

	fleet := []string{"fw-01", "fw-010", "lab-fw-01", "fw-01.old", "fw.01", "fwx01"}
	for _, name := range []string{"fw-01", "fw.01"} {
		re := regexp.MustCompile("(?i)" + ExactNamePattern(name))
		var selected []string
		for _, candidate := range fleet {
			if re.MatchString(candidate) {
				selected = append(selected, candidate)
			}
		}
		if len(selected) != 1 || selected[0] != name {
			t.Errorf("pattern for %q selects %v, want only %q", name, selected, name)
		}
	}
}
