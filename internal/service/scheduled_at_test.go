package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

// TestResolveScheduledAt_ExplicitOffsetKeepsTheInstant is the core of issue
// #217: a wall-clock time with a -03:00 offset must become the UTC instant it
// names, not the same digits relabelled as UTC.
func TestResolveScheduledAt_ExplicitOffsetKeepsTheInstant(t *testing.T) {
	resolved, err := ResolveScheduledAt("2126-09-21T23:00:00-03:00", time.UTC, "at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resolved.RFC3339UTC(); got != "2126-09-22T02:00:00Z" {
		t.Fatalf("wire value = %q, want 2126-09-22T02:00:00Z", got)
	}
	if got := resolved.RFC3339Zoned(); got != "2126-09-21T23:00:00-03:00" {
		t.Fatalf("zoned value = %q, want the input rendered back", got)
	}
	if resolved.Zone != "UTC-03:00" {
		t.Fatalf("zone = %q, want UTC-03:00", resolved.Zone)
	}
}

// TestResolveScheduledAt_BareTimestampUsesTheLocation pins that loc — not the
// host zone — interprets a bare timestamp.
func TestResolveScheduledAt_BareTimestampUsesTheLocation(t *testing.T) {
	loc := mustLoad(t, "America/Sao_Paulo") // UTC-03:00, no DST since 2019
	resolved, err := ResolveScheduledAt("2126-09-21 23:00", loc, "at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resolved.RFC3339UTC(); got != "2126-09-22T02:00:00Z" {
		t.Fatalf("wire value = %q, want 2126-09-22T02:00:00Z", got)
	}
	if resolved.Zone != "America/Sao_Paulo" {
		t.Fatalf("zone = %q, want America/Sao_Paulo", resolved.Zone)
	}
}

func TestResolveScheduledAt_RelativeOffsetIsUTC(t *testing.T) {
	loc := mustLoad(t, "America/Sao_Paulo")
	resolved, err := ResolveScheduledAt("2h", loc, "at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resolved.RFC3339UTC(); got[len(got)-1] != 'Z' {
		t.Fatalf("wire value = %q, want a Z-suffixed UTC instant", got)
	}
	delta := time.Until(resolved.UTC)
	if delta < 110*time.Minute || delta > 130*time.Minute {
		t.Fatalf("2h resolved to %v from now", delta)
	}
}

func TestResolveScheduledAt_EmptyResolvesToNothing(t *testing.T) {
	resolved, err := ResolveScheduledAt("   ", time.UTC, "at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != nil {
		t.Fatalf("expected nil for an empty input, got %+v", resolved)
	}
}

func TestResolveScheduledAt_PastIsRefused(t *testing.T) {
	_, err := ResolveScheduledAt("2020-01-01T00:00:00Z", time.UTC, "at")
	if err == nil {
		t.Fatal("expected a past timestamp to be refused")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *service.Error, got %T", err)
	}
	if svcErr.Code != CodeInvalidInput {
		t.Fatalf("code = %q, want %q", svcErr.Code, CodeInvalidInput)
	}
	if svcErr.Message != "at is in the past" {
		t.Fatalf("message = %q, want it to name the label", svcErr.Message)
	}
}

// TestResolveScheduledAt_SmallBackwardSkewIsTolerated keeps clock drift between
// the client and NDManager from rejecting a legitimate "now".
func TestResolveScheduledAt_SmallBackwardSkewIsTolerated(t *testing.T) {
	at := time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339)
	if _, err := ResolveScheduledAt(at, time.UTC, "at"); err != nil {
		t.Fatalf("a 5s-stale timestamp must be accepted, got %v", err)
	}
}

func TestResolveScheduledAt_LabelNamesTheSurface(t *testing.T) {
	_, err := ResolveScheduledAt("not-a-time", time.UTC, "--at")
	if err == nil {
		t.Fatal("expected a parse failure")
	}
	if got := err.Error(); len(got) < 5 || got[:5] != "--at:" {
		t.Fatalf("error = %q, want it prefixed with the CLI flag label", got)
	}
}

// TestRunNormalizesScheduledAt proves the normalization is reached through the
// service entry point, not only through the front-ends: whatever a caller puts
// in RunOpts.ScheduledAt, the wire carries a UTC instant.
func TestRunNormalizesScheduledAt(t *testing.T) {
	loc := mustLoad(t, "America/Sao_Paulo")
	for _, tc := range []struct {
		name string
		at   string
		loc  *time.Location
		want string
	}{
		{"explicit offset", "2126-09-21T23:00:00-03:00", nil, "2126-09-22T02:00:00Z"},
		{"bare in location", "2126-09-21 23:00", loc, "2126-09-22T02:00:00Z"},
		{"already utc", "2126-09-22T02:00:00Z", nil, "2126-09-22T02:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(models.RunResult{Type: "SHUTDOWN", Total: 1})
			})
			srv := httptest.NewServer(handler)
			defer srv.Close()

			svc := newTestService(t, srv)
			_, err := svc.Run(context.Background(), "acme", RunOpts{
				Type:                models.TaskTypeShutdown,
				Devices:             []string{"fw-a"},
				ScheduledAt:         tc.at,
				ScheduledAtLocation: tc.loc,
			})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got := string(body["scheduled_at"]); got != `"`+tc.want+`"` {
				t.Fatalf("wire scheduled_at = %s, want %q", got, tc.want)
			}
		})
	}
}

// TestRunRefusesAPastScheduledAt proves the past-time gate is enforced at the
// service layer, so no front-end can skip it.
func TestRunRefusesAPastScheduledAt(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.RunResult{Type: "SHUTDOWN", Total: 1})
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.Run(context.Background(), "acme", RunOpts{
		Type:        models.TaskTypeShutdown,
		Devices:     []string{"fw-a"},
		ScheduledAt: "2020-01-01T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected a past scheduled_at to be refused")
	}
	if hits != 0 {
		t.Fatalf("expected no request to be sent, got %d", hits)
	}
}

// TestRunRejectsAScheduleName and TestRunRegisterSpecRejectsAScheduledAt pin
// the mutual exclusion the RunOpts doc comment claims, client-side, instead of
// leaving it to a server 422.
func TestRunRejectsAScheduleName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent when a one-shot run carries a schedule")
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.Run(context.Background(), "acme", RunOpts{
		Type:     models.TaskTypeShutdown,
		Devices:  []string{"fw-a"},
		Schedule: "nightly",
	})
	if err == nil {
		t.Fatal("expected Run to refuse a schedule name")
	}
}

func TestRunRegisterSpecRejectsAScheduledAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent when a spec carries a scheduled instant")
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.RunRegisterSpec(context.Background(), "acme", RunOpts{
		Type:        models.TaskTypeShutdown,
		Devices:     []string{"fw-a"},
		Schedule:    "nightly",
		ScheduledAt: "2126-09-21T23:00:00-03:00",
	})
	if err == nil {
		t.Fatal("expected RunRegisterSpec to refuse a one-time instant")
	}
}
