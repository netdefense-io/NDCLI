package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// syncMessageEnvelope mirrors the JSON shape NDBroker stores in
// Tasks.message for SYNC tasks (see NDBroker
// routers/websocket.py, SYNC branch). Older or non-SYNC tasks just
// store a plain string, in which case the JSON parse fails and we fall
// back to rendering the raw text.
type syncMessageEnvelope struct {
	Message          string                 `json:"message"`
	Results          []syncResultEntry      `json:"results"`
	ValidationErrors []syncValidationEntry  `json:"validation_errors"`
}

type syncResultEntry struct {
	Type   string `json:"type"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
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
		Symbol string
		Type   string
		Name   string
		Err    string
	}
	rows := make([]row, 0, len(results))
	typeWidth := 0
	for _, r := range results {
		sym := symbolForResult(r)
		if sym == "" {
			continue
		}
		typeLabel := r.Type
		if len(typeLabel) > typeWidth {
			typeWidth = len(typeLabel)
		}
		rows = append(rows, row{Symbol: sym, Type: typeLabel, Name: r.Name, Err: r.Error})
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
		if r.Err != "" {
			fmt.Fprintf(b, "  %s %s%s  %s — %s\n", r.Symbol, r.Type, pad, r.Name, r.Err)
		} else {
			fmt.Fprintf(b, "  %s %s%s  %s\n", r.Symbol, r.Type, pad, r.Name)
		}
	}
}

func symbolForResult(r syncResultEntry) string {
	if r.Status != "" && r.Status != "success" && r.Status != "ok" {
		return "✗"
	}
	switch r.Action {
	case "create", "created":
		return "+"
	case "update", "updated":
		return "~"
	case "delete", "deleted":
		return "-"
	}
	return ""
}

func looksLikeJSONObject(s string) bool {
	return len(s) >= 2 && s[0] == '{' && s[len(s)-1] == '}'
}
