package output

import (
	"fmt"
	"io"
	"strings"
)

// Box drawing characters
const (
	// Rounded corners (for headers)
	BoxTopLeft     = "╭"
	BoxTopRight    = "╮"
	BoxBottomLeft  = "╰"
	BoxBottomRight = "╯"

	// Sharp corners (for sections)
	BoxTopLeftSharp     = "┌"
	BoxTopRightSharp    = "┐"
	BoxBottomLeftSharp  = "└"
	BoxBottomRightSharp = "┘"

	// Lines
	BoxHorizontal = "─"
	BoxVertical   = "│"

	// T-junctions (left/right)
	BoxTeeLeft  = "├"
	BoxTeeRight = "┤"

	// T-junctions (top/bottom)
	BoxTeeTop    = "┬"
	BoxTeeBottom = "┴"

	// Cross intersection
	BoxCross = "┼"

	// Rounded T-junctions for tables
	BoxTeeTopRounded    = "╭" // Use rounded for top corners
	BoxTeeBottomRounded = "╰" // Use rounded for bottom corners
)

// BoxStyle defines the style of box to draw
type BoxStyle int

const (
	BoxStyleRounded BoxStyle = iota
	BoxStyleSharp
)

// Box represents a text box with optional title
type Box struct {
	Width int
	Style BoxStyle
}

// NewBox creates a new box with the given width
func NewBox(width int) *Box {
	return &Box{Width: width, Style: BoxStyleRounded}
}

// NewSharpBox creates a new box with sharp corners
func NewSharpBox(width int) *Box {
	return &Box{Width: width, Style: BoxStyleSharp}
}

// TopLine returns the top line of the box
func (b *Box) TopLine() string {
	tl, tr := BoxTopLeft, BoxTopRight
	if b.Style == BoxStyleSharp {
		tl, tr = BoxTopLeftSharp, BoxTopRightSharp
	}
	return tl + strings.Repeat(BoxHorizontal, b.Width-2) + tr
}

// BottomLine returns the bottom line of the box
func (b *Box) BottomLine() string {
	bl, br := BoxBottomLeft, BoxBottomRight
	if b.Style == BoxStyleSharp {
		bl, br = BoxBottomLeftSharp, BoxBottomRightSharp
	}
	return bl + strings.Repeat(BoxHorizontal, b.Width-2) + br
}

// TopLineWithTitle returns the top line with a title embedded
func (b *Box) TopLineWithTitle(title string) string {
	tl, tr := BoxTopLeftSharp, BoxTopRightSharp
	if b.Style == BoxStyleRounded {
		tl, tr = BoxTopLeft, BoxTopRight
	}

	titleLen := len(title)
	if titleLen+4 > b.Width {
		title = title[:b.Width-7] + "..."
		titleLen = len(title)
	}

	leftPad := 1
	rightPad := b.Width - 4 - titleLen - leftPad

	return tl + strings.Repeat(BoxHorizontal, leftPad) + " " + title + " " + strings.Repeat(BoxHorizontal, rightPad) + tr
}

// ContentLine returns a line with content padded to fit the box
func (b *Box) ContentLine(content string) string {
	contentLen := visibleLength(content)
	// padding = width - 1(│) - 2(spaces) - content - 1(space) - 1(│)
	padding := b.Width - 5 - contentLen
	if padding < 0 {
		padding = 0
	}
	return BoxVertical + "  " + content + strings.Repeat(" ", padding) + " " + BoxVertical
}

// EmptyLine returns an empty content line
func (b *Box) EmptyLine() string {
	return BoxVertical + strings.Repeat(" ", b.Width-2) + BoxVertical
}

// visibleLength returns the visible length of a string (excluding ANSI codes)
func visibleLength(s string) int {
	// Simple approach: count non-escape characters
	inEscape := false
	count := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		count++
	}
	return count
}

// Header draws a header box with title and optional subtitle
func Header(w io.Writer, title string, subtitle string, width int) {
	box := NewBox(width)
	fmt.Fprintln(w, box.TopLine())
	ColorHeader.Fprintf(w, "%s  %s", BoxVertical, title)
	// padding = width - 1(│) - 2(spaces) - title_len - 1(space) - 1(│)
	fmt.Fprintf(w, "%s %s\n", strings.Repeat(" ", width-5-len(title)), BoxVertical)
	if subtitle != "" {
		ColorDim.Fprintf(w, "%s  %s", BoxVertical, subtitle)
		fmt.Fprintf(w, "%s %s\n", strings.Repeat(" ", width-5-len(subtitle)), BoxVertical)
	}
	fmt.Fprintln(w, box.BottomLine())
}

// Section draws a section box with title and content lines
func Section(w io.Writer, title string, lines []string, width int) {
	box := NewSharpBox(width)
	fmt.Fprintln(w, box.TopLineWithTitle(title))
	for _, line := range lines {
		fmt.Fprintln(w, box.ContentLine(line))
	}
	fmt.Fprintln(w, box.BottomLine())
}

// KeyValue formats a key-value pair with consistent spacing
func KeyValue(key string, value string, keyWidth int) string {
	return fmt.Sprintf("%-*s  %s", keyWidth, key, value)
}

// TwoColumn formats two key-value pairs side by side
func TwoColumn(key1, val1, key2, val2 string, keyWidth, colWidth int) string {
	left := fmt.Sprintf("%-*s  %-*s", keyWidth, key1, colWidth-keyWidth-2, val1)
	right := fmt.Sprintf("%-*s  %s", keyWidth, key2, val2)
	return left + "  " + right
}

// Bullet returns a bulleted item
func Bullet(text string) string {
	return "• " + text
}

// Indent returns indented text
func Indent(text string, spaces int) string {
	return strings.Repeat(" ", spaces) + text
}

// StatusIndicator returns a status indicator character
func StatusIndicator(status string) string {
	switch status {
	case "ENABLED", "COMPLETED", "SUCCESS", "ACTIVE":
		return ColorEnabled.Sprint("●")
	case "DISABLED", "FAILED", "ERROR":
		return ColorDisabled.Sprint("○")
	case "PENDING", "INVITED":
		return ColorPending.Sprint("◐")
	case "IN_PROGRESS", "RUNNING":
		return ColorInProgress.Sprint("◑")
	default:
		return "○"
	}
}

// SyncIndicator returns a sync status indicator
func SyncIndicator(synced bool) string {
	if synced {
		return ColorSuccess.Sprint("✓")
	}
	return ColorDim.Sprint("✗")
}

// Divider returns a horizontal divider line
func Divider(width int) string {
	return strings.Repeat("─", width)
}

// WebAdminBox prints a prominent box showing the WebAdmin URL
func WebAdminBox(url string) {
	// Build content: "  🌐  WebAdmin: <url>"
	// Emoji 🌐 is 4 bytes but 2 visual columns
	content := "  🌐  WebAdmin: " + url
	// Visual width: 2 spaces + 2 (emoji) + 2 spaces + "WebAdmin: " (10) + url length = 16 + len(url)
	visualWidth := 16 + len(url)

	boxWidth := visualWidth + 4 // 2 for borders, 2 for padding
	box := NewBox(boxWidth)

	fmt.Println(box.TopLine())
	padding := boxWidth - 2 - visualWidth // inner width minus content
	ColorInfo.Printf("%s%s%s%s\n", BoxVertical, content, strings.Repeat(" ", padding), BoxVertical)
	fmt.Println(box.BottomLine())
}
