package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// dashboardScreen is the home view: org-level counters plus the attention-
// ranked fleet table. Enter on a fleet row drills into device health.
type dashboardScreen struct {
	ctx    *AppContext
	data   *models.DashboardResponse
	rows   []models.DashboardCompactRow // sorted view of data.Compact
	cursor int
	w, h   int
	err    string
}

func newDashboardScreen(ctx *AppContext) *dashboardScreen {
	return &dashboardScreen{ctx: ctx}
}

func (s *dashboardScreen) Title() string { return "Dashboard" }

func (s *dashboardScreen) SetSize(w, h int) { s.w, s.h = w, h }

func (s *dashboardScreen) Init() tea.Cmd { return s.Refresh() }

func (s *dashboardScreen) Refresh() tea.Cmd {
	ctx := s.ctx
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := ctx.Svc.Dashboard(c, ctx.Org)
		if err != nil {
			return errMsg{context: "dashboard", err: err}
		}
		return dashLoadedMsg{data: d}
	}
}

func (s *dashboardScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case dashLoadedMsg:
		s.data = msg.data
		s.rows = uihelp.SortedCompact(msg.data.Compact)
		s.err = ""
		s.clampCursor()
	case errMsg:
		if msg.context == "dashboard" {
			s.err = msg.err.Error()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.rows)-1 {
				s.cursor++
			}
		case "g", "home":
			s.cursor = 0
		case "G", "end":
			s.cursor = len(s.rows) - 1
			s.clampCursor()
		case "pgdown", "]":
			s.cursor += s.pageStep()
			s.clampCursor()
		case "pgup", "[":
			s.cursor -= s.pageStep()
			s.clampCursor()
		case "enter":
			if dev := s.selectedDevice(); dev != "" {
				return s, pushScreen(newHealthScreen(s.ctx, dev))
			}
		}
	}
	return s, nil
}

func (s *dashboardScreen) clampCursor() {
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor > len(s.rows)-1 {
		s.cursor = len(s.rows) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// statusLine reports the fleet size for the footer.
func (s *dashboardScreen) statusLine() string {
	if s.data == nil {
		return ""
	}
	return fmt.Sprintf("%d device%s in fleet", len(s.rows), uihelp.Plural(len(s.rows)))
}

func (s *dashboardScreen) selectedDevice() string {
	if s.cursor >= 0 && s.cursor < len(s.rows) {
		return s.rows[s.cursor].Name
	}
	return ""
}

// pageStep is roughly the height of the fleet viewport.
func (s *dashboardScreen) pageStep() int {
	if s.h > 16 {
		return s.h - 16
	}
	return 1
}

func (s *dashboardScreen) View() string {
	if s.data == nil {
		if s.err != "" {
			return errStyle.Render("dashboard error: " + s.err)
		}
		return mutedStyle.Render("loading dashboard…")
	}
	d := s.data

	// Devices/Sync/Tasks are content-sized; the two version charts flex to fill
	// the rest of the row out to the right edge.
	dev, syncB, tasks := s.devicesPanel(d.Devices), s.syncPanel(d.Sync), s.tasksPanel(d.Tasks24h)
	used := lipgloss.Width(dev) + lipgloss.Width(syncB) + lipgloss.Width(tasks) + 4 // 4 single-space gaps
	remaining := s.w - used
	if remaining < 24 {
		remaining = 24
	}
	agentW := remaining / 2
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		dev, " ", syncB, " ", tasks, " ",
		versionPanelW("Agent versions", agentItems(d.AgentVersions), agentW), " ",
		versionPanelW("OPNsense versions", opnsenseItems(d.Compact), remaining-agentW),
	)
	top := lipgloss.JoinVertical(lipgloss.Left, row, "")

	// Give the fleet whatever vertical space the boxes above leave.
	return lipgloss.JoinVertical(lipgloss.Left, top, s.fleet(s.h-(strings.Count(top, "\n")+1)))
}

// dashPanelLines is the fixed content height (incl. title) of every summary box.
const dashPanelLines = 6

// boxPanel renders a fixed-height bordered panel so the dashboard boxes line up.
func boxPanel(title string, body []string) string {
	lines := append([]string{titleStyle.Render(title)}, body...)
	for len(lines) < dashPanelLines {
		lines = append(lines, "")
	}
	if len(lines) > dashPanelLines {
		lines = lines[:dashPanelLines]
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (s *dashboardScreen) devicesPanel(c models.DashboardDeviceCounters) string {
	return boxPanel("Devices", []string{
		fmt.Sprintf(" total   %3d", c.Total),
		fmt.Sprintf(" %s %3d", okStyle.Render("online "), c.Online),
		fmt.Sprintf(" %s %3d", warnStyle.Render("stale  "), c.Stale),
		fmt.Sprintf(" %s %3d", errStyle.Render("offline"), c.Offline),
		fmt.Sprintf(" pending %3d", c.Pending),
	})
}

func (s *dashboardScreen) syncPanel(c models.DashboardSyncCounters) string {
	return boxPanel("Sync", []string{
		fmt.Sprintf(" %s %3d", okStyle.Render("in-sync"), c.InSync),
		fmt.Sprintf(" %s %3d", warnStyle.Render("drift  "), c.Drift),
		fmt.Sprintf(" %s %3d", errStyle.Render("error  "), c.Error),
		fmt.Sprintf(" never   %3d", c.Never),
	})
}

func (s *dashboardScreen) tasksPanel(c models.DashboardTaskCounters) string {
	return boxPanel("Tasks 24h", []string{
		fmt.Sprintf(" %s %3d", okStyle.Render("done   "), c.Completed),
		fmt.Sprintf(" running %3d", c.InProgress),
		fmt.Sprintf(" pending %3d", c.Pending),
		fmt.Sprintf(" %s %3d", errStyle.Render("failed "), c.Failed),
		fmt.Sprintf(" expired %3d", c.Expired),
	})
}

// countItem is a label and its count for the version bar charts.
type countItem struct {
	label string
	count int
}

func agentItems(versions []models.DashboardAgentVersion) []countItem {
	out := make([]countItem, 0, len(versions))
	for _, v := range uihelp.SortedAgentVersions(versions) {
		out = append(out, countItem{label: v.Version, count: v.Count})
	}
	return out
}

// opnsenseItems aggregates the per-device OPNsense versions into a histogram.
func opnsenseItems(rows []models.DashboardCompactRow) []countItem {
	counts := map[string]int{}
	for _, r := range rows {
		if r.OPNsenseVersion != "" {
			counts[r.OPNsenseVersion]++
		}
	}
	out := make([]countItem, 0, len(counts))
	for v, c := range counts {
		out = append(out, countItem{label: v, count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].label < out[j].label
	})
	return out
}

// panelBoxFixed renders a fixed-height (dashPanelLines) panel of an exact content
// width, so the flexing version charts keep the row's uniform height.
func panelBoxFixed(title string, body []string, contentW int) string {
	if contentW < 8 {
		contentW = 8
	}
	lines := append([]string{titleStyle.Render(title)}, body...)
	for len(lines) < dashPanelLines {
		lines = append(lines, "")
	}
	if len(lines) > dashPanelLines {
		lines = lines[:dashPanelLines]
	}
	for i := range lines {
		lines[i] = padLine(lines[i], contentW)
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

// versionPanelW renders a horizontal bar chart of version → count that fills the
// given outer width, like the NDWeb distribution widgets.
func versionPanelW(title string, items []countItem, outerW int) string {
	contentW := outerW - 4
	if contentW < 12 {
		contentW = 12
	}
	if len(items) == 0 {
		return panelBoxFixed(title, []string{mutedStyle.Render(" (none reported)")}, contentW)
	}
	maxCount, labelW := 0, 0
	for _, it := range items {
		if it.count > maxCount {
			maxCount = it.count
		}
		if l := len([]rune(it.label)); l > labelW {
			labelW = l
		}
	}
	if labelW > 12 {
		labelW = 12
	}
	countW := len(fmt.Sprintf("%d", maxCount))
	barW := contentW - 3 - labelW - countW // " " + label + " " + bar + " " + count
	if barW < 3 {
		barW = 3
	}
	shown, more := items, 0
	if len(shown) > 5 {
		more, shown = len(shown)-4, shown[:4]
	}
	body := make([]string, 0, 5)
	for _, it := range shown {
		filled := 0
		if maxCount > 0 {
			filled = int(float64(it.count)/float64(maxCount)*float64(barW) + 0.5)
		}
		if filled > barW {
			filled = barW
		}
		bar := okStyle.Render(strings.Repeat("█", filled)) + dimStyle.Render(strings.Repeat("·", barW-filled))
		body = append(body, fmt.Sprintf(" %-*s %s %*d", labelW, uihelp.Truncate(it.label, labelW), bar, countW, it.count))
	}
	if more > 0 {
		body = append(body, mutedStyle.Render(fmt.Sprintf(" +%d more", more)))
	}
	return panelBoxFixed(title, body, contentW)
}

// fleetCols defines the attention-first fleet table layout.
var fleetCols = []registry.Column{
	{Title: "DEVICE", Width: 22},
	{Title: "STATUS", Width: 9},
	{Title: "HEARTBEAT", Width: 10},
	{Title: "SYNC", Width: 16},
	{Title: "ATTENTION", Width: 0},
	{Title: "AGENT", Width: 16},
}

func (s *dashboardScreen) fleet(avail int) string {
	heading := titleStyle.Render(fmt.Sprintf("Fleet (%d device%s) — attention-first",
		len(s.rows), uihelp.Plural(len(s.rows))))

	widths := computeWidths(fleetCols, s.w)
	head := headerRow(fleetCols, widths)

	rowsAvail := avail - 2 // heading + column header
	if rowsAvail < 1 {
		rowsAvail = 1
	}
	start, end := windowRange(s.cursor, len(s.rows), rowsAvail)

	lines := make([]string, 0, end-start+2)
	lines = append(lines, heading, head)
	for i := start; i < end; i++ {
		r := s.rows[i]
		lines = append(lines, s.fleetRow(r, widths, i == s.cursor))
	}
	if len(s.rows) == 0 {
		lines = append(lines, mutedStyle.Render("no devices"))
	}
	return strings.Join(lines, "\n")
}

func (s *dashboardScreen) fleetRow(r models.DashboardCompactRow, widths []int, selected bool) string {
	name := r.Name
	if ou := strings.Join(r.OUs, "/"); ou != "" {
		name = r.Name + " (" + ou + ")"
	}
	syncCell := r.Sync.State
	if r.Sync.AgeSec != nil {
		syncCell = fmt.Sprintf("%s · %s", r.Sync.State, uihelp.HumanDuration(*r.Sync.AgeSec))
	}
	attn := uihelp.AttentionTags(&r)
	if attn == "" {
		attn = "—"
	}

	cells := []string{
		fitCell(name, widths[0]),
		fitCell(uihelp.StatusColorLabel(r.StatusColor), widths[1]),
		fitCell(uihelp.HumanDurationPtr(r.HeartbeatAgeSec), widths[2]),
		fitCell(syncCell, widths[3]),
		fitCell(attn, widths[4]),
		fitCell(uihelp.Default(r.AgentVersion, "—"), widths[5]),
	}
	if !selected {
		// Colour the status and attention cells when not under the cursor
		// (the selection bar overrides colours for readability).
		cells[1] = statusStyle(uihelp.StatusColorLabel(r.StatusColor)).Render(cells[1])
		if attn != "—" {
			cells[4] = warnStyle.Render(cells[4])
		}
	}
	line := strings.Join(cells, colGap)
	if selected {
		return selRowStyle.Render(line)
	}
	return line
}
