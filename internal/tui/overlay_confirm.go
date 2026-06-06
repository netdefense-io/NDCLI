package tui

import (
	"strings"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// confirmModel is the destructive-action gate. Standard actions use y/n;
// high-blast-radius actions (BlastRadius set) require typing "yes".
type confirmModel struct {
	act    registry.Action
	target string
	typed  string
}

func newConfirm(act registry.Action, target string) *confirmModel {
	return &confirmModel{act: act, target: target}
}

func (c *confirmModel) needsType() bool { return c.act.BlastRadius != "" }

func (c *confirmModel) prompt() string {
	p := c.act.Prompt
	if p == "" {
		p = c.act.Label + " " + c.target + "?"
	}
	return strings.ReplaceAll(p, "{id}", c.target)
}

// handleKey processes a key. done reports the modal should close; confirmed is
// only meaningful when done is true.
func (c *confirmModel) handleKey(key string) (done, confirmed bool) {
	if c.needsType() {
		switch key {
		case "esc":
			return true, false
		case "enter":
			return true, strings.EqualFold(strings.TrimSpace(c.typed), "yes")
		case "backspace":
			if r := []rune(c.typed); len(r) > 0 {
				c.typed = string(r[:len(r)-1])
			}
		default:
			if len(key) == 1 {
				c.typed += key
			}
		}
		return false, false
	}
	switch key {
	case "y", "Y":
		return true, true
	case "n", "N", "esc":
		return true, false
	}
	return false, false
}

func (c *confirmModel) View() string {
	lines := []string{titleStyle.Render("Confirm"), "", c.prompt()}
	if c.needsType() {
		lines = append(lines,
			"",
			errStyle.Render("⚠ "+c.act.BlastRadius),
			"",
			"Type "+keyStyle.Render("yes")+" to confirm: "+c.typed+"▌",
			"",
			mutedStyle.Render("esc to cancel"),
		)
		return dangerModalStyle.Render(strings.Join(lines, "\n"))
	}
	lines = append(lines, "", keyStyle.Render("y")+" confirm    "+keyStyle.Render("n")+" cancel")
	return modalStyle.Render(strings.Join(lines, "\n"))
}
