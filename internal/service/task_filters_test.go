package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The filters echo of GET /api/v1/tasks as each NDManager sends it: before
// 1.99 it names only the filters it always had; 1.99 adds exclude_type,
// origin, schedule, device, q, scheduled_after, scheduled_before and
// include_active; 1.100 adds run_id and requested_by.
var (
	echoBefore199 = map[string]any{
		"organization": "acme", "device_name": nil, "type": "all", "status": "all", "expired": nil,
		"created_after": nil, "created_before": nil, "sort_by": "created_at:desc",
	}
	echo199  = withKeys(echoBefore199, "exclude_type", "origin", "schedule", "device", "q", "scheduled_after", "scheduled_before", "include_active")
	echo1100 = withKeys(echo199, "run_id", "requested_by")
)

func withKeys(base map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(base)+len(keys))
	for k, v := range base {
		out[k] = v
	}
	for _, k := range keys {
		out[k] = nil
	}
	return out
}

// newEchoingTaskListService answers every list with filters (no key at all
// when filters is nil).
func newEchoingTaskListService(t *testing.T, filters map[string]any) (*Service, *taskListRecorder) {
	t.Helper()
	rec := &taskListRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		rec.rawQuery = r.URL.RawQuery
		rec.query = r.URL.Query()
		body := map[string]any{"items": []any{map[string]any{"task": "T1", "type": "PING", "status": "COMPLETED"}},
			"total": 1, "page": 1, "per_page": 30, "pages": 1}
		if filters != nil {
			body["filters"] = filters
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv), rec
}

func TestTaskList_SendsTheNewFilters(t *testing.T) {
	svc, rec := newEchoingTaskListService(t, echo1100)
	before := time.Now().UTC().Truncate(time.Second)

	result, err := svc.TaskList(context.Background(), "acme", TaskListOpts{
		ExcludeTypes:    []string{"connect", "PING,connect"},
		Origins:         []string{"schedule,firmware"},
		Schedule:        " nightly-backup ",
		Devices:         []string{"e2e-a", " e2e-b ,e2e-a", ""},
		Search:          " Pq3L ",
		ScheduledAfter:  "2026-10-18T09:00:00-03:00",
		ScheduledBefore: "+24h",
		IncludeActive:   true,
		RunID:           " R7x2Kp9Q ",
		RequestedBy:     " ana@acme.com ",
	})
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Errorf("tasks = %+v", result.Tasks)
	}

	for key, want := range map[string][]string{
		"exclude_type":    {"CONNECT", "PING"},
		"origin":          {"SCHEDULE", "FIRMWARE"},
		"schedule":        {"nightly-backup"},
		"device":          {"e2e-a", "e2e-b"},
		"q":               {"Pq3L"},
		"scheduled_after": {"2026-10-18T12:00:00Z"},
		"include_active":  {"true"},
		"run_id":          {"R7x2Kp9Q"},
		"requested_by":    {"ana@acme.com"},
	} {
		if got := rec.query[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s sent as %v, want %v", key, got, want)
		}
	}
	upper, err := time.Parse(time.RFC3339, rec.query.Get("scheduled_before"))
	if err != nil || !strings.HasSuffix(rec.query.Get("scheduled_before"), "Z") {
		t.Fatalf("scheduled_before = %q, want a UTC instant", rec.query.Get("scheduled_before"))
	}
	if d := upper.Sub(before); d < 24*time.Hour || d > 24*time.Hour+time.Minute {
		t.Errorf("scheduled_before +24h is %v from now", d)
	}
	// The pattern filter stays where it was.
	if _, sent := rec.query["device_name"]; sent {
		t.Errorf("an exact device must not be sent as the device_name pattern: %v", rec.query)
	}
}

// TestTaskList_OnlySendsTheNewFiltersItWasGiven: a list without them is the
// request it always was, and works against any NDManager.
func TestTaskList_OnlySendsTheNewFiltersItWasGiven(t *testing.T) {
	for name, echo := range map[string]map[string]any{"before 1.99": echoBefore199, "no echo at all": nil} {
		svc, rec := newEchoingTaskListService(t, echo)
		if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{Types: []string{"SYNC"}, CreatedAfter: "24h"}); err != nil {
			t.Fatalf("%s: TaskList: %v", name, err)
		}
		for _, key := range []string{"exclude_type", "origin", "schedule", "device", "q", "scheduled_after",
			"scheduled_before", "include_active", "run_id", "requested_by"} {
			if _, sent := rec.query[key]; sent {
				t.Errorf("%s: sent %s unasked: %s", name, key, rec.rawQuery)
			}
		}
	}
}

// TestTaskList_RefusesAListTheServerDidNotFilter: an NDManager that predates
// a filter ignores it and answers the unfiltered list; its echo has no key for
// the filter. TaskList refuses rather than return that list as filtered.
func TestTaskList_RefusesAListTheServerDidNotFilter(t *testing.T) {
	everything := TaskListOpts{
		ExcludeTypes: []string{"CONNECT"}, Origins: []string{"MANUAL"}, Schedule: "s", Devices: []string{"d"},
		Search: "q", ScheduledAfter: "2h", ScheduledBefore: "+2h", IncludeActive: true,
		RunID: "R7x2Kp9Q", RequestedBy: "ana@acme.com",
	}
	for _, tc := range []struct {
		name string
		echo map[string]any
		opts TaskListOpts
		want string // "" when the list is returned
	}{
		{"1.100 applies every filter", echo1100, everything, ""},
		{"1.99 does not have run_id", echo199, TaskListOpts{RunID: "R7x2Kp9Q"},
			"this NDManager predates the task list filter run_id: it did not apply it, so no list is shown"},
		{"1.99 does not have run_id or requested_by", echo199, everything,
			"this NDManager predates the task list filters requested_by, run_id: it did not apply them, so no list is shown"},
		{"1.99 applies its own filters", echo199, TaskListOpts{Origins: []string{"MANUAL"}, Search: "q", Devices: []string{"d"}}, ""},
		{"before 1.99 none of them", echoBefore199, TaskListOpts{Origins: []string{"MANUAL"}, Search: "q", Devices: []string{"d"}},
			"this NDManager predates the task list filters device_exact, origin, search: it did not apply them, so no list is shown"},
		{"no echo at all", nil, TaskListOpts{IncludeActive: true},
			"this NDManager predates the task list filter include_active: it did not apply it, so no list is shown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rec := newEchoingTaskListService(t, tc.echo)
			result, err := svc.TaskList(context.Background(), "acme", tc.opts)
			if tc.want == "" {
				if err != nil || result == nil {
					t.Fatalf("TaskList: %v", err)
				}
				return
			}
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld || svcErr.Message != tc.want {
				t.Fatalf("err = %#v, want %s %q", err, CodeServerTooOld, tc.want)
			}
			if ErrorCode(err) != CodeServerTooOld {
				t.Errorf("ErrorCode = %q", ErrorCode(err))
			}
			if result != nil {
				t.Errorf("a list was returned with the refusal: %+v", result)
			}
			if rec.hits != 1 {
				t.Errorf("requests = %d, want the one that showed the filter was ignored", rec.hits)
			}
		})
	}
}

func TestTaskList_RefusesABadNewFilterBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts TaskListOpts
		want string
	}{
		{"unknown origin", TaskListOpts{Origins: []string{"SCHEDULES"}},
			`unknown task origin "SCHEDULES" — valid values: MANUAL, SCHEDULE, FIRMWARE`},
		{"unknown excluded type", TaskListOpts{ExcludeTypes: []string{"CONECT"}}, `unknown task type "CONECT"`},
		{"excluding all", TaskListOpts{ExcludeTypes: []string{"SYNC,all"}}, "excluding all of them would list nothing"},
		{"a run id that is not one", TaskListOpts{RunID: "R7x2"}, `invalid run_id "R7x2": a run id is 8 letters or digits`},
		{"a run id with a symbol", TaskListOpts{RunID: "R7x2Kp9%"}, `invalid run_id "R7x2Kp9%"`},
		{"an unreadable time", TaskListOpts{ScheduledAfter: "tomorrow"}, `invalid scheduled_after value: could not parse "tomorrow"`},
		{"a time too far away", TaskListOpts{ScheduledBefore: "+999999999w"}, `invalid scheduled_before value: "+999999999w" is too far from now`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rec := newEchoingTaskListService(t, echo1100)
			_, err := svc.TaskList(context.Background(), "acme", tc.opts)
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || !strings.Contains(svcErr.Message, tc.want) {
				t.Fatalf("err = %#v, want %s containing %q", err, CodeInvalidInput, tc.want)
			}
			if rec.hits != 0 {
				t.Errorf("a refused filter still reached the server: %s", rec.rawQuery)
			}
		})
	}
}

func TestTaskList_AllMeansNoOriginFilter(t *testing.T) {
	svc, rec := newEchoingTaskListService(t, echoBefore199)
	if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{Origins: []string{"all"}}); err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if got := rec.query["origin"]; got != nil {
		t.Errorf("origin all sent as %v, want nothing", got)
	}
}

// TestTaskList_KeepsTheQueryShapeForTheNewFilters pins the whole query, so a
// filter sent under another key is caught.
func TestTaskList_KeepsTheQueryShapeForTheNewFilters(t *testing.T) {
	svc, rec := newEchoingTaskListService(t, echo1100)
	if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{
		Origins: []string{"MANUAL"}, RunID: "R7x2Kp9Q", ScheduledAfter: "2026-10-18T03:00:00",
	}); err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	want := url.Values{
		"organization": {"acme"}, "page": {"1"}, "per_page": {"30"},
		"origin": {"MANUAL"}, "run_id": {"R7x2Kp9Q"}, "scheduled_after": {"2026-10-18T03:00:00Z"},
	}
	if !reflect.DeepEqual(rec.query, want) {
		t.Errorf("query = %v, want %v", rec.query, want)
	}
}
