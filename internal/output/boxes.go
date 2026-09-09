package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
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

	// The line is corner, leftPad dashes, space, title, space, rightPad
	// dashes, corner — so once the single left dash is spent the title has
	// Width-5 columns to live in. Titles were measured and cut in bytes
	// against a budget one column too generous, which drove rightPad to -1
	// and panicked strings.Repeat; a multi-byte title was also cut mid-rune.
	const leftPad = 1
	budget := b.Width - 4 - leftPad
	if budget < 1 {
		// No room for a title at all. A bare border beats a broken one.
		if b.Width < 2 {
			return tl + tr
		}
		return b.TopLine()
	}

	runes := []rune(title)
	if len(runes) > budget {
		if budget == 1 {
			title = "…"
		} else {
			title = string(runes[:budget-1]) + "…"
		}
	}

	titleLen := len([]rune(title))
	rightPad := b.Width - 4 - titleLen - leftPad
	if rightPad < 0 {
		rightPad = 0
	}

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

// stripANSI removes ANSI escape sequences and returns the visible text. It is
// the counterpart of visibleLength and shares its simple scanner.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inEscape := false
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
		b.WriteRune(r)
	}
	return b.String()
}

// Header draws a header box with title and optional subtitle
func Header(w io.Writer, title string, subtitle string, width int) {
	box := NewBox(width)
	fmt.Fprintln(w, box.TopLine())
	ColorHeader.Fprintf(w, "%s  %s", BoxVertical, title)
	// padding = width - 1(│) - 2(spaces) - title_len - 1(space) - 1(│)
	titlePad := width - 5 - len(title)
	if titlePad < 0 {
		titlePad = 0
	}
	fmt.Fprintf(w, "%s %s\n", strings.Repeat(" ", titlePad), BoxVertical)
	if subtitle != "" {
		ColorDim.Fprintf(w, "%s  %s", BoxVertical, subtitle)
		subtitlePad := width - 5 - len(subtitle)
		if subtitlePad < 0 {
			subtitlePad = 0
		}
		fmt.Fprintf(w, "%s %s\n", strings.Repeat(" ", subtitlePad), BoxVertical)
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

// fieldBoxMinWidth is the floor for a fieldBox; the box grows past it to fit
// its widest field.
const fieldBoxMinWidth = 66

// fieldBoxFallbackWidth caps a fieldBox when stdout is not a terminal, so a
// redirected or piped run still produces a box that fits a normal window.
const fieldBoxFallbackWidth = 100

// fieldBoxFloorWidth keeps a very narrow terminal from producing a box with
// no room for content.
const fieldBoxFloorWidth = 24

// boxWidthLimit reports the widest a self-sizing box may grow: the terminal's
// width when stdout is one, else a fixed cap. Growing past it would wrap in
// the terminal and break every border. It sizes off os.Stdout by design —
// that is where the formatters write, whatever writer a test hands them. It
// is a var so a test can force a width.
var boxWidthLimit = func() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return fieldBoxFallbackWidth
}

// fieldBox collects label/value pairs and renders them *inside* one titled
// box, wrapping any value too wide for the box onto continuation lines.
//
// Several detailed renderers used to print the titled top border and the
// bottom border back to back and then print their fields underneath with
// printLabelValue, which writes no border at all. The result was an empty
// single-line box with every field outside it. Build the block through this
// type and the fields always sit between the two borders.
type fieldBox struct {
	title  string
	fields []fieldBoxEntry
}

type fieldBoxEntry struct {
	label string
	value string
}

// field appends one "Label: value" line. The value may already carry ANSI
// colour; width is measured with visibleLength, so it still aligns.
//
// The value is normalised on the way in, so everything downstream — the
// width calculation as much as the wrap — sees line breaks in one form.
func (b *fieldBox) field(label, value string) {
	b.fields = append(b.fields, fieldBoxEntry{label: label, value: normalizeLineBreaks(value)})
}

// normalizeLineBreaks folds CRLF and lone CR into LF and drops trailing
// blank space.
//
// A carriage return is worse than a newline here. visibleLength counts it as
// one ordinary column, so the padding comes out a column short, and the
// terminal then renders it as a return to column 0, painting the rest of the
// value over the left border. Trailing breaks go too: they would otherwise
// add an empty content line under the field.
func normalizeLineBreaks(s string) string {
	if strings.ContainsRune(s, '\r') {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return strings.TrimRight(s, " \n")
}

// hasLineBreak reports whether s would break a content line. It still looks
// for a carriage return even though field() normalises them away, so a value
// reaching the renderer by some other route cannot slip past.
func hasLineBreak(s string) bool {
	return strings.ContainsAny(s, "\n\r")
}

// width picks the rendered width: wide enough for the longest field and the
// title, never wider than the terminal.
func (b *fieldBox) width() int {
	limit := boxWidthLimit()
	if limit < fieldBoxFloorWidth {
		limit = fieldBoxFloorWidth
	}
	width := fieldBoxMinWidth
	// ContentLine spends 5 columns on borders and padding.
	for _, f := range b.fields {
		if w := len(f.label) + 2 + visibleLength(f.value) + 5; w > width {
			width = w
		}
	}
	// TopLineWithTitle spends 4 columns on borders, spaces and the left
	// dash, plus the single dash it always draws on the right.
	if w := len(b.title) + 6; w > width {
		width = w
	}
	if width > limit {
		width = limit
	}
	return width
}

// wrapFirstThenRest wraps s with `first` columns available on the opening
// line and `rest` on every line after it. A boxed field spends its first line
// on the label, so a narrow box would otherwise wrap the whole value at the
// few columns left beside a long label instead of using the full width once
// the label is out of the way.
func wrapFirstThenRest(s string, first, rest int) []string {
	if first < 1 {
		first = 1
	}
	if rest < 1 {
		rest = 1
	}
	// Trim before wrapping and compare against the trimmed value: wrapToWidth
	// splits on fields, so the chunk it returns for a value with leading
	// whitespace is not a literal prefix of the original. Trimming afterwards
	// instead produced a tail that still held the opening chunk, which was
	// then printed twice.
	trimmed := strings.TrimLeft(s, " \n")
	head := wrapToWidth(trimmed, first)
	if len(head) <= 1 {
		return head
	}
	tail := strings.TrimLeft(strings.TrimPrefix(trimmed, head[0]), " \n")
	if tail == "" || tail == trimmed {
		// The head is not a literal prefix of the value (repeated whitespace
		// was collapsed); keep the single-width wrap rather than guess.
		return head
	}
	return append([]string{head[0]}, wrapToWidth(tail, rest)...)
}

// lines renders the content lines, wrapping a value that does not fit.
// Continuation lines are indented two columns under the label.
func (b *fieldBox) lines(content int) []string {
	var out []string
	for _, f := range b.fields {
		prefix := f.label + ": "
		// A line break has to reach the wrap branch whatever its length:
		// visibleLength counts one as an ordinary column, so a value that
		// "fits" could still break the line in two and leave one half
		// without a left border and the other without a right one.
		if visibleLength(ColorLabel.Sprint(prefix)+f.value) <= content &&
			!hasLineBreak(f.value) {
			// Fits as it stands, colour and all. Every coloured value today
			// is a short status or enum, so this is the path they take.
			out = append(out, ColorLabel.Sprint(prefix)+f.value)
			continue
		}
		// It has to wrap, and wrapping coloured text could cut an escape
		// sequence in half. A value too wide to fit therefore loses its
		// colour rather than the box losing its border.
		value := stripANSI(f.value)
		// What is left of the line once the label is printed. wrapToWidth
		// hard-splits a token wider than that, so a narrow box wraps more
		// often rather than running through the right border.
		avail := content - len(prefix)
		if avail < 1 {
			// The label alone fills the line. Give the value its own
			// indented block underneath instead of pushing past the border.
			indent := content - 2
			if indent < 1 {
				indent = 1
			}
			// The label goes through the wrap too: this branch exists to keep
			// a long label off the border, so printing it unclamped would
			// defeat it.
			for _, part := range wrapToWidth(strings.TrimSuffix(prefix, " "), content) {
				out = append(out, ColorLabel.Sprint(part))
			}
			for _, rest := range wrapToWidth(value, indent) {
				out = append(out, "  "+rest)
			}
			continue
		}
		wrapped := wrapFirstThenRest(value, avail, content-2)
		out = append(out, ColorLabel.Sprint(prefix)+wrapped[0])
		for _, rest := range wrapped[1:] {
			out = append(out, "  "+rest)
		}
	}
	return out
}

// render writes the box to the formatter's writer.
func (b *fieldBox) render(f BaseFormatter) {
	width := b.width()
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle(b.title))
	for _, line := range b.lines(width - 5) {
		fmt.Fprintln(f.Writer, box.ContentLine(line))
	}
	fmt.Fprintln(f.Writer, box.BottomLine())
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

// OnlineIndicator returns a tri-state indicator for the device online registry:
// true → connected, false → authoritative offline, nil → unknown (Redis down).
func OnlineIndicator(online *bool) string {
	if online == nil {
		return ColorDim.Sprint("?")
	}
	if *online {
		return ColorEnabled.Sprint("●")
	}
	return ColorDisabled.Sprint("○")
}

// OnlineLabel returns the plain-text label paired with OnlineIndicator.
func OnlineLabel(online *bool) string {
	if online == nil {
		return "unknown"
	}
	if *online {
		return "online"
	}
	return "offline"
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
