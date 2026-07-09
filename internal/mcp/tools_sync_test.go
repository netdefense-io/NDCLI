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
