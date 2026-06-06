package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// listScreen is the generic, paginated, filterable table that renders any
// registry.Resource. Drilling into a row opens device health (for devices) or
// the generic detail screen (for resources that implement Describer).
type listScreen struct {
	ctx     *AppContext
	res     registry.Resource
	rows    []registry.Row
	total   int
	page    int
	perPage int
	cursor  int
	w, h    int
	err     string

	filtering bool
	filter    string
}

func newListScreen(ctx *AppContext, res registry.Resource) *listScreen {
	return &listScreen{ctx: ctx, res: res, page: 1, perPage: 50}
}

func (s *listScreen) Title() string    { return s.res.Title() }
func (s *listScreen) SetSize(w, h int) { s.w, s.h = w, h }
func (s *listScreen) Init() tea.Cmd    { return s.loadCmd(s.page) }
func (s *listScreen) Refresh() tea.Cmd { return s.loadCmd(s.page) }

func (s *listScreen) loadCmd(page int) tea.Cmd {
	ctx, res, perPage := s.ctx, s.res, s.perPage
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rows, total, err := res.Fetch(c, ctx.Svc, ctx.Org, page, perPage)
		if err != nil {
			return errMsg{context: res.Title(), err: err}
		}
		return listLoadedMsg{kind: res.Kind(), rows: rows, total: total, page: page}
	}
}

// actionable
func (s *listScreen) actions() []registry.Action  { return s.res.Actions() }
func (s *listScreen) resource() registry.Resource { return s.res }
func (s *listScreen) selectedID() string {
	vr := s.visibleRows()
	if s.cursor >= 0 && s.cursor < len(vr) {
		return vr[s.cursor].ID
	}
	return ""
}

// inputCapturer
func (s *listScreen) capturingInput() bool { return s.filtering }

// statusLine reports the visible/total counts and any active filter.
func (s *listScreen) statusLine() string {
	msg := fmt.Sprintf("%d of %d", len(s.visibleRows()), s.total)
	if s.filter != "" {
		msg += " · filter: " + s.filter
	}
	return msg
}

func (s *listScreen) visibleRows() []registry.Row {
	if s.filter == "" {
		return s.rows
	}
	q := strings.ToLower(s.filter)
	out := make([]registry.Row, 0, len(s.rows))
	for _, r := range s.rows {
		if rowMatches(r, q) {
			out = append(out, r)
		}
	}
	return out
}

func rowMatches(r registry.Row, q string) bool {
	if strings.Contains(strings.ToLower(r.ID), q) {
		return true
	}
	for _, c := range r.Cells {
		if strings.Contains(strings.ToLower(c), q) {
			return true
		}
	}
	return false
}

func (s *listScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case listLoadedMsg:
		if msg.kind != s.res.Kind() {
			return s, nil
		}
		s.rows = msg.rows
		s.total = msg.total
		s.page = msg.page
		s.err = ""
		s.clampCursor()
	case errMsg:
		if msg.context == s.res.Title() {
			s.err = msg.err.Error()
		}
	case tea.KeyMsg:
		if s.filtering {
			return s.updateFilter(msg)
		}
		return s.updateNav(msg)
	}
	return s, nil
}

func (s *listScreen) updateFilter(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.filtering = false
		s.filter = ""
		s.cursor = 0
	case "enter":
		s.filtering = false
	case "backspace":
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			s.cursor = 0
		}
	default:
		if len(msg.Runes) == 1 {
			s.filter += string(msg.Runes)
			s.cursor = 0
		}
	}
	return s, nil
}

func (s *listScreen) updateNav(msg tea.KeyMsg) (screen, tea.Cmd) {
	vr := s.visibleRows()
	switch msg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(vr)-1 {
			s.cursor++
		}
	case "g", "home":
		s.cursor = 0
	case "G", "end":
		s.cursor = len(vr) - 1
		s.clampCursor()
	case "/":
		s.filtering = true
		s.filter = ""
		s.cursor = 0
	case "pgdown", "]":
		// Jump down a screenful; at the bottom of the page, load the next one.
		if s.cursor >= len(vr)-1 && s.page*s.perPage < s.total {
			s.page++
			s.cursor = 0
			return s, s.loadCmd(s.page)
		}
		s.cursor += s.pageStep()
		s.clampCursor()
	case "pgup", "[":
		// Jump up a screenful; at the top of the page, load the previous one.
		if s.cursor <= 0 && s.page > 1 {
			s.page--
			s.cursor = 0
			return s, s.loadCmd(s.page)
		}
		s.cursor -= s.pageStep()
		s.clampCursor()
	case "enter":
		id := s.selectedID()
		if id == "" {
			return s, nil
		}
		if s.res.Kind() == "device" {
			return s, pushScreen(newHealthScreen(s.ctx, id))
		}
		if _, ok := s.res.(registry.Describer); ok {
			return s, pushScreen(newDetailScreen(s.ctx, s.res, id))
		}
	}
	return s, nil
}

// pageStep is roughly one visible screenful of rows.
func (s *listScreen) pageStep() int {
	if s.h > 4 {
		return s.h - 3
	}
	return 1
}

func (s *listScreen) clampCursor() {
	n := len(s.visibleRows())
	if s.cursor > n-1 {
		s.cursor = n - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *listScreen) View() string {
	cols := s.res.Columns()
	widths := computeWidths(cols, s.w)
	vr := s.visibleRows()

	var top []string
	if s.filtering {
		top = append(top, keyStyle.Render("/")+s.filter+"▌")
	} else if s.filter != "" {
		top = append(top, mutedStyle.Render(fmt.Sprintf("filter: %s  (esc to clear)", s.filter)))
	}

	chrome := len(top) + 2 // header row + count line
	avail := s.h - chrome
	if avail < 1 {
		avail = 1
	}
	start, end := windowRange(s.cursor, len(vr), avail)

	lines := make([]string, 0, end-start+chrome+1)
	lines = append(lines, top...)
	lines = append(lines, headerRow(cols, widths))
	for i := start; i < end; i++ {
		lines = append(lines, dataRow(vr[i].Cells, widths, i == s.cursor))
	}
	if len(vr) == 0 {
		msg := "no " + strings.ToLower(s.res.Title())
		if s.err != "" {
			msg = "error: " + s.err
		}
		lines = append(lines, errOrMuted(s.err, msg))
	}
	count := fmt.Sprintf("%d shown · %d total · page %d", len(vr), s.total, s.page)
	lines = append(lines, mutedStyle.Render(count))
	return strings.Join(lines, "\n")
}

func errOrMuted(err, msg string) string {
	if err != "" {
		return errStyle.Render(msg)
	}
	return mutedStyle.Render(msg)
}
