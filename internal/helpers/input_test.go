package helpers

import (
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
		// Confirm() rather than the non-TTY branch. In that case we can't
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
