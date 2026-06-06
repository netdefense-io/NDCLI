package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// colGap separates table columns.
const colGap = "  "

// computeWidths resolves each column's render width. Columns with Width<=0 are
// "flex" and split the leftover horizontal space (the first flex column takes
// it all — a single flex column is the common case).
func computeWidths(cols []registry.Column, total int) []int {
	widths := make([]int, len(cols))
	fixed := 0
	flex := -1
	for i, c := range cols {
		if c.Width <= 0 {
			if flex == -1 {
				flex = i
			}
			continue
		}
		widths[i] = c.Width
		fixed += c.Width
	}
	gaps := len(colGap) * (len(cols) - 1)
	if flex >= 0 {
		rem := total - fixed - gaps
		if rem < 6 {
			rem = 6
		}
		widths[flex] = rem
	}
	return widths
}

// fitCell truncates s to width w (by display width) and right-pads with
// spaces so columns align. ANSI styling must be applied AFTER fitting.
func fitCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = uihelp.Truncate(s, w)
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// headerRow renders the column titles.
func headerRow(cols []registry.Column, widths []int) string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = colHeadStyle.Render(fitCell(c.Title, widths[i]))
	}
	return strings.Join(cells, colGap)
}

// dataRow renders one row of cells. When selected, the whole line is
// highlighted; the line is exactly the table content width so the bar spans it.
func dataRow(cells []string, widths []int, selected bool) string {
	out := make([]string, len(widths))
	for i := range widths {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		out[i] = fitCell(v, widths[i])
	}
	line := strings.Join(out, colGap)
	if selected {
		return selRowStyle.Render(line)
	}
	return line
}

// windowRange returns the [start,end) slice of items to render so that the
// cursor stays visible within a viewport of the given height.
func windowRange(cursor, total, height int) (start, end int) {
	if height <= 0 || total == 0 {
		return 0, 0
	}
	if total <= height {
		return 0, total
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > total {
		start = total - height
	}
	return start, start + height
}
