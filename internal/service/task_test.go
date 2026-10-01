package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
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

// taskListRecorder is what the fake list endpoint saw.
type taskListRecorder struct {
	hits     int
	rawQuery string
	query    url.Values
}

func newTaskListService(t *testing.T) (*Service, *taskListRecorder) {
	t.Helper()
	rec := &taskListRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		rec.hits++
		rec.rawQuery = r.URL.RawQuery
		rec.query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "per_page": 30, "pages": 0})
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv), rec
}

// TestTaskList_SendsEachFilterValueAsARepeatedParameter is the wire shape
// NDManager reads (type: List[str]): the key repeated, never comma-joined.
func TestTaskList_SendsEachFilterValueAsARepeatedParameter(t *testing.T) {
	svc, rec := newTaskListService(t)

	_, err := svc.TaskList(context.Background(), "acme", TaskListOpts{
		Types:    []string{"SYNC", "BACKUP"},
		Statuses: []string{"FAILED", "EXPIRED"},
	})
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	for _, want := range []string{"status=FAILED&status=EXPIRED", "type=SYNC&type=BACKUP"} {
		if !strings.Contains(rec.rawQuery, want) {
			t.Errorf("query %q does not contain %q", rec.rawQuery, want)
		}
	}
}

func TestTaskList_NormalizesFilterInput(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		types, statuses         []string
		wantTypes, wantStatuses []string
	}{
		{name: "lower case is upper-cased", types: []string{"firmware_upgrade"}, statuses: []string{"failed"},
			wantTypes: []string{"FIRMWARE_UPGRADE"}, wantStatuses: []string{"FAILED"}},
		{name: "a comma-separated element is split", types: []string{"sync,backup"},
			wantTypes: []string{"SYNC", "BACKUP"}},
		{name: "spaces and empty elements are dropped", types: []string{" sync , ", ""},
			wantTypes: []string{"SYNC"}},
		{name: "duplicates collapse, first occurrence wins the order", types: []string{"SYNC", "sync", "SYNC,PING"},
			wantTypes: []string{"SYNC", "PING"}},
		{name: "CANCELLED is the CANCELED alias", statuses: []string{"cancelled"},
			wantStatuses: []string{"CANCELED"}},
		{name: "both spellings collapse to one value", statuses: []string{"CANCELED", "cancelled"},
			wantStatuses: []string{"CANCELED"}},
		{name: "all means no filter", types: []string{"all"}, statuses: []string{"ALL"}},
		{name: "empty means no filter", types: []string{""}, statuses: nil},
		{name: "all outweighs the other values", types: []string{"all", "SYNC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rec := newTaskListService(t)
			if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{Types: tc.types, Statuses: tc.statuses}); err != nil {
				t.Fatalf("TaskList: %v", err)
			}
			if got := rec.query["type"]; !reflect.DeepEqual(got, tc.wantTypes) {
				t.Errorf("type sent as %v, want %v", got, tc.wantTypes)
			}
			if got := rec.query["status"]; !reflect.DeepEqual(got, tc.wantStatuses) {
				t.Errorf("status sent as %v, want %v", got, tc.wantStatuses)
			}
		})
	}
}

// TestTaskList_AcceptsEveryCanonicalValue covers the two the old hand-typed
// list left out, CONNECT and FIRMWARE_UPGRADE, and everything else with them.
func TestTaskList_AcceptsEveryCanonicalValue(t *testing.T) {
	svc, rec := newTaskListService(t)

	for _, ty := range models.TaskTypes {
		if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{Types: []string{ty}}); err != nil {
			t.Errorf("type %s refused: %v", ty, err)
		} else if got := rec.query["type"]; !reflect.DeepEqual(got, []string{ty}) {
			t.Errorf("type %s sent as %v", ty, got)
		}
	}
	for _, st := range models.TaskStatuses {
		if _, err := svc.TaskList(context.Background(), "acme", TaskListOpts{Statuses: []string{st}}); err != nil {
			t.Errorf("status %s refused: %v", st, err)
		} else if got := rec.query["status"]; !reflect.DeepEqual(got, []string{st}) {
			t.Errorf("status %s sent as %v", st, got)
		}
	}
}

// TestTaskList_RejectsUnknownFilterValuesBeforeSending: NDManager drops a
// value it does not recognise and, with none left, applies no filter — so a
// typo used to come back as the whole list with exit 0.
func TestTaskList_RejectsUnknownFilterValuesBeforeSending(t *testing.T) {
	typeList := models.TaskValuesHelp(models.TaskTypes)
	statusList := models.TaskValuesHelp(models.TaskStatuses)

	for _, tc := range []struct {
		name string
		opts TaskListOpts
		want string
	}{
		{"unknown type", TaskListOpts{Types: []string{"BOGUS"}},
			fmt.Sprintf(`unknown task type "BOGUS" — valid values: %s`, typeList)},
		{"unknown status", TaskListOpts{Statuses: []string{"nope"}},
			fmt.Sprintf(`unknown task status "nope" — valid values: %s`, statusList)},
		{"unknown value inside a comma list", TaskListOpts{Types: []string{"SYNC,SYNCC"}},
			fmt.Sprintf(`unknown task type "SYNCC" — valid values: %s`, typeList)},
		{"all does not excuse another value", TaskListOpts{Statuses: []string{"all", "nope"}},
			fmt.Sprintf(`unknown task status "nope" — valid values: %s`, statusList)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, rec := newTaskListService(t)

			_, err := svc.TaskList(context.Background(), "acme", tc.opts)

			svcErr, ok := err.(*Error)
			if !ok || svcErr.Code != CodeInvalidInput {
				t.Fatalf("err = %#v, want *Error with %s", err, CodeInvalidInput)
			}
			if svcErr.Message != tc.want {
				t.Errorf("message = %q, want %q", svcErr.Message, tc.want)
			}
			if rec.hits != 0 {
				t.Errorf("a rejected filter still reached the server (%d requests)", rec.hits)
			}
		})
	}

	if !strings.Contains(typeList, "FIRMWARE_UPGRADE") || !strings.Contains(statusList, "CANCELED") {
		t.Errorf("the lists in the message are missing FIRMWARE_UPGRADE or CANCELED: %s | %s", typeList, statusList)
	}
}

// TestTaskList_KeepsTheOtherQueryParameters guards the rest of the query,
// which moved from a one-value-per-key map to url.Values with the filters.
func TestTaskList_KeepsTheOtherQueryParameters(t *testing.T) {
	svc, rec := newTaskListService(t)

	_, err := svc.TaskList(context.Background(), "acme", TaskListOpts{
		Device:        "fw-.*",
		Expired:       false,
		ExpiredSet:    true,
		CreatedAfter:  "2026-01-02T03:04:05Z",
		CreatedBefore: "2026-02-03",
		SortBy:        "created_at:asc",
		Page:          2,
		PerPage:       50,
	})
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}

	want := url.Values{
		"organization":   {"acme"},
		"device_name":    {"fw-.*"},
		"expired":        {"false"},
		"created_after":  {"2026-01-02T03:04:05Z"},
		"created_before": {"2026-02-03"},
		"sort_by":        {"created_at:asc"},
		"page":           {"2"},
		"per_page":       {"50"},
	}
	if !reflect.DeepEqual(rec.query, want) {
		t.Errorf("query = %v, want %v", rec.query, want)
	}

	// No filters, no organization: none of those keys is sent at all.
	if _, err := svc.TaskList(context.Background(), "", TaskListOpts{}); err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if want := (url.Values{"page": {"1"}, "per_page": {"30"}}); !reflect.DeepEqual(rec.query, want) {
		t.Errorf("unfiltered query = %v, want %v", rec.query, want)
	}
}
