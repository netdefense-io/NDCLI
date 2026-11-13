package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Retained telemetry is asked for only when the caller asks: the live-only
// answer stays the default (#256).
func TestDeviceHealthAsksForRetainedTelemetryOnlyWhenTold(t *testing.T) {
	var query []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/devices/fw-01/telemetry" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		query = append(query, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		stale := ""
		if r.URL.Query().Get("retained") == "true" {
			stale = `,"stale":false`
		}
		_, _ = io.WriteString(w, `{"name":"fw-01","sync":{"state":"in_sync"},"as_of":1`+stale+`}`)
	}))
	defer srv.Close()
	svc := newTestService(t, srv)
	for _, retained := range []bool{false, true} {
		if _, err := svc.DeviceHealth(context.Background(), "acme", "fw-01", DeviceHealthOpts{Retained: retained}); err != nil {
			t.Fatal(err)
		}
	}
	if len(query) != 2 || query[0] != "" || query[1] != "retained=true" {
		t.Errorf("queries = %q, want none, then retained=true", query)
	}
}

// A server that predates retained telemetry ignores the parameter and answers
// live-only, with no stale key: that answer is refused, never shown as the
// last-known telemetry that was asked for.
func TestDeviceHealthRefusesAnIgnoredRetained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"fw-01","sync":{"state":"in_sync"},"as_of":1}`)
	}))
	defer srv.Close()
	_, err := newTestService(t, srv).DeviceHealth(context.Background(), "acme", "fw-01", DeviceHealthOpts{Retained: true})
	if ErrorCode(err) != CodeServerTooOld {
		t.Errorf("err = %v, want SERVER_TOO_OLD", err)
	}
}
