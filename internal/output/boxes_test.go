package output

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// forceBoxWidth clamps the self-sizing box to w for the duration of a test.
func forceBoxWidth(t *testing.T, w int) {
	t.Helper()
	restore := boxWidthLimit
	boxWidthLimit = func() int { return w }
	t.Cleanup(func() { boxWidthLimit = restore })
}

// renderFieldBox renders a box and returns its lines.
func renderFieldBox(b *fieldBox) []string {
	var buf bytes.Buffer
	b.render(BaseFormatter{Writer: &buf})
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

// assertWithinWidth checks that no line is wider than w visible columns.
func assertWithinWidth(t *testing.T, lines []string, w int) {
	t.Helper()
	for _, line := range lines {
		if got := visibleLength(line); got > w {
			t.Errorf("line is %d columns, over the %d limit: %q", got, w, line)
		}
	}
}

// countTokens counts whole space-separated tokens across the box's content,
// with the borders stripped. Whole tokens rather than substrings: a short word
// often sits inside a longer one.
func countTokens(lines []string) map[string]int {
	trimSet := BoxVertical + BoxHorizontal + BoxTopLeft + BoxTopRight + BoxBottomLeft + BoxBottomRight + " "
	seen := map[string]int{}
	for _, line := range lines {
		for _, tok := range strings.Fields(strings.Trim(stripANSI(line), trimSet)) {
			seen[tok]++
		}
	}
	return seen
}

// TestFieldBox_OverWideColouredValueLosesColourNotTheBorder covers the
// fallback for a coloured value. A coloured value that fits is emitted whole,
// because wrapping it could cut an escape sequence in half. One that does not
// fit is stripped of its escapes and wrapped like any other value: losing the
// colour is a smaller loss than running through the border.
func TestFieldBox_OverWideColouredValueLosesColourNotTheBorder(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	// Literal escapes rather than a ColorX helper: the colour package
	// disables itself when stdout is not a terminal, so a helper would
	// produce plain text here and never exercise this path.
	const red, reset = "\x1b[31m", "\x1b[0m"
	words := []string{"connection", "refused", "by", "the", "upstream", "relay"}
	value := red + strings.Join(words, " ") + reset

	b := &fieldBox{title: "Status"}
	b.field("Detail", value)
	lines := renderFieldBox(b)

	assertWithinWidth(t, lines, forced)
	seen := countTokens(lines)
	for _, w := range words {
		if seen[w] != 1 {
			t.Errorf("word %q appears %d times, want exactly 1:\n%s", w, seen[w], strings.Join(lines, "\n"))
		}
	}

	// A coloured value that fits keeps its colour.
	short := &fieldBox{title: "Status"}
	short.field("State", red+"OPEN"+reset)
	if out := strings.Join(renderFieldBox(short), "\n"); !strings.Contains(out, red) {
		t.Errorf("a coloured value that fits must keep its escapes:\n%q", out)
	}
}

// TestFieldBox_LabelAloneBranchStaysInsideTheBorder covers the branch for a
// label with no room left beside it. The label is wrapped at the content
// width like any other text and the value follows as an indented block, so
// neither can push past the border the branch exists to protect.
func TestFieldBox_LabelAloneBranchStaysInsideTheBorder(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	label := strings.Repeat("L", 36)
	b := &fieldBox{title: "Guard"}
	b.field(label, "a value that follows the label")
	lines := renderFieldBox(b)

	// Every line is padded to the full width, so all of them measure exactly
	// the forced width; a line over it would mean an overflow.
	for _, line := range lines {
		if got := visibleLength(line); got != forced {
			t.Errorf("line is %d columns, want exactly %d: %q", got, forced, line)
		}
	}

	// The value sits underneath the label, indented, never beside it.
	var valueLines, labelLines int
	for _, line := range lines[1 : len(lines)-1] {
		body := strings.Trim(stripANSI(line), BoxVertical)
		switch {
		case strings.HasPrefix(body, "    "):
			valueLines++
		case strings.Contains(body, "L"):
			labelLines++
			if strings.Contains(body, "value") {
				t.Errorf("the value must not share a line with the label: %q", line)
			}
		}
	}
	if labelLines < 2 {
		t.Errorf("expected the long label to wrap across lines, got %d:\n%s", labelLines, strings.Join(lines, "\n"))
	}
	if valueLines == 0 {
		t.Errorf("expected the value indented beneath the label:\n%s", strings.Join(lines, "\n"))
	}
}

// TestFieldBox_InternalDoubleSpacesAreNotDuplicated covers the fallback in
// wrapFirstThenRest. wrapToWidth collapses runs of whitespace, so its first
// chunk is not a literal prefix of a value whose opening words are separated
// by more than one space. The two-width wrap cannot line the remainder up in
// that case and falls back to the single-width wrap, which must still print
// every word exactly once.
func TestFieldBox_InternalDoubleSpacesAreNotDuplicated(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	// The double space falls inside the first wrapped chunk.
	value := "alpha  beta " + strings.Join(words[2:], " ")

	b := &fieldBox{title: "Wrap"}
	b.field("Subject", value)
	lines := renderFieldBox(b)

	assertWithinWidth(t, lines, forced)
	seen := countTokens(lines)
	for _, w := range words {
		if seen[w] != 1 {
			t.Errorf("word %q appears %d times, want exactly 1:\n%s", w, seen[w], strings.Join(lines, "\n"))
		}
	}
}

// TestTopLineWithTitle_NeverPanicsAcrossWidths pins the title arithmetic.
// The budget was one column too generous and the cut was made in bytes, so a
// title that did not fit produced a negative right pad and panicked
// strings.Repeat — and a multi-byte title was cut mid-rune. Every width must
// produce a line of exactly that many visible columns, with the title intact
// as UTF-8.
func TestTopLineWithTitle_NeverPanicsAcrossWidths(t *testing.T) {
	titles := map[string]string{
		"ascii":      "Personal Access Token Created!",  // 30 columns
		"multi-byte": "Configuração Ampliada de Módulo", // accents: bytes > runes
		"short":      "OK",
	}
	for name, title := range titles {
		t.Run(name, func(t *testing.T) {
			for width := 24; width <= 40; width++ {
				line := NewBox(width).TopLineWithTitle(title)
				if got := visibleLength(line); got != width {
					t.Errorf("width %d: line is %d columns: %q", width, got, line)
				}
				if !utf8.ValidString(line) {
					t.Errorf("width %d: line is not valid UTF-8: %q", width, line)
				}
				// Anything that did not fit must say so with an ellipsis.
				if len([]rune(title)) > width-5 && !strings.Contains(line, "…") {
					t.Errorf("width %d: an over-long title must be marked truncated: %q", width, line)
				}
			}
		})
	}
}

// TestTopLineWithTitle_DegenerateWidths covers widths with no room for a
// title at all. These cannot arise through fieldBox, which floors at 24, but
// Box is exported and must not panic on any of them.
func TestTopLineWithTitle_DegenerateWidths(t *testing.T) {
	for width := 0; width <= 8; width++ {
		line := NewBox(width).TopLineWithTitle("Something Long")
		if !utf8.ValidString(line) {
			t.Errorf("width %d: line is not valid UTF-8: %q", width, line)
		}
	}
}

// TestTopLineWithTitle_UnchangedWhenTheTitleFits guards the common path: a
// title with room to spare renders exactly as it did before.
func TestTopLineWithTitle_UnchangedWhenTheTitleFits(t *testing.T) {
	got := NewBox(20).TopLineWithTitle("Device")
	want := BoxTopLeft + BoxHorizontal + " Device " + strings.Repeat(BoxHorizontal, 9) + BoxTopRight
	if got != want {
		t.Errorf("TopLineWithTitle = %q, want %q", got, want)
	}
	if visibleLength(got) != 20 {
		t.Errorf("line is %d columns, want 20", visibleLength(got))
	}
}

// TestFieldBox_EmbeddedNewlineStaysInsideTheBorder covers a value carrying a
// newline. visibleLength counts "\n" as one ordinary column, so a short
// two-line value passed the "it fits" check and went out whole, splitting the
// content line: the first half kept its left border and lost its right, the
// second the reverse. Such a value now goes through the wrap branch, which
// splits on newlines before anything else.
func TestFieldBox_EmbeddedNewlineStaysInsideTheBorder(t *testing.T) {
	// Wide enough that the value fits on one line by length alone — the
	// newline is the only reason it must still be split.
	forceBoxWidth(t, 66)

	b := &fieldBox{title: "Newline"}
	b.field("Detail", "Line1\nLine2")
	lines := renderFieldBox(b)

	if len(lines) < 3 {
		t.Fatalf("expected a box with content, got:\n%s", strings.Join(lines, "\n"))
	}
	width := visibleLength(lines[0])
	for i, line := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(line, BoxVertical) || !strings.HasSuffix(line, BoxVertical) {
			t.Errorf("content line %d is not bordered on both sides: %q", i, line)
		}
		if got := visibleLength(line); got != width {
			t.Errorf("content line %d is %d columns, want %d: %q", i, got, width, line)
		}
	}

	seen := countTokens(lines)
	for _, want := range []string{"Line1", "Line2"} {
		if seen[want] != 1 {
			t.Errorf("%q appears %d times, want exactly 1:\n%s", want, seen[want], strings.Join(lines, "\n"))
		}
	}
	// The two halves belong on separate lines, never rejoined into one.
	for _, line := range lines {
		if strings.Contains(line, "Line1") && strings.Contains(line, "Line2") {
			t.Errorf("the newline must still break the value: %q", line)
		}
	}
}
