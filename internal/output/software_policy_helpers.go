package output

import "encoding/json"

// softwarePolicyCountSet is the per-policy summary the list and
// describe views render. Everything is derived from the JSON content
// string — the API returns the document verbatim and does not send
// counts of its own.
type softwarePolicyCountSet struct {
	Present      int
	Absent       int
	Repositories int
	External     int
}

// HasEntries reports whether the policy uses the repository / external
// keys at all. Formatters use it to keep those columns off the output
// for the policies that don't — which, for anything written before
// these keys existed, is all of them.
func (c softwarePolicyCountSet) HasEntries() bool {
	return c.Repositories > 0 || c.External > 0
}

// softwarePolicyCounts cheaply extracts the list lengths from a
// SoftwarePolicy JSON content string for table/detailed/simple
// renders. Returns a zero set for content that fails to parse — bad
// rows shouldn't crash listings; the describe view will surface the raw
// content so the operator can see what's wrong.
func softwarePolicyCounts(content string) softwarePolicyCountSet {
	if content == "" {
		return softwarePolicyCountSet{}
	}
	var doc struct {
		Present      []json.RawMessage `json:"present"`
		Absent       []json.RawMessage `json:"absent"`
		Repositories []json.RawMessage `json:"repositories"`
		External     []json.RawMessage `json:"external"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return softwarePolicyCountSet{}
	}
	return softwarePolicyCountSet{
		Present:      len(doc.Present),
		Absent:       len(doc.Absent),
		Repositories: len(doc.Repositories),
		External:     len(doc.External),
	}
}
