package output

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/sanitize"
)

// syncMessageEnvelope mirrors the JSON shape NDBroker stores in
// Tasks.message for SYNC tasks (see NDBroker
// routers/websocket.py, SYNC branch). Older or non-SYNC tasks just
// store a plain string, in which case the JSON parse fails and we fall
// back to rendering the raw text.
type syncMessageEnvelope struct {
	Message          string                `json:"message"`
	Results          []syncResultEntry     `json:"results"`
	ValidationErrors []syncValidationEntry `json:"validation_errors"`
}

type syncResultEntry struct {
	Type   string `json:"type"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Code, Before, After, Available and Risks are additive fields of
	// NDAgent's SyncAPIItemResult. Code is the structured outcome code a
	// consumer keys on instead of parsing Error (AUTH, TRUST, RULE and ALIAS
	// items). Before/After are an auth_facility item's kept/written order
	// (names only) or a renumbered local rule's old/new sequence. Available
	// is the resolution set an unresolved order entry was checked against,
	// reported only on a refusal. Risks is the auth_local_server warning's
	// structured risk list.
	Code      string   `json:"code,omitempty"`
	Before    []string `json:"before,omitempty"`
	After     []string `json:"after,omitempty"`
	Available []string `json:"available,omitempty"`
	Risks     []string `json:"risks,omitempty"`
}

type syncValidationEntry struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// FormatTaskMessage returns a human-readable rendering of a task's
// Message field. When the message is the SYNC JSON envelope the result
// is a summary line plus a per-change list, with validation errors
// appended when present. For non-SYNC or unparseable messages the raw
// string is returned unchanged so older tasks still render.
func FormatTaskMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !looksLikeJSONObject(raw) {
		return raw
	}
	var env syncMessageEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return raw
	}
	// Task.Message is JSON-as-a-string: the outer api.DecodeJSON/
	// sanitize.Struct pass on the Task only ran sanitize.String on that
	// outer string field, so any \uXXXX escape inside it (a device-
	// controlled name, an AUTH result's before/after/available/risks)
	// was still literal escape text at that point, not yet the control
	// byte it decodes to. This second Unmarshal is what turns the escape
	// into a live byte, so it needs its own sanitize pass before
	// anything here reaches a formatter.
	sanitize.Struct(reflect.ValueOf(&env))
	// Heuristic: treat as a SYNC envelope only when at least one of the
	// structured fields is populated. A bare {"message": "..."} string
	// could be anything, so don't claim the format unless it carries
	// the sync-shaped detail we know how to render.
	if len(env.Results) == 0 && len(env.ValidationErrors) == 0 {
		if env.Message == "" {
			return raw
		}
		return env.Message
	}

	var b strings.Builder
	if env.Message != "" {
		b.WriteString(env.Message)
	}

	if len(env.Results) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Changes:\n")
		writeResultLines(&b, env.Results)
	}

	if len(env.ValidationErrors) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Validation errors:\n")
		for _, v := range env.ValidationErrors {
			fmt.Fprintf(&b, "  • %s %s: %s\n", v.Type, v.Name, v.Message)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func writeResultLines(b *strings.Builder, results []syncResultEntry) {
	type row struct {
		Symbol    string
		Type      string
		Name      string
		Err       string
		Action    string
		Code      string
		Before    []string
		After     []string
		Available []string
		Risks     []string
	}
	rows := make([]row, 0, len(results))
	typeWidth := 0
	for _, r := range results {
		sym := symbolForResult(r)
		typeLabel := r.Type
		if len(typeLabel) > typeWidth {
			typeWidth = len(typeLabel)
		}
		rows = append(rows, row{
			Symbol: sym, Type: typeLabel, Name: r.Name, Err: r.Error, Action: r.Action,
			Code: r.Code, Before: r.Before, After: r.After, Available: r.Available, Risks: r.Risks,
		})
	}

	// Stable order: errors first, then by type/name. Keeps the eye on
	// failures regardless of the order the device reported them.
	sort.SliceStable(rows, func(i, j int) bool {
		errI := rows[i].Symbol == "✗"
		errJ := rows[j].Symbol == "✗"
		if errI != errJ {
			return errI
		}
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Name < rows[j].Name
	})

	for _, r := range rows {
		pad := strings.Repeat(" ", typeWidth-len(r.Type))
		name := r.Name
		// A trust_reload row's name is only the consumer's description; its
		// action is the reload that ran ("openvpn configure", "ipsec
		// reload") or "scheduled", which is what says what kind of service
		// it was. "none" (no known reload) is left to the warning text.
		if r.Type == "trust_reload" && r.Action != "" && r.Action != "none" {
			name += " (" + r.Action + ")"
		}
		if r.Err != "" {
			fmt.Fprintf(b, "  %s %s%s  %s — %s\n", r.Symbol, r.Type, pad, name, r.Err)
		} else {
			fmt.Fprintf(b, "  %s %s%s  %s\n", r.Symbol, r.Type, pad, name)
		}
		for _, extra := range resultDetailLines(r.Action, r.Code, r.Before, r.After, r.Available, r.Risks) {
			fmt.Fprintf(b, "      %s\n", extra)
		}
	}
}

// resultDetailLines renders the additive result fields a plain
// Type/Action/Status/Error line doesn't otherwise show: Before/After (an
// auth_facility item's kept/written order, or a renumbered local rule's
// old/new sequence), Available (the resolution set an unresolved order
// entry was checked against, on a refusal only), Risks (an
// auth_local_server warning's structured risk list) and Code (the
// structured outcome code). Names and sequence numbers only, never values.
// TRUST, ALIAS and most RULE items carry at most Code, so for them this is
// at most the "code: ..." line.
//
// Two things are suppressed as noise rather than detail: the AUTH helper
// reports code "OK" on every successful write/unchanged/delete, so
// printing it would put a redundant "code: OK" under nearly every
// AUTH_SERVER/AUTH_ORDER row; and an "unchanged" item's before/after are
// always identical by definition, so showing both says nothing the "="
// symbol on the row above doesn't already say.
func resultDetailLines(action, code string, before, after, available, risks []string) []string {
	var lines []string
	if (len(before) > 0 || len(after) > 0) && action != "unchanged" {
		lines = append(lines, fmt.Sprintf("before: [%s]  after: [%s]", strings.Join(before, ", "), strings.Join(after, ", ")))
	}
	if len(available) > 0 {
		lines = append(lines, "available: "+strings.Join(available, ", "))
	}
	if len(risks) > 0 {
		lines = append(lines, "risks: "+strings.Join(risks, ", "))
	}
	if code != "" && code != "OK" {
		lines = append(lines, "code: "+code)
	}
	return lines
}

// symbolForResult maps a result entry's action to a one-character
// marker. Two families arrive here: config sync uses lowercase verbs
// (create/update/delete), and the software-policy path on NDAgent uses
// SCREAMING_SNAKE constants (INSTALLED, REPO_CONFIGURED, ...).
//
// An action this build does not recognise gets a neutral bullet rather
// than being dropped. Dropping it meant a newer agent's results
// rendered as an empty change list, which reads as "nothing happened"
// rather than "this build does not know that word" — and it is why an
// installed package was invisible in `task describe` for a while.
func symbolForResult(r syncResultEntry) string {
	// A "warning" item is never a failure, in any family: NDAgent uses the
	// status for something the sync did not break but someone should know
	// (a hand-made server named in an AUTH order, a TRUST reload that
	// failed, a RULE placement OPNsense's sections overrule, a failed pkg
	// catalog refresh), and the task still completes. It has to be checked
	// before the generic "any non-success/ok status is a failure" rule
	// below, or a warning would render as ✗ and read as broken. Any other
	// status this build does not know still falls through to ✗.
	if r.Status == "warning" {
		return "⚠"
	}
	if r.Status != "" && r.Status != "success" && r.Status != "ok" {
		return "✗"
	}
	// A trust_reload item reports a service restarted (or scheduled to
	// restart) for a renewed CA or certificate; it says so by its type,
	// whatever verb the action carries.
	if r.Type == "trust_reload" {
		return "↻"
	}
	switch strings.ToLower(r.Action) {
	// Config sync
	case "create", "created":
		return "+"
	case "update", "updated":
		return "~"
	case "delete", "deleted":
		return "-"
	// unchanged: the device already held the object as desired (AUTH,
	// TRUST, RULE, ALIAS); written: an AUTH_ORDER facility was rewritten.
	// Only reached with status success/ok/"".
	case "unchanged":
		return "="
	case "written":
		return "~"
	// A local rule whose sequence RULE placement raised so the PREPEND
	// rules of its section come first: a change to the device, counted as
	// one in the summary line; before/after carry both sequences.
	case "renumbered":
		return "~"

	// Package reconcile
	case "installed":
		return "+"
	case "removed":
		return "-"
	case "already_present", "already_absent":
		return "="
	case "not_found", "invalid_name", "error":
		return "✗"

	// Custom repositories and external packages
	case "repo_configured":
		return "+"
	case "repo_removed":
		return "-"
	case "repo_unchanged":
		return "="
	// REPO_CONFLICT: a repository this policy does not manage already
	// defines the name. SHADOWED: more than one configured repository
	// offers the package, so which one pkg picks is not determined by
	// the policy. The agent refuses both rather than guessing.
	case "repo_conflict", "shadowed":
		return "✗"
	}
	return "•"
}

func looksLikeJSONObject(s string) bool {
	return len(s) >= 2 && s[0] == '{' && s[len(s)-1] == '}'
}
