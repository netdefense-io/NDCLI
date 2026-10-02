package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestSyncApply_ScheduleGatedByConfirm verifies that registering a recurring
// SYNC spec via --schedule is gated by confirm just like an immediate apply:
// without confirm, /api/v1/sync must not be hit and the response must be a
// preview; with confirm=true, the spec is registered. Fails against pre-fix
// code, where the schedule branch ran before the confirm check.
func TestSyncApply_ScheduleGatedByConfirm(t *testing.T) {
	var syncHits int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/status":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"device_name": "fw-01", "organization": "acme", "in_sync": true},
				},
				"total": 1,
			})
		case "/api/v1/sync":
			atomic.AddInt32(&syncHits, 1)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":          "IjKl9012",
				"kind":          "SYNC",
				"request":       map[string]interface{}{},
				"enabled":       true,
				"schedule_name": "nightly",
				"created_by":    "admin@acme.com",
				"last_fired_at": nil,
				"created_at":    "2026-05-01T00:00:00+00:00",
				"updated_at":    "2026-05-01T00:00:00+00:00",
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &syncApplyInput{}
	input.Schedule = "nightly"

	result, err := s.syncApply(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&syncHits) != 0 {
		t.Fatalf("expected 0 /api/v1/sync requests before confirm, got %d", syncHits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected an unconfirmed schedule call to return a preview, got %v", resp.Data)
	}

	input.Confirm = true
	result, err = s.syncApply(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&syncHits) != 1 {
		t.Fatalf("expected 1 /api/v1/sync request after confirm, got %d", syncHits)
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
}

// TestSyncApply_RendersAuthIssues guards ndcli.sync.apply's error mapping
// against dropping auth_issues: an AUTH_BUILD_INVALID device error must
// carry its auth_issues list through to the tool's result data, not just
// device/error/code.
func TestSyncApply_RendersAuthIssues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/status":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{{"device_name": "e2e-a", "organization": "acme", "in_sync": false}},
				"total": 1,
			})
		case "/api/v1/sync":
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":          "Sync triggered for 0 device(s); 1 error(s)",
				"devices_affected": 0,
				"skipped":          0,
				"tasks":            []interface{}{},
				"errors": []map[string]interface{}{
					{
						"device_name": "e2e-a",
						"error":       "AUTH build failed",
						"code":        "AUTH_BUILD_INVALID",
						"auth_issues": []map[string]interface{}{
							{"code": "AUTH_GROUP_NOT_ATTACHED", "message": "group not attached", "group": "IT-Staff", "server": "Corp-AD"},
						},
					},
				},
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	input := &syncApplyInput{Confirm: true}

	result, err := s.syncApply(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	errs, ok := data["errors"].([]interface{})
	if !ok || len(errs) != 1 {
		t.Fatalf("expected exactly one error, got %v", data["errors"])
	}
	errEntry, ok := errs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map error entry, got %T", errs[0])
	}
	issues, ok := errEntry["auth_issues"].([]interface{})
	if !ok || len(issues) != 1 {
		t.Fatalf("expected auth_issues to survive into the tool result, got %v", errEntry["auth_issues"])
	}
	issue, ok := issues[0].(map[string]interface{})
	if !ok || issue["code"] != "AUTH_GROUP_NOT_ATTACHED" || issue["group"] != "IT-Staff" {
		t.Errorf("auth_issues[0] = %v, want code AUTH_GROUP_NOT_ATTACHED / group IT-Staff", issue)
	}
}

// TestSyncApply_RendersTrustIssuesAlongsideAuthIssues guards the same
// mapping for trust_issues, on a device error that carries both lists:
// neither may displace the other in the tool result.
func TestSyncApply_RendersTrustIssuesAlongsideAuthIssues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/status":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{{"device_name": "e2e-a", "organization": "acme", "in_sync": false}},
				"total": 1,
			})
		case "/api/v1/sync":
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":          "Sync triggered for 0 device(s); 1 error(s)",
				"devices_affected": 0,
				"skipped":          0,
				"tasks":            []interface{}{},
				"errors": []map[string]interface{}{
					{
						"device_name": "e2e-a",
						"error":       "build failed",
						"code":        "AUTH_BUILD_INVALID",
						"auth_issues": []map[string]interface{}{
							{"code": "AUTH_GROUP_NOT_ATTACHED", "message": "group not attached", "group": "IT-Staff"},
						},
						"trust_issues": []map[string]interface{}{
							{"code": "TRUST_KEY_MISMATCH", "message": "key does not match certificate", "snippet": []string{"fw-wildcard"}, "template": []string{"edge"}, "name": "fw-wildcard"},
						},
					},
				},
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, err := s.syncApply(context.Background(), &syncApplyInput{Confirm: true})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	errs, ok := data["errors"].([]interface{})
	if !ok || len(errs) != 1 {
		t.Fatalf("expected exactly one error, got %v", data["errors"])
	}
	errEntry, ok := errs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map error entry, got %T", errs[0])
	}
	if auth, ok := errEntry["auth_issues"].([]interface{}); !ok || len(auth) != 1 {
		t.Errorf("expected auth_issues to survive next to trust_issues, got %v", errEntry["auth_issues"])
	}
	trust, ok := errEntry["trust_issues"].([]interface{})
	if !ok || len(trust) != 1 {
		t.Fatalf("expected trust_issues to survive into the tool result, got %v", errEntry["trust_issues"])
	}
	issue, ok := trust[0].(map[string]interface{})
	if !ok || issue["code"] != "TRUST_KEY_MISMATCH" || issue["name"] != "fw-wildcard" {
		t.Errorf("trust_issues[0] = %v, want code TRUST_KEY_MISMATCH / name fw-wildcard", issue)
	}
	if snippets, _ := issue["snippet"].([]interface{}); len(snippets) != 1 || snippets[0] != "fw-wildcard" {
		t.Errorf("trust_issues[0].snippet = %v, want [fw-wildcard]", issue["snippet"])
	}
}
