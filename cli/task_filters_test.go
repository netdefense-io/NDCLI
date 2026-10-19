package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// runTaskListEchoing runs `task list` with args against a control plane whose
// answer echoes filters (no key when nil), and returns the query it sent.
func runTaskListEchoing(t *testing.T, filters map[string]any, args ...string) (map[string][]string, error) {
	t.Helper()
	prepareRootCommand()
	resetTaskListFilterFlags(t)

	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		body := map[string]any{"items": []any{}, "total": 0, "page": 1, "per_page": 30, "pages": 0}
		if filters != nil {
			body["filters"] = filters
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	origSvc, origOrg, origFormatter := svc, orgName, formatter
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName, formatter = "acme", &taskRecorder{}
	t.Cleanup(func() { svc, orgName, formatter = origSvc, origOrg, origFormatter })

	if err := taskListCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return query, runTaskList(taskListCmd, nil)
}

// echoOf is an NDManager 1.100 filters echo: every filter has its key.
func echoOf() map[string]any {
	echo := map[string]any{}
	for _, key := range []string{"organization", "device_name", "type", "status", "expired", "created_after",
		"created_before", "sort_by", "exclude_type", "origin", "schedule", "device", "q", "scheduled_after",
		"scheduled_before", "include_active", "run_id", "requested_by"} {
		echo[key] = nil
	}
	return echo
}

// TestTaskListSendsTheNewFilters covers each new flag end to end: runTaskList
// reads the flags and ignores the error, so a renamed flag would leave its
// filter silently unset.
func TestTaskListSendsTheNewFilters(t *testing.T) {
	query, err := runTaskListEchoing(t, echoOf(),
		"--exclude-type", "connect", "--origin", "SCHEDULE,firmware", "--schedule", "nightly-backup",
		"--device-exact", "e2e-a", "--device-exact", "e2e-b", "--search", "Pq3L",
		"--scheduled-after", "2026-10-18T09:00:00-03:00", "--scheduled-before", "2026-10-19T00:00:00Z",
		"--include-active", "--run", "R7x2Kp9Q", "--requested-by", "ana@acme.com", "--device", "^e2e-")
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
	for key, want := range map[string][]string{
		"exclude_type":     {"CONNECT"},
		"origin":           {"SCHEDULE", "FIRMWARE"},
		"schedule":         {"nightly-backup"},
		"device":           {"e2e-a", "e2e-b"},
		"q":                {"Pq3L"},
		"scheduled_after":  {"2026-10-18T12:00:00Z"},
		"scheduled_before": {"2026-10-19T00:00:00Z"},
		"include_active":   {"true"},
		"run_id":           {"R7x2Kp9Q"},
		"requested_by":     {"ana@acme.com"},
		"device_name":      {"^e2e-"},
	} {
		if got := query[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s sent as %v, want %v", key, got, want)
		}
	}
}

// TestTaskListRefusesAFilterAnOlderNDManagerIgnored: the command fails, with
// the code and nothing listed, rather than print the unfiltered list.
func TestTaskListRefusesAFilterAnOlderNDManagerIgnored(t *testing.T) {
	echo := echoOf()
	delete(echo, "run_id")
	delete(echo, "requested_by")
	_, err := runTaskListEchoing(t, echo, "--run", "R7x2Kp9Q")
	if err == nil {
		t.Fatal("task list --run against an NDManager without run_id succeeded")
	}
	if rec := formatter.(*taskRecorder); rec.tasks != nil {
		t.Errorf("tasks were rendered with the refusal: %+v", rec.tasks)
	}
	useFormatter(t, output.NewTableFormatter(), "")
	want := "Error: [SERVER_TOO_OLD] this NDManager predates the task list filter run_id: it did not apply it, so no list is shown\n"
	if got := printed(err); got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
	useFormatter(t, output.NewJSONFormatter(), "json")
	if doc := decodeCommandError(t, printed(err)); doc["code"] != "SERVER_TOO_OLD" {
		t.Errorf("-f json error = %v", doc)
	}
}

func TestTaskListNewFilterFlags(t *testing.T) {
	for _, tc := range []struct {
		flag, kind string
	}{
		{"exclude-type", "stringSlice"}, {"origin", "stringSlice"}, {"device-exact", "stringSlice"},
		{"schedule", "string"}, {"search", "string"}, {"scheduled-after", "string"}, {"scheduled-before", "string"},
		{"run", "string"}, {"requested-by", "string"}, {"include-active", "bool"},
	} {
		f := taskListCmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Errorf("task list has no --%s", tc.flag)
			continue
		}
		if f.Value.Type() != tc.kind {
			t.Errorf("--%s is a %s, want %s", tc.flag, f.Value.Type(), tc.kind)
		}
	}
	if want := "Filter by source, repeatable or comma-separated: MANUAL, SCHEDULE, FIRMWARE"; taskListCmd.Flags().Lookup("origin").Usage != want {
		t.Errorf("--origin usage = %q, want %q", taskListCmd.Flags().Lookup("origin").Usage, want)
	}

	for flag, list := range map[string][]string{"origin": models.TaskOrigins, "exclude-type": models.TaskTypes} {
		complete, ok := taskListCmd.GetFlagCompletionFunc(flag)
		if !ok {
			t.Fatalf("--%s has no completion registered", flag)
		}
		got, directive := complete(taskListCmd, nil, "")
		if !reflect.DeepEqual(got, list) || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("--%s completes %v (%v), want %v", flag, got, directive, list)
		}
	}
	for _, flag := range []string{"schedule", "device-exact", "requested-by"} {
		if _, ok := taskListCmd.GetFlagCompletionFunc(flag); !ok {
			t.Errorf("--%s has no completion registered", flag)
		}
	}
	if !strings.Contains(taskListCmd.Long, "SERVER_TOO_OLD") {
		t.Error("task list help does not say what an older control plane answers")
	}
}
