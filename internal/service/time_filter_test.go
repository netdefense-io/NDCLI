package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestListTimeFiltersSendARelativeBoundInUTC: every list that takes a time
// filter sends a relative bound as a UTC instant with Z, whatever the host's
// zone. NDManager reads a bound without an offset as UTC, so the host's wall
// clock, here seven hours behind UTC, would move each bound by seven hours.
func TestListTimeFiltersSendARelativeBoundInUTC(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC-7", -7*3600)
	t.Cleanup(func() { time.Local = local })

	queries := map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries[r.URL.Path] = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": [], "total": 0, "page": 1, "per_page": 30, "pages": 0}`))
	}))
	defer srv.Close()
	svc := newTestService(t, srv)
	ctx := context.Background()

	start := time.Now().UTC().Truncate(time.Second)
	for name, call := range map[string]func() error{
		"devices": func() error {
			_, err := svc.DeviceList(ctx, "acme", DeviceListOpts{HeartbeatAfter: "30m", HeartbeatBefore: "1h",
				SyncedAfter: "2h", SyncedBefore: "1d", CreatedAfter: "7d", CreatedBefore: "1w"})
			return err
		},
		"ous": func() error {
			_, err := svc.OUList(ctx, "acme", OUListOpts{CreatedAfter: "30m", CreatedBefore: "1h"})
			return err
		},
		"snippets": func() error {
			_, err := svc.SnippetList(ctx, "acme", SnippetListOpts{CreatedAfter: "30m", CreatedBefore: "1h",
				UpdatedAfter: "2h", UpdatedBefore: "1d"})
			return err
		},
		"templates": func() error {
			_, err := svc.TemplateList(ctx, "acme", TemplateListOpts{CreatedAfter: "30m", CreatedBefore: "1h",
				UpdatedAfter: "2h", UpdatedBefore: "1d"})
			return err
		},
		"tasks": func() error {
			_, err := svc.TaskList(ctx, "acme", TaskListOpts{CreatedAfter: "30m", CreatedBefore: "1h"})
			return err
		},
	} {
		if err := call(); err != nil {
			t.Fatalf("%s list: %v", name, err)
		}
	}
	end := time.Now().UTC()

	for path, bounds := range map[string]map[string]time.Duration{
		"/api/v1/organizations/acme/devices": {
			"heartbeat_after": 30 * time.Minute, "heartbeat_before": time.Hour, "synced_after": 2 * time.Hour,
			"synced_before": 24 * time.Hour, "created_after": 7 * 24 * time.Hour, "created_before": 7 * 24 * time.Hour,
		},
		"/api/v1/organizations/acme/ous": {"created_after": 30 * time.Minute, "created_before": time.Hour},
		"/api/v1/organizations/acme/snippets": {
			"created_after": 30 * time.Minute, "created_before": time.Hour,
			"updated_after": 2 * time.Hour, "updated_before": 24 * time.Hour,
		},
		"/api/v1/organizations/acme/templates": {
			"created_after": 30 * time.Minute, "created_before": time.Hour,
			"updated_after": 2 * time.Hour, "updated_before": 24 * time.Hour,
		},
		"/api/v1/tasks": {"created_after": 30 * time.Minute, "created_before": time.Hour},
	} {
		query, ok := queries[path]
		if !ok {
			t.Errorf("no request reached %s", path)
			continue
		}
		for key, ago := range bounds {
			raw := query.Get(key)
			at, err := time.Parse(time.RFC3339, raw)
			if err != nil || !strings.HasSuffix(raw, "Z") {
				t.Errorf("%s %s = %q, want a UTC instant with Z", path, key, raw)
				continue
			}
			if at.Before(start.Add(-ago)) || at.After(end.Add(-ago)) {
				t.Errorf("%s %s = %s, want %s before now", path, key, raw, ago)
			}
		}
	}
}

// TestListTimeFiltersSendAnAbsoluteTimeAsTyped: a time the user wrote out is
// sent unchanged, offset and all, for NDManager to read.
func TestListTimeFiltersSendAnAbsoluteTimeAsTyped(t *testing.T) {
	svc, rec := newTaskListService(t)
	if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{
		CreatedAfter: "2026-10-18T09:00:00-03:00", CreatedBefore: "2026-10-18 21:00",
	}); err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if got := rec.query.Get("created_after"); got != "2026-10-18T09:00:00-03:00" {
		t.Errorf("created_after = %q, want it as typed", got)
	}
	if got := rec.query.Get("created_before"); got != "2026-10-18 21:00" {
		t.Errorf("created_before = %q, want it as typed", got)
	}
}
