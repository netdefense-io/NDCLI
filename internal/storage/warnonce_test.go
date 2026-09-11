package storage

import (
	"bytes"
	"strings"
	"testing"
)

// resetWarnedConditions clears the process-wide record of shown warnings so
// each subtest starts from a clean slate.
func resetWarnedConditions(t *testing.T) {
	t.Helper()
	clear := func() {
		warnedConditions.Range(func(k, _ any) bool {
			warnedConditions.Delete(k)
			return true
		})
	}
	clear()
	t.Cleanup(clear)
}

// TestEmitDistinctWarning covers the double-warning on `ndcli auth login`:
// auth.GetManager() in PersistentPreRunE and the login client built after
// fetching the OAuth2 config each construct a TokenManager, each of which
// calls GetStorage. The user saw the keyring warning twice.
func TestEmitDistinctWarning(t *testing.T) {
	const keyringWarning = "Warning: system keyring is not available.\n"

	t.Run("a repeat of the same condition is suppressed", func(t *testing.T) {
		resetWarnedConditions(t)

		var first, second bytes.Buffer
		emitDistinctWarning(&first, []byte(keyringWarning))
		emitDistinctWarning(&second, []byte(keyringWarning))

		if !strings.Contains(first.String(), "system keyring is not available") {
			t.Errorf("first warning was swallowed: %q", first.String())
		}
		if second.Len() != 0 {
			t.Errorf("the repeat should be suppressed, got: %q", second.String())
		}
	})

	// The long-lived front-ends (the TUI and the MCP server) share this code
	// path. A single process-wide latch would let a degradation that appears
	// after startup be swallowed by a warning already shown for an unrelated
	// condition; deduping per condition must not do that.
	t.Run("a different condition later still surfaces once", func(t *testing.T) {
		resetWarnedConditions(t)

		var startup, degraded, repeat bytes.Buffer
		emitDistinctWarning(&startup, []byte("Warning: unknown auth.storage value \"keychain\".\n"))
		emitDistinctWarning(&degraded, []byte(keyringWarning))
		emitDistinctWarning(&repeat, []byte(keyringWarning))

		if startup.Len() == 0 {
			t.Error("the first condition should have been written")
		}
		if degraded.Len() == 0 {
			t.Error("a condition that appears later must not be swallowed by an earlier, different one")
		}
		if repeat.Len() != 0 {
			t.Errorf("the repeat of the second condition should be suppressed, got: %q", repeat.String())
		}
	})

	t.Run("an empty warning records nothing", func(t *testing.T) {
		resetWarnedConditions(t)

		var quiet, loud bytes.Buffer
		emitDistinctWarning(&quiet, nil)
		emitDistinctWarning(&quiet, []byte{})
		emitDistinctWarning(&loud, []byte(keyringWarning))

		if quiet.Len() != 0 {
			t.Errorf("expected nothing written for an empty warning, got: %q", quiet.String())
		}
		if loud.Len() == 0 {
			t.Error("a real warning after two empty ones must still be written")
		}
	})
}
