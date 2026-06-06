package tui

import (
	"strings"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// paletteQuitKind is the sentinel kind for the palette's "quit" entry, so a vi
// user can type ":q⏎" to exit.
const paletteQuitKind = "\x00quit"

// paletteItem is one navigable resource in the command palette.
type paletteItem struct {
	kind  string
	title string
}

// paletteModel is the ":" command palette for jumping between resources.
type paletteModel struct {
	items    []paletteItem
	filtered []paletteItem
	input    string
	cursor   int
}

func newPalette(reg *registry.Registry) *paletteModel {
	items := make([]paletteItem, 0)
	for _, r := range reg.All() {
		items = append(items, paletteItem{kind: r.Kind(), title: r.Title()})
	}
	items = append(items, paletteItem{kind: paletteQuitKind, title: "Quit"})
	p := &paletteModel{items: items}
	p.refilter()
	return p
}

func (p *paletteModel) refilter() {
	q := strings.ToLower(p.input)
	p.filtered = p.filtered[:0]
	for _, it := range p.items {
		if q == "" || strings.Contains(strings.ToLower(it.title), q) || strings.Contains(it.kind, q) {
			p.filtered = append(p.filtered, it)
		}
	}
	if p.cursor > len(p.filtered)-1 {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// handleKey processes a key. done reports the palette should close; kind is
// the chosen resource kind (empty when cancelled).
func (p *paletteModel) handleKey(key string) (done bool, kind string) {
	switch key {
	case "esc":
		return true, ""
	case "enter":
		if p.cursor >= 0 && p.cursor < len(p.filtered) {
			return true, p.filtered[p.cursor].kind
		}
		return true, ""
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "ctrl+n":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
	case "backspace":
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
			p.refilter()
		}
	default:
		if len(key) == 1 {
			p.input += key
			p.refilter()
		}
	}
	return false, ""
}

func (p *paletteModel) View() string {
	lines := []string{titleStyle.Render("Go to resource"), keyStyle.Render(":") + p.input + "▌", ""}
	for i, it := range p.filtered {
		cell := fitCell(it.title, 26)
		if i == p.cursor {
			cell = selRowStyle.Render(cell)
		}
		lines = append(lines, cell)
	}
	if len(p.filtered) == 0 {
		lines = append(lines, mutedStyle.Render("no match"))
	}
	return modalStyle.Render(strings.Join(lines, "\n"))
}
