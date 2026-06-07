package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestRun_TargetsSerializeAsLists is a regression for the NDManager rejection
// "body.targets.ous: Input should be a valid list": when a caller targets by
// device only and leaves OUs nil (as the TUI device actions do), the request
// must still send targets.devices/targets.ous as [] — never null.
func TestRun_TargetsSerializeAsLists(t *testing.T) {
	var raw map[string]json.RawMessage
	var targets map[string]json.RawMessage
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if err := json.Unmarshal(raw["targets"], &targets); err != nil {
			t.Fatalf("decode targets: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.RunResult{Type: "PING", Total: 1})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.Run(context.Background(), "acme", RunOpts{
		Type:    models.TaskTypePing,
		Devices: []string{"fw-a"},
		Payload: map[string]interface{}{"target": "1.1.1.1"},
		// OUs intentionally nil — this is the case that produced targets.ous=null.
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := string(targets["ous"]); got != "[]" {
		t.Errorf("targets.ous = %s, want [] (must not be null)", got)
	}
	if got := string(targets["devices"]); got != `["fw-a"]` {
		t.Errorf(`targets.devices = %s, want ["fw-a"]`, got)
	}
}
