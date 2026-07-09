package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// specRegisterJSON is a minimal, valid ScheduledTaskRegisterResult payload.
func specRegisterJSON(code, scheduleName string) map[string]interface{} {
	return map[string]interface{}{
		"code":          code,
		"kind":          "RUN",
		"request":       map[string]interface{}{},
		"enabled":       true,
		"schedule_name": scheduleName,
		"created_by":    "admin@acme.com",
		"last_fired_at": nil,
		"created_at":    "2026-05-01T00:00:00+00:00",
		"updated_at":    "2026-05-01T00:00:00+00:00",
	}
}

// TestRunCommand_ScheduleGatedByConfirm verifies that registering a
// recurring spec via --schedule is gated by confirm just like an immediate
// run: without confirm, the tasks endpoint must not be hit and the response
// must be a preview; with confirm=true, the spec is registered. Fails
// against pre-fix code, where the schedule branch ran before the confirm
// check and always hit the endpoint.
func TestRunCommand_ScheduleGatedByConfirm(t *testing.T) {
	var hits int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(specRegisterJSON("AbCd1234", "nightly"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		Schedule: "nightly",
	}

	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("expected 0 requests before confirm, got %d", hits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected an unconfirmed schedule call to return a preview, got %v", resp.Data)
	}

	input.Confirm = true
	result, err = s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 request after confirm, got %d", hits)
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
}

// TestFirmwareUpgrade_ScheduleGatedByConfirm is the same regression, but for
// the dedicated firmware_upgrade handler core, which has its own
// schedule-vs-confirm ordering.
func TestFirmwareUpgrade_ScheduleGatedByConfirm(t *testing.T) {
	var hits int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(specRegisterJSON("EfGh5678", "monthly"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		Mode:     "minor",
		Schedule: "monthly",
	}

	result, err := s.firmwareUpgrade(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("expected 0 requests before confirm, got %d", hits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected an unconfirmed schedule call to return a preview, got %v", resp.Data)
	}

	input.Confirm = true
	result, err = s.firmwareUpgrade(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 request after confirm, got %d", hits)
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
}
