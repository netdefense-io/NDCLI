package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// detailScreen renders a Describer resource's sections for a single entity,
// with simple scroll. Reached by pressing Enter on a list row.
type detailScreen struct {
	ctx      *AppContext
	res      registry.Resource
	id       string
	sections []registry.Section
	scroll   int
	w, h     int
	err      string
}

func newDetailScreen(ctx *AppContext, res registry.Resource, id string) *detailScreen {
	return &detailScreen{ctx: ctx, res: res, id: id}
}

func (s *detailScreen) Title() string    { return s.res.Title() + " · " + s.id }
func (s *detailScreen) SetSize(w, h int) { s.w, s.h = w, h }
func (s *detailScreen) Init() tea.Cmd    { return s.Refresh() }

func (s *detailScreen) Refresh() tea.Cmd {
	d, ok := s.res.(registry.Describer)
	if !ok {
		return nil
	}
	ctx, id := s.ctx, s.id
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sections, err := d.Describe(c, ctx.Svc, ctx.Org, id)
		if err != nil {
			return errMsg{context: s.res.Title() + " · " + id, err: err}
		}
		return detailLoadedMsg{kind: s.res.Kind(), id: id, sections: sections}
	}
}

func (s *detailScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case detailLoadedMsg:
		if msg.kind == s.res.Kind() && msg.id == s.id {
			s.sections = msg.sections
			s.err = ""
		}
	case errMsg:
		if msg.context == s.res.Title()+" · "+s.id {
			s.err = msg.err.Error()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if s.scroll > 0 {
				s.scroll--
			}
		case "down", "j":
			s.scroll++
		case "pgdown", "]":
			if s.h > 2 {
				s.scroll += s.h - 2
			}
		case "pgup", "[":
			if s.h > 2 {
				s.scroll -= s.h - 2
			}
			if s.scroll < 0 {
				s.scroll = 0
			}
		case "g", "home":
			s.scroll = 0
		}
	}
	return s, nil
}

func (s *detailScreen) View() string {
	if s.err != "" {
		return errStyle.Render("error: " + s.err)
	}
	if s.sections == nil {
		return mutedStyle.Render("loading…")
	}
	lines := renderSections(s.sections)
	if s.scroll > len(lines)-1 {
		s.scroll = len(lines) - 1
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	end := s.scroll + s.h
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[s.scroll:end], "\n")
}

// renderSections turns describe sections into display lines (label-aligned
// fields or wrapped text).
func renderSections(sections []registry.Section) []string {
	var lines []string
	for i, sec := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, titleStyle.Render(sec.Title))
		if sec.Text != "" {
			lines = append(lines, strings.Split(sec.Text, "\n")...)
			continue
		}
		width := 0
		for _, f := range sec.Fields {
			if len(f.Label) > width {
				width = len(f.Label)
			}
		}
		for _, f := range sec.Fields {
			label := mutedStyle.Render(fmt.Sprintf("%-*s", width, f.Label))
			lines = append(lines, "  "+label+"  "+f.Value)
		}
	}
	return lines
}
