package helpers

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestConfirmOrForce_Force verifies that force=true always returns (true, nil)
// without touching stdin, regardless of TTY state.
func TestConfirmOrForce_Force(t *testing.T) {
	ok, err := ConfirmOrForce("irrelevant message", true)
	if err != nil {
		t.Fatalf("expected nil error with force=true, got: %v", err)
	}
	if !ok {
		t.Error("expected ok=true with force=true")
	}
}

// TestConfirmOrForce_NonTTY verifies that force=false with non-TTY stdin
// returns (false, error) with a message directing the user to pass --force.
// In CI and unit tests os.Stdin is a pipe, not a terminal, so this branch
// is exercised without any mocking.
func TestConfirmOrForce_NonTTY(t *testing.T) {
	// os.Stdin in a test binary is not a terminal (pipes/redirects in CI).
	ok, err := ConfirmOrForce("Delete something?", false)
	if err == nil {
		// If this machine happens to run tests attached to a real terminal
		// (e.g. an interactive developer shell), ConfirmOrForce will drop into
		// ConfirmAnswer() rather than the non-TTY branch. In that case we can't
		// assert the error path without mocking stdin, so skip.
		t.Skip("stdin is a terminal; non-TTY branch cannot be tested interactively")
	}
	if ok {
		t.Error("expected ok=false when non-TTY and force=false")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected error message to mention --force, got: %q", err.Error())
	}
}

// withStdin runs fn with os.Stdin reading input from a pipe, and os.Stderr,
// where the prompt goes, discarded.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, input)
	_ = w.Close()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	origIn, origErr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = r, devNull
	defer func() {
		os.Stdin, os.Stderr = origIn, origErr
		_ = r.Close()
		_ = devNull.Close()
	}()
	fn()
}

// An answer is any line, an empty one included; only a stdin that ends before
// anything was read leaves the prompt unanswered.
func TestConfirmAnswer(t *testing.T) {
	for _, tc := range []struct {
		input         string
		yes, answered bool
	}{
		{"", false, false},
		{"\n", false, true},
		{"n\n", false, true},
		{"y\n", true, true},
		{" YES \n", true, true},
		{"y", true, true},
		{"yesterday\n", false, true},
	} {
		withStdin(t, tc.input, func() {
			yes, answered := ConfirmAnswer("Delete?")
			if yes != tc.yes || answered != tc.answered {
				t.Errorf("input %q: yes=%v answered=%v, want %v %v", tc.input, yes, answered, tc.yes, tc.answered)
			}
		})
	}
}

func TestAskYesNoWithYesForItsDefault(t *testing.T) {
	for _, tc := range []struct {
		input         string
		yes, answered bool
	}{
		{"", false, false},
		{"\n", true, true},
		{"n\n", false, true},
		{"no\n", false, true},
		{"y\n", true, true},
	} {
		withStdin(t, tc.input, func() {
			yes, answered := AskYesNo("Update?", true)
			if yes != tc.yes || answered != tc.answered {
				t.Errorf("input %q: yes=%v answered=%v, want %v %v", tc.input, yes, answered, tc.yes, tc.answered)
			}
		})
	}
}

// A command that asks twice finds each answer on its own line of a pipe.
func TestReadAnswerLeavesTheNextLineForTheNextQuestion(t *testing.T) {
	long := strings.Repeat("x", maxAnswerBytes+10)
	withStdin(t, "y\n"+long+"\nn\n", func() {
		for i, want := range []string{"y", long[:maxAnswerBytes], "n"} {
			if got, answered := ReadAnswer(); got != want || !answered {
				t.Errorf("answer %d = %q (answered %v), want %q", i, got, answered, want)
			}
		}
		if got, answered := ReadAnswer(); answered {
			t.Errorf("after the last line: %q, answered", got)
		}
	})
}
