package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// connectStatusServer returns a test server that answers the two-step connect
// flow ConsoleConnect drives: POST .../connect → a task code, then
// GET /tasks/{code}/connect-status → COMPLETED with a payload and the given
// read_only field shape. includeReadOnly controls whether the field is present
// at all (false simulates an older NDManager); readOnly is its value when present.
func connectStatusServer(t *testing.T, includeReadOnly, readOnly bool) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"task":   "task-123",
				"status": "PENDING",
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/connect-status"):
			resp := map[string]interface{}{
				"task":    "task-123",
				"status":  "COMPLETED",
				"payload": `{"jti":"j1","pathfinder_session":"sess-abc"}`,
			}
			if includeReadOnly {
				resp["read_only"] = readOnly
			}
			json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return httptest.NewServer(handler)
}

// TestConsoleConnect_ReadOnlyTrue verifies the authoritative read_only=true flag
// from connect-status is surfaced on the result so console_open can fail early.
func TestConsoleConnect_ReadOnlyTrue(t *testing.T) {
	srv := connectStatusServer(t, true, true)
	defer srv.Close()
	svc := newTestService(t, srv)

	res, err := svc.ConsoleConnect(context.Background(), "org", "dev", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ReadOnly == nil {
		t.Fatal("expected ReadOnly to be set, got nil")
	}
	if !*res.ReadOnly {
		t.Fatal("expected ReadOnly=true")
	}
	if res.PathfinderSession != "sess-abc" {
		t.Errorf("expected pathfinder session sess-abc, got %q", res.PathfinderSession)
	}
}

// TestConsoleConnect_ReadOnlyFalse verifies an explicit read_only=false is
// surfaced as *false (session proceeds).
func TestConsoleConnect_ReadOnlyFalse(t *testing.T) {
	srv := connectStatusServer(t, true, false)
	defer srv.Close()
	svc := newTestService(t, srv)

	res, err := svc.ConsoleConnect(context.Background(), "org", "dev", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ReadOnly == nil {
		t.Fatal("expected ReadOnly to be set to false, got nil")
	}
	if *res.ReadOnly {
		t.Fatal("expected ReadOnly=false")
	}
}

// TestConsoleConnect_ReadOnlyAbsent verifies graceful degradation: when an older
// NDManager omits read_only, ReadOnly is nil and the caller proceeds normally.
func TestConsoleConnect_ReadOnlyAbsent(t *testing.T) {
	srv := connectStatusServer(t, false, false)
	defer srv.Close()
	svc := newTestService(t, srv)

	res, err := svc.ConsoleConnect(context.Background(), "org", "dev", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ReadOnly != nil {
		t.Fatalf("expected ReadOnly nil when the field is absent, got %v", *res.ReadOnly)
	}
}
