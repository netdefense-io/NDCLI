package models

import (
	"encoding/json"
	"fmt"
)

// PackageEditResult is what require-package, block-package and waive-package
// report: one outcome per package, in the order given, and the server's
// conflict notices. -f json is the ndcli.software.*_package result:
// {"policy", "op", "results": [{"package", "action", "from"}], "summary",
// "warnings"}, with warnings only when there are some.
type PackageEditResult struct {
	Policy   string
	Op       string // require, block or waive
	Outcomes []PackageActionOutcome
	Warnings []string
}

// Counts is how many packages changed state, moved from required to blocked
// or back, and were already as asked.
func (r PackageEditResult) Counts() (changed, moved, noop int) {
	for _, o := range r.Outcomes {
		switch o.Action {
		case "no-change":
			noop++
		case "moved":
			moved++
		default:
			changed++
		}
	}
	return changed, moved, noop
}

// Summary is the counts in the words the MCP result uses.
func (r PackageEditResult) Summary() string {
	changed, moved, noop := r.Counts()
	return fmt.Sprintf("%d change(s), %d move(s), %d no-op(s)", changed, moved, noop)
}

// MarshalJSON writes the MCP result's keys.
func (r PackageEditResult) MarshalJSON() ([]byte, error) {
	results := r.Outcomes
	if results == nil {
		results = []PackageActionOutcome{}
	}
	out := map[string]interface{}{"policy": r.Policy, "op": r.Op, "results": results, "summary": r.Summary()}
	if len(r.Warnings) > 0 {
		out["warnings"] = r.Warnings
	}
	return json.Marshal(out)
}

// EntryEditResult is what add-repo, remove-repo, add-external and
// remove-external report. -f json is the ndcli.software.set_repository (and
// remove_repository, set_external, remove_external) result: {"policy",
// "kind", "name", "action", "warnings"}, with warnings only when there are
// some.
type EntryEditResult struct {
	Policy   string
	Kind     string // repository or external
	Outcome  EntryActionOutcome
	Warnings []string
}

// MarshalJSON writes the MCP result's keys.
func (r EntryEditResult) MarshalJSON() ([]byte, error) {
	out := map[string]interface{}{"policy": r.Policy, "kind": r.Kind, "name": r.Outcome.Name, "action": r.Outcome.Action}
	if len(r.Warnings) > 0 {
		out["warnings"] = r.Warnings
	}
	return json.Marshal(out)
}
