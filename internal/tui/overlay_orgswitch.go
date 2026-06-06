package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// orgSwitcherModel is the "o" overlay for changing the active organization for
// the session. The list of orgs is loaded asynchronously on open.
type orgSwitcherModel struct {
	all      []string
	filtered []string
	input    string
	cursor   int
	loading  bool
	err      string
}

func newOrgSwitcher() *orgSwitcherModel {
	return &orgSwitcherModel{loading: true}
}

// loadOrgsCmd fetches the org names for the switcher.
func loadOrgsCmd(svc *service.Service) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := svc.OrgList(c, service.OrgListOpts{PerPage: 200})
		if err != nil {
			return errMsg{context: "orgs", err: err}
		}
		names := make([]string, 0, len(res.Orgs))
		for _, o := range res.Orgs {
			names = append(names, o.Name)
		}
		return orgsLoadedMsg{names: names}
	}
}

func (m *orgSwitcherModel) setOrgs(names []string) {
	m.all = names
	m.loading = false
	m.refilter()
}

// setErr records a load failure so the overlay shows the error instead of
// hanging on "loading…".
func (m *orgSwitcherModel) setErr(msg string) {
	m.err = msg
	m.loading = false
}

func (m *orgSwitcherModel) refilter() {
	q := strings.ToLower(m.input)
	m.filtered = m.filtered[:0]
	for _, n := range m.all {
		if q == "" || strings.Contains(strings.ToLower(n), q) {
			m.filtered = append(m.filtered, n)
		}
	}
	if m.cursor > len(m.filtered)-1 {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// handleKey processes a key. done reports the overlay should close; org is the
// chosen org name (empty when cancelled).
func (m *orgSwitcherModel) handleKey(key string) (done bool, org string) {
	switch key {
	case "esc":
		return true, ""
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			return true, m.filtered[m.cursor]
		}
		return true, ""
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "backspace":
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
			m.refilter()
		}
	default:
		if len(key) == 1 {
			m.input += key
			m.refilter()
		}
	}
	return false, ""
}

func (m *orgSwitcherModel) View() string {
	lines := []string{titleStyle.Render("Switch organization"), keyStyle.Render("o ") + m.input + "▌", ""}
	if m.loading {
		lines = append(lines, mutedStyle.Render("loading…"))
		return modalStyle.Render(strings.Join(lines, "\n"))
	}
	if m.err != "" {
		lines = append(lines, errStyle.Render(m.err))
		return modalStyle.Render(strings.Join(lines, "\n"))
	}
	for i, n := range m.filtered {
		cell := fitCell(n, 26)
		if i == m.cursor {
			cell = selRowStyle.Render(cell)
		}
		lines = append(lines, cell)
	}
	if len(m.filtered) == 0 {
		lines = append(lines, mutedStyle.Render("no match"))
	}
	return modalStyle.Render(strings.Join(lines, "\n"))
}
