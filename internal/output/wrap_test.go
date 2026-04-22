package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestWrapToWidth_ShortLinePassesThrough(t *testing.T) {
	got := wrapToWidth("short line", 40)
	if len(got) != 1 || got[0] != "short line" {
		t.Fatalf("want [\"short line\"], got %q", got)
	}
}

func TestWrapToWidth_PreservesEmbeddedNewlines(t *testing.T) {
	got := wrapToWidth("line one\nline two\nline three", 40)
	want := []string{"line one", "line two", "line three"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestWrapToWidth_WrapsAtWordBoundary(t *testing.T) {
	// 50 chars
	s := "Invalid JSON payload: Expecting value: line 1 column 1 (char 0)"
	got := wrapToWidth(s, 50)
	if len(got) < 2 {
		t.Fatalf("expected >=2 lines when wrapping %d chars at width 50, got %d: %q", len(s), len(got), got)
	}
	for i, line := range got {
		if len(line) > 50 {
			t.Errorf("line %d exceeds width 50 (len=%d): %q", i, len(line), line)
		}
	}
	// No "..." truncation artifact.
	if joined := strings.Join(got, " "); !strings.Contains(joined, "(char 0)") {
		t.Errorf("full message content must be preserved end-to-end, got: %q", joined)
	}
}

func TestWrapToWidth_HardSplitsLongWordsWithoutSpaces(t *testing.T) {
	s := strings.Repeat("x", 120) // no spaces, way over width
	got := wrapToWidth(s, 40)
	total := 0
	for _, line := range got {
		if len(line) > 40 {
			t.Errorf("line exceeds width: %q (len=%d)", line, len(line))
		}
		total += len(line)
	}
	if total != 120 {
		t.Errorf("hard-split must preserve every char; got %d total, want 120", total)
	}
}

// Regression test for NDCLI-go#53: the exact truncation the issue reported
// must not happen. The detailed formatter should render the full message
// wrapped across multiple lines, not "… co..." hard-truncated.
func TestDetailedFormatTask_LongMessageIsNotTruncated(t *testing.T) {
	longMsg := "Invalid JSON payload: Expecting value: line 1 column 1 (char 0)"
	task := &models.Task{
		ID:           "82l7C3nQ",
		Type:         "PING",
		Status:       "FAILED",
		DeviceName:   "opnsense-a",
		Organization: "junix-org",
		Message:      longMsg,
	}

	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTask(task); err != nil {
		t.Fatalf("FormatTask: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "...") {
		t.Errorf("detailed output must not truncate with '...' anymore:\n%s", out)
	}
	if !strings.Contains(out, "(char 0)") {
		t.Errorf("full message must be present in output; missing '(char 0)':\n%s", out)
	}
}
