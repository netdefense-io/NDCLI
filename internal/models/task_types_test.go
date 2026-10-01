package models

import (
	"reflect"
	"slices"
	"testing"
)

// These are copies of NDDataModels' TaskType and TaskStatus enums
// (NDDataModels/DB.py; NDManager's openapi.json TaskType lists the same
// types). NDCLI-go's CI cannot read that repo, so the pin is a literal: when
// NDDataModels adds, renames or drops a value these tests fail until
// TaskTypes/TaskStatuses and this copy change together. That is also what
// keeps `task list` from refusing, as unknown, a type the server now returns.
var (
	nddataModelsTaskTypes = []string{
		"PING", "SHUTDOWN", "REBOOT", "RESTART", "PULL", "SYNC", "BACKUP", "CONNECT",
		"PLUGIN_INSTALL", "FIRMWARE_UPGRADE",
	}
	nddataModelsTaskStatuses = []string{
		"PENDING", "SCHEDULED", "IN_PROGRESS", "COMPLETED", "EXPIRED", "FAILED", "CANCELED",
	}
)

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func TestTaskTypesMatchNDDataModels(t *testing.T) {
	if got, want := sortedCopy(TaskTypes), sortedCopy(nddataModelsTaskTypes); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskTypes = %v, want the NDDataModels TaskType set %v", got, want)
	}
}

func TestTaskStatusesMatchNDDataModels(t *testing.T) {
	if got, want := sortedCopy(TaskStatuses), sortedCopy(nddataModelsTaskStatuses); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskStatuses = %v, want the NDDataModels TaskStatus set %v", got, want)
	}
	// The server writes CANCELED (one L); comparing against the two-L spelling
	// is what left three wait loops polling until their timeout.
	if TaskStatusCanceled != "CANCELED" {
		t.Errorf("TaskStatusCanceled = %q, want CANCELED", TaskStatusCanceled)
	}
}

func TestTaskStatusAliasesResolveToCanonicalStatuses(t *testing.T) {
	for alias, canonical := range TaskStatusAliases {
		if !slices.Contains(TaskStatuses, canonical) {
			t.Errorf("alias %q resolves to %q, which is not in TaskStatuses", alias, canonical)
		}
		if slices.Contains(TaskStatuses, alias) {
			t.Errorf("alias %q is itself a canonical status", alias)
		}
	}
	if TaskStatusAliases["CANCELLED"] != TaskStatusCanceled {
		t.Errorf("CANCELLED must resolve to %q", TaskStatusCanceled)
	}
}

func TestTaskFilterListsHaveNoDuplicates(t *testing.T) {
	for name, list := range map[string][]string{"TaskTypes": TaskTypes, "TaskStatuses": TaskStatuses} {
		seen := make(map[string]bool, len(list))
		for _, v := range list {
			if seen[v] {
				t.Errorf("%s contains %q more than once", name, v)
			}
			seen[v] = true
		}
	}
}
