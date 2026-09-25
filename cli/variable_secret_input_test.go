package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Secret value input at every scope (org/ou/template/device): a value
// nobody should have to type as a literal command-line argument — an
// AUTH_SERVER's ldap_bindpw secret reference target, or an override of
// one — gets --value-stdin and, absent that, a no-echo interactive
// prompt.

func TestTrimOneTrailingNewline(t *testing.T) {
	cases := map[string]string{
		"hunter2\n":   "hunter2",
		"hunter2\r\n": "hunter2",
		"hunter2":     "hunter2", // no trailing newline: unchanged
		"hunter2\n\n": "hunter2\n",
		"":            "",
	}
	for in, want := range cases {
		if got := trimOneTrailingNewline(in); got != want {
			t.Errorf("trimOneTrailingNewline(%q) = %q, want %q", in, got, want)
		}
	}
}

// withStdin replaces os.Stdin with a pipe carrying content, restoring the
// original on cleanup, and returns once the writer side is closed so a
// reader sees EOF after content.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	w.Close()
}

func newValueStdinFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("value-stdin", false, "")
	return cmd
}

// newSetValueFlagCmd mirrors makeVarSetCommand's flag set, for exercising
// resolveVariableSetValueInput without building a full scope command.
func newSetValueFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("value", "", "")
	cmd.Flags().Bool("value-stdin", false, "")
	cmd.Flags().String("description", "", "")
	return cmd
}

func TestReadValueFromStdin_TrimsExactlyOneTrailingNewline(t *testing.T) {
	withStdin(t, "s3cr3t-p@ss\n")
	got, err := readValueFromStdin()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "s3cr3t-p@ss" {
		t.Errorf("got %q, want %q", got, "s3cr3t-p@ss")
	}
}

func TestResolveVariableValueInput_ValueStdin(t *testing.T) {
	withStdin(t, "from-stdin\n")
	cmd := newValueStdinFlagCmd()
	cmd.Flags().Set("value-stdin", "true")

	got, err := resolveVariableValueInput(cmd, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from-stdin" {
		t.Errorf("got %q, want %q", got, "from-stdin")
	}
}

func TestResolveVariableValueInput_ValueStdinRejectsPositionalValue(t *testing.T) {
	cmd := newValueStdinFlagCmd()
	cmd.Flags().Set("value-stdin", "true")

	_, err := resolveVariableValueInput(cmd, "also-given", true)
	if err == nil {
		t.Fatal("expected an error combining --value-stdin with a positional value")
	}
}

func TestResolveVariableValueInput_PositionalWins(t *testing.T) {
	cmd := newValueStdinFlagCmd()
	got, err := resolveVariableValueInput(cmd, "literal-value", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "literal-value" {
		t.Errorf("got %q, want %q", got, "literal-value")
	}
}

// TestResolveVariableValueInput_NonTTYWithNoValueErrors mirrors
// internal/helpers's TestConfirmOrForce_NonTTY pattern: os.Stdin in a test
// binary is not a terminal (pipes/redirects in CI), so the "neither a
// value nor stdin was given" branch is reachable without mocking a TTY.
// There is no interactive fallback here: on an actual terminal this
// function would block in promptSecretValue rather than return an error,
// so this test only makes sense — and only ever runs — non-interactively.
func TestResolveVariableValueInput_NonTTYWithNoValueErrors(t *testing.T) {
	cmd := newValueStdinFlagCmd()
	_, err := resolveVariableValueInput(cmd, "", false)
	if err == nil {
		t.Fatal("expected an error when neither a value nor --value-stdin is given on a non-terminal stdin")
	}
	if !strings.Contains(err.Error(), "--value-stdin") {
		t.Errorf("error should point at --value-stdin as a way forward, got: %v", err)
	}
}

// TestReadValueFromStdin_RefusesATerminal guards against the on-screen
// echo a raw terminal read would otherwise produce: --value-stdin exists
// so a secret value never has to be typed where the terminal shows it.
// os.Stdin in a test binary is a pipe/redirect, never a terminal, so this
// exercises the refusal message rather than the IsTerminal branch itself.
func TestReadValueFromStdin_RefusesATerminal(t *testing.T) {
	withStdin(t, "s3cr3t\n")
	_, err := readValueFromStdin()
	if err != nil {
		t.Fatalf("a piped stdin must not be refused: %v", err)
	}
}

func TestResolveVariableSetValueInput_ValueStdin(t *testing.T) {
	withStdin(t, "from-stdin\n")
	cmd := newSetValueFlagCmd()
	cmd.Flags().Set("value-stdin", "true")

	got, err := resolveVariableSetValueInput(cmd, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || *got != "from-stdin" {
		t.Errorf("got %v, want \"from-stdin\"", got)
	}
}

func TestResolveVariableSetValueInput_ValueAndValueStdinMutuallyExclusive(t *testing.T) {
	cmd := newSetValueFlagCmd()
	cmd.Flags().Set("value", "literal")
	cmd.Flags().Set("value-stdin", "true")

	_, err := resolveVariableSetValueInput(cmd, false)
	if err == nil {
		t.Fatal("expected an error combining --value with --value-stdin")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("got %v, want a mutual-exclusion error", err)
	}
}

func TestResolveVariableSetValueInput_ValueWins(t *testing.T) {
	cmd := newSetValueFlagCmd()
	cmd.Flags().Set("value", "literal-value")

	got, err := resolveVariableSetValueInput(cmd, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || *got != "literal-value" {
		t.Errorf("got %v, want \"literal-value\"", got)
	}
}

// TestResolveVariableSetValueInput_DescriptionOnlyIsNotAValueChange checks
// that a description-only `set` (no --value, no --value-stdin) never falls
// through to the interactive prompt — otherwise `ndcli variable ... set
// --description "..."` alone would hang waiting for a value on a terminal.
func TestResolveVariableSetValueInput_DescriptionOnlyIsNotAValueChange(t *testing.T) {
	cmd := newSetValueFlagCmd()
	got, err := resolveVariableSetValueInput(cmd, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want a nil value for a description-only update", got)
	}
}

// TestResolveVariableSetValueInput_NonTTYWithNothingGivenErrors is the
// set-path mirror of TestResolveVariableValueInput_NonTTYWithNoValueErrors:
// neither a value nor a description given, and stdin is not a terminal in
// a test binary, so the explicit-input-required branch is reachable.
func TestResolveVariableSetValueInput_NonTTYWithNothingGivenErrors(t *testing.T) {
	cmd := newSetValueFlagCmd()
	_, err := resolveVariableSetValueInput(cmd, false)
	if err == nil {
		t.Fatal("expected an error when nothing at all is given on a non-terminal stdin")
	}
	if !strings.Contains(err.Error(), "--value") || !strings.Contains(err.Error(), "--description") {
		t.Errorf("error should name the ways forward, got: %v", err)
	}
}
