package models

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SoftwarePolicy represents a reusable package inventory policy attached
// to templates. Content is a JSON document with `present` and `absent`
// string arrays; it's transported as an opaque string here so the CLI
// can render it as the user wrote it and the MCP can hand it back to the
// model without an intermediate Go shape rebuilding the JSON.
type SoftwarePolicy struct {
	Name         string       `json:"name"`
	Content      string       `json:"content,omitempty"`
	Organization string       `json:"organization_name,omitempty"`
	// TemplateNames is the list of templates this policy is currently
	// attached to. NDManager populates it on the single-policy GET
	// (describe) only — list endpoints omit it to avoid the JOIN on
	// every row. Nil here means "not loaded" (e.g. list response); an
	// empty slice means "explicitly not attached anywhere".
	TemplateNames []string     `json:"template_names,omitempty"`
	CreatedAt     FlexibleTime `json:"created_at"`
	UpdatedAt     FlexibleTime `json:"updated_at"`
}

// SoftwarePolicyListResponse mirrors the API's paginated list shape.
type SoftwarePolicyListResponse struct {
	Items []SoftwarePolicy `json:"items"`
	Total int              `json:"total"`
}

// EmptySoftwarePolicyContent is the canonical empty document — what
// `ndcli software create` produces when no content is supplied.
const EmptySoftwarePolicyContent = `{"present":[],"absent":[]}`

// SoftwarePolicyContent is the parsed form of the JSON document carried
// in SoftwarePolicy.Content. CLI mutation verbs (require/block/waive)
// work against this shape; the Marshal() method renders back to the
// canonical JSON the server stores.
type SoftwarePolicyContent struct {
	Present []string `json:"present"`
	Absent  []string `json:"absent"`
}

// ParseSoftwarePolicyContent is tolerant of missing keys and treats a
// nil array the same as an empty one — matches how the server-side
// validator accepts present-only or absent-only documents.
func ParseSoftwarePolicyContent(raw string) (*SoftwarePolicyContent, error) {
	if raw == "" {
		return &SoftwarePolicyContent{Present: []string{}, Absent: []string{}}, nil
	}
	var c SoftwarePolicyContent
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("parse software policy content: %w", err)
	}
	if c.Present == nil {
		c.Present = []string{}
	}
	if c.Absent == nil {
		c.Absent = []string{}
	}
	return &c, nil
}

// Marshal renders the content back to compact JSON with sorted lists.
// Sorting is intentional: it makes diffs between successive
// `require-package` / `block-package` calls trivial to read, and it
// matches the deterministic-payload posture NDManager's sync merge
// already takes on the wire (alphabetical).
func (c *SoftwarePolicyContent) Marshal() string {
	out := SoftwarePolicyContent{
		Present: append([]string{}, c.Present...),
		Absent:  append([]string{}, c.Absent...),
	}
	sort.Strings(out.Present)
	sort.Strings(out.Absent)
	if out.Present == nil {
		out.Present = []string{}
	}
	if out.Absent == nil {
		out.Absent = []string{}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// PackagePolicyState is where a package currently sits in a policy.
type PackagePolicyState string

const (
	PackageStateNone     PackagePolicyState = ""
	PackageStateRequired PackagePolicyState = "required"
	PackageStateBlocked  PackagePolicyState = "blocked"
)

// PackageActionOutcome describes what a require/block/waive call did
// to a single package. The CLI renders one line per outcome and the
// MCP returns the array verbatim so the LLM can summarize.
type PackageActionOutcome struct {
	Package string             `json:"package"`
	// Action is the user-visible verb: "required", "blocked", "waived",
	// "moved", or "no-change".
	Action string             `json:"action"`
	// From is the prior state when the action actually changed things —
	// useful for "Waived bash (was: required)" and move notices. Empty
	// when nothing changed or there was nothing prior.
	From PackagePolicyState `json:"from,omitempty"`
}

// Changed returns true when the underlying content was mutated. Callers
// use it to skip the PUT round-trip if every outcome was a no-op.
func (o PackageActionOutcome) Changed() bool {
	return o.Action != "no-change"
}

// Require ensures `pkg` is in the Present list. If it's currently in
// Absent, the package is moved (Absent → Present) and the outcome
// reports Action="moved", From="blocked". A package already in Present
// is a no-op.
func (c *SoftwarePolicyContent) Require(pkg string) PackageActionOutcome {
	if containsString(c.Present, pkg) {
		return PackageActionOutcome{Package: pkg, Action: "no-change", From: PackageStateRequired}
	}
	if containsString(c.Absent, pkg) {
		c.Absent = removeString(c.Absent, pkg)
		c.Present = append(c.Present, pkg)
		return PackageActionOutcome{Package: pkg, Action: "moved", From: PackageStateBlocked}
	}
	c.Present = append(c.Present, pkg)
	return PackageActionOutcome{Package: pkg, Action: "required"}
}

// Block ensures `pkg` is in the Absent list, symmetric to Require.
func (c *SoftwarePolicyContent) Block(pkg string) PackageActionOutcome {
	if containsString(c.Absent, pkg) {
		return PackageActionOutcome{Package: pkg, Action: "no-change", From: PackageStateBlocked}
	}
	if containsString(c.Present, pkg) {
		c.Present = removeString(c.Present, pkg)
		c.Absent = append(c.Absent, pkg)
		return PackageActionOutcome{Package: pkg, Action: "moved", From: PackageStateRequired}
	}
	c.Absent = append(c.Absent, pkg)
	return PackageActionOutcome{Package: pkg, Action: "blocked"}
}

// Waive removes `pkg` from whichever list it currently sits in. A
// package in neither list is a no-op (the outcome reports
// Action="no-change", From=""), which is intentional — the operator's
// intent is "I don't care about this package," and the policy already
// satisfies that.
func (c *SoftwarePolicyContent) Waive(pkg string) PackageActionOutcome {
	if containsString(c.Present, pkg) {
		c.Present = removeString(c.Present, pkg)
		return PackageActionOutcome{Package: pkg, Action: "waived", From: PackageStateRequired}
	}
	if containsString(c.Absent, pkg) {
		c.Absent = removeString(c.Absent, pkg)
		return PackageActionOutcome{Package: pkg, Action: "waived", From: PackageStateBlocked}
	}
	return PackageActionOutcome{Package: pkg, Action: "no-change"}
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeString(s []string, v string) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
