package tui

import (
	"strings"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// formModel collects an action's FormField inputs in a modal. Text fields
// accept typed input; select fields (Options set) cycle with ←/→. tab/↑/↓ move
// between fields, enter submits, esc cancels.
type formModel struct {
	act    registry.Action
	target string
	values []string
	cursor int
	err    string
}

func newForm(act registry.Action, target string) *formModel {
	f := &formModel{act: act, target: target, values: make([]string, len(act.Form))}
	for i, fld := range act.Form {
		switch {
		case len(fld.Options) > 0:
			f.values[i] = fld.Options[selectIndex(fld)]
		default:
			f.values[i] = fld.Default
		}
	}
	return f
}

// selectIndex returns the starting option index for a select field (its
// Default if present, else 0).
func selectIndex(fld registry.FormField) int {
	for i, o := range fld.Options {
		if o == fld.Default {
			return i
		}
	}
	return 0
}

func (f *formModel) cur() registry.FormField { return f.act.Form[f.cursor] }

func (f *formModel) cycle(delta int) {
	opts := f.cur().Options
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, o := range opts {
		if o == f.values[f.cursor] {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(opts)) % len(opts)
	f.values[f.cursor] = opts[idx]
}

// handleKey processes a key. done reports the modal should close; submit is
// only meaningful when done, and args carries the collected values.
func (f *formModel) handleKey(key string) (done, submit bool, args map[string]string) {
	switch key {
	case "esc":
		return true, false, nil
	case "tab", "down":
		f.cursor = (f.cursor + 1) % len(f.act.Form)
	case "shift+tab", "up":
		f.cursor = (f.cursor - 1 + len(f.act.Form)) % len(f.act.Form)
	case "left":
		f.cycle(-1)
	case "right":
		f.cycle(1)
	case "enter":
		for i, fld := range f.act.Form {
			if fld.Required && strings.TrimSpace(f.values[i]) == "" {
				f.err = fld.Label + " is required"
				f.cursor = i
				return false, false, nil
			}
		}
		out := make(map[string]string, len(f.act.Form))
		for i, fld := range f.act.Form {
			out[fld.Key] = strings.TrimSpace(f.values[i])
		}
		return true, true, out
	case "backspace":
		if len(f.cur().Options) == 0 {
			if r := []rune(f.values[f.cursor]); len(r) > 0 {
				f.values[f.cursor] = string(r[:len(r)-1])
			}
		}
	default:
		if len(f.cur().Options) == 0 && len(key) == 1 {
			f.values[f.cursor] += key
		}
	}
	return false, false, nil
}

func (f *formModel) View() string {
	title := f.act.Label
	if f.target != "" {
		title += " · " + f.target
	}
	lines := []string{titleStyle.Render(title), ""}
	for i, fld := range f.act.Form {
		label := mutedStyle.Render(fitCell(fld.Label, 12))
		var val string
		if len(fld.Options) > 0 {
			val = "‹ " + f.values[i] + " ›"
		} else if f.values[i] == "" && fld.Placeholder != "" {
			val = dimStyle.Render(fld.Placeholder)
		} else {
			val = f.values[i]
		}
		if i == f.cursor {
			if len(fld.Options) == 0 {
				val += "▌"
			}
			lines = append(lines, keyStyle.Render("› ")+label+"  "+val)
		} else {
			lines = append(lines, "  "+label+"  "+val)
		}
	}
	if f.err != "" {
		lines = append(lines, "", errStyle.Render(f.err))
	}
	lines = append(lines, "", mutedStyle.Render("tab/↑↓ move · ←/→ change · enter run · esc cancel"))
	return modalStyle.Render(strings.Join(lines, "\n"))
}
