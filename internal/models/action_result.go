package models

import (
	"encoding/json"
	"errors"
)

// ActionResult is what a command that changes state reports: the human
// formats print Message, the line the command has always printed, then any
// Notes and Warnings, and -f json prints the facts, {"<resource>": "<name>",
// "action": "<verb>"} and Details beside them (#444).
//
// The contract every command keeps:
//   - Where the command has an MCP tool, the JSON carries the same top-level
//     keys with the same meaning as that tool's result, so a key never means
//     one thing on the CLI and another over MCP. It may add keys the tool
//     lacks. An object under a key is the model the server answered, whose
//     own keys can differ from the tool's summary of it.
//   - Details and Value hold the command's own arguments, or values decoded
//     by api.DecodeJSON, which has sanitized them, never something decoded a
//     second time.
//   - The resource's key holds the name the command was given (a rename adds
//     "new_name"), or Value when the tool returns the object there.
//   - Action is a past-tense snake_case verb: the tool's own where it has one
//     ("added", "removed"), with a related object under its own key.
//   - Details never uses the resource's key or "action": MarshalJSON writes
//     those last, so they win.
type ActionResult struct {
	Resource string                 // "ou", "device", ...
	Name     string                 // the resource's name
	Value    interface{}            // written under Resource in place of Name, when set
	Action   string                 // "created", "deleted", "renamed", ...
	Details  map[string]interface{} // more facts, as top-level JSON keys
	Message  string                 // the human line, "✓ OU created: branch"
	Notes    []string               // human lines printed after Message, uncoloured
	Warnings []string               // human lines printed after Notes and a blank line, in the warning colour
}

// MarshalJSON writes the facts; Message, Notes and Warnings are for the human
// formats (a warning a script needs goes in Details too).
func (r ActionResult) MarshalJSON() ([]byte, error) {
	if r.Resource == "" || r.Action == "" {
		return nil, errors.New("action result without a resource or an action")
	}
	out := make(map[string]interface{}, len(r.Details)+2)
	for k, v := range r.Details {
		out[k] = v
	}
	out[r.Resource] = r.Name
	if r.Value != nil {
		out[r.Resource] = r.Value
	}
	out["action"] = r.Action
	return json.Marshal(out)
}

// ApproveAllResult is what `device approve-all` reports: each pending device
// in the order it was tried, and whether it was approved. -f json is the
// ndcli.device.approve_all result: {"approved": N, "approved_names": [...],
// "failed_count": M, "failed": [{"device", "code", "error"}]}.
type ApproveAllResult struct {
	Results []ApproveOutcome
}

// ApproveOutcome is one device approve-all tried.
type ApproveOutcome struct {
	Device string
	Failed bool
	Code   string
	Error  string
}

// ApproveFailure is a failed approval as -f json shows it.
type ApproveFailure struct {
	Device string `json:"device"`
	Code   string `json:"code,omitempty"`
	Error  string `json:"error"`
}

// Counts is how many devices were approved and how many failed.
func (r ApproveAllResult) Counts() (approved, failed int) {
	for _, o := range r.Results {
		if o.Failed {
			failed++
		} else {
			approved++
		}
	}
	return approved, failed
}

// MarshalJSON writes the counts and the lists as ndcli.device.approve_all does.
func (r ApproveAllResult) MarshalJSON() ([]byte, error) {
	out := struct {
		Approved      int              `json:"approved"`
		ApprovedNames []string         `json:"approved_names"`
		FailedCount   int              `json:"failed_count"`
		Failed        []ApproveFailure `json:"failed"`
	}{ApprovedNames: []string{}, Failed: []ApproveFailure{}}
	for _, o := range r.Results {
		if o.Failed {
			out.Failed = append(out.Failed, ApproveFailure{Device: o.Device, Code: o.Code, Error: o.Error})
		} else {
			out.ApprovedNames = append(out.ApprovedNames, o.Device)
		}
	}
	out.Approved, out.FailedCount = len(out.ApprovedNames), len(out.Failed)
	return json.Marshal(out)
}
