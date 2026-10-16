package cli

import (
	"encoding/json"
	"errors"
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

// TestTaskListFilterFlagsUseCanonicalLists guards `task list --type` and
// `--status` against a hand-edited list drifting from models.TaskTypes and
// models.TaskStatuses — the copy this replaced had 8 of the 10 types and spelled
// the status the server writes CANCELED as CANCELLED. Equality, not a substring
// check.
func TestTaskListFilterFlagsUseCanonicalLists(t *testing.T) {
	for _, tc := range []struct {
		flag, prefix string
		list         []string
	}{
		{"type", "Filter by task type, repeatable or comma-separated: ", models.TaskTypes},
		{"status", "Filter by status, repeatable or comma-separated: ", models.TaskStatuses},
	} {
		f := taskListCmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("task list has no --%s flag", tc.flag)
		}
		if want := tc.prefix + models.TaskValuesHelp(tc.list); f.Usage != want {
			t.Errorf("--%s usage = %q, want %q", tc.flag, f.Usage, want)
		}
		// StringSlice, not StringArray: --type SYNC,BACKUP has to read as two values.
		if f.Value.Type() != "stringSlice" {
			t.Errorf("--%s type = %q, want stringSlice", tc.flag, f.Value.Type())
		}
	}
}

// resetTaskListFilterFlags puts --type and --status back to unset. A
// StringSlice replaces its default on the first Set and appends after that, so
// a value left by one test would leak into the next.
func resetTaskListFilterFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, name := range []string{"type", "status"} {
			f := taskListCmd.Flags().Lookup(name)
			if err := f.Value.(interface{ Replace([]string) error }).Replace(nil); err != nil {
				t.Fatalf("reset --%s: %v", name, err)
			}
			f.Changed = false
		}
	}
	reset()
	t.Cleanup(reset)
}

// taskRecorder is a formatter that keeps what the command handed it.
type taskRecorder struct {
	output.Formatter
	tasks []models.Task
}

func (r *taskRecorder) FormatTasks(tasks []models.Task, total int) error {
	r.tasks = tasks
	return nil
}

// runTaskListAgainst runs `task list` with args against a fake control plane
// and returns the query it sent (nil when nothing was), what the formatter got
// and the command's error.
func runTaskListAgainst(t *testing.T, args ...string) (query map[string][]string, rec *taskRecorder, err error) {
	t.Helper()
	prepareRootCommand()
	resetTaskListFilterFlags(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []any{map[string]any{"task": "T1", "type": "SYNC", "status": "FAILED"}},
			"total": 1, "page": 1, "per_page": 30, "pages": 1,
		})
	}))
	t.Cleanup(srv.Close)

	origSvc, origOrg, origFormatter := svc, orgName, formatter
	rec = &taskRecorder{}
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName, formatter = "acme", rec
	t.Cleanup(func() { svc, orgName, formatter = origSvc, origOrg, origFormatter })

	if err := taskListCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	err = runTaskList(taskListCmd, nil)
	return query, rec, err
}

// TestTaskListSendsRepeatedAndCommaSeparatedFilters covers the flag wiring end
// to end: runTaskList reads the flags with GetStringSlice and ignores the
// error, so a rename on either side would leave the filters silently unset.
func TestTaskListSendsRepeatedAndCommaSeparatedFilters(t *testing.T) {
	query, rec, err := runTaskListAgainst(t,
		"--type", "SYNC", "--type", "backup,PING", "--status", "FAILED,expired")
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
	if want := []string{"SYNC", "BACKUP", "PING"}; !reflect.DeepEqual(query["type"], want) {
		t.Errorf("type sent as %v, want %v", query["type"], want)
	}
	if want := []string{"FAILED", "EXPIRED"}; !reflect.DeepEqual(query["status"], want) {
		t.Errorf("status sent as %v, want %v", query["status"], want)
	}
	if len(rec.tasks) != 1 || rec.tasks[0].ID != "T1" {
		t.Errorf("the formatter got %+v, want the listed task", rec.tasks)
	}
}

func TestTaskListAllMeansNoFilter(t *testing.T) {
	query, _, err := runTaskListAgainst(t, "--type", "all", "--status", "")
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
	if len(query["type"]) != 0 || len(query["status"]) != 0 {
		t.Errorf("all/empty must send no filter, sent type=%v status=%v", query["type"], query["status"])
	}
}

func TestTaskListRefusesAnUnknownFilterBeforeSending(t *testing.T) {
	query, rec, err := runTaskListAgainst(t, "--type", "BOGUS")

	var svcErr *service.Error
	if !errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput {
		t.Fatalf("err = %#v, want %s", err, service.CodeInvalidInput)
	}
	if !strings.Contains(err.Error(), "FIRMWARE_UPGRADE") {
		t.Errorf("the error should list the valid types:\n%s", err)
	}
	if query != nil || rec.tasks != nil {
		t.Errorf("nothing may be sent or rendered: query=%v tasks=%v", query, rec.tasks)
	}
}

func TestCompleteValueList(t *testing.T) {
	for _, tc := range []struct {
		name       string
		toComplete string
		want       []string
	}{
		{"nothing typed offers every value", "", models.TaskTypes},
		{"a prefix narrows", "S", []string{"SHUTDOWN", "SYNC"}},
		{"case is ignored", "sy", []string{"SYNC"}},
		{"after a comma the rest are offered, kept behind what was typed", "SYNC,",
			[]string{"SYNC,BACKUP", "SYNC,CONNECT", "SYNC,FIRMWARE_TRACK", "SYNC,FIRMWARE_UPGRADE", "SYNC,PING",
				"SYNC,PLUGIN_INSTALL", "SYNC,PULL", "SYNC,REBOOT", "SYNC,RESTART", "SYNC,SHUTDOWN"}},
		{"the last element is completed", "SYNC,PU", []string{"SYNC,PULL"}},
		{"what was typed is left as typed", "sync,ping,pl", []string{"sync,ping,PLUGIN_INSTALL"}},
		{"a value already chosen is not offered again", "SYNC,SYN", nil},
		{"no match", "zzz", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := completeValueList(models.TaskTypes, tc.toComplete); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeValueList(%q) = %v, want %v", tc.toComplete, got, tc.want)
			}
		})
	}
}

// TestTaskListFilterFlagsCompleteFromTheCanonicalLists checks the completion
// actually registered on the flags, not just the helper behind it.
func TestTaskListFilterFlagsCompleteFromTheCanonicalLists(t *testing.T) {
	for flag, list := range map[string][]string{"type": models.TaskTypes, "status": models.TaskStatuses} {
		complete, ok := taskListCmd.GetFlagCompletionFunc(flag)
		if !ok {
			t.Fatalf("--%s has no completion registered", flag)
		}
		got, directive := complete(taskListCmd, nil, "")
		if !reflect.DeepEqual(got, list) {
			t.Errorf("--%s completes %v, want %v", flag, got, list)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("--%s directive = %v, want NoFileComp", flag, directive)
		}
	}
}
