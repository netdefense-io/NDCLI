package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// walkCommands returns every command in the tree rooted at cmd, cmd included.
func walkCommands(cmd *cobra.Command) []*cobra.Command {
	out := []*cobra.Command{cmd}
	for _, sub := range cmd.Commands() {
		out = append(out, walkCommands(sub)...)
	}
	return out
}

// TestEveryParentCommandRejectsUnknownSubcommand is the regression net for the
// whole command tree: a parent group added without going through
// enforceSubcommands would silently print help and exit 0 for a mistyped
// subcommand, and fails here instead.
func TestEveryParentCommandRejectsUnknownSubcommand(t *testing.T) {
	prepareRootCommand()

	for _, cmd := range walkCommands(rootCmd) {
		// The root is the one parent cobra already guards: legacyArgs
		// rejects an unknown command there, from Find rather than from the
		// Args validator. TestGroupExecution covers it end to end.
		if cmd == rootCmd || !cmd.HasSubCommands() {
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			// Runnable, or cobra returns flag.ErrHelp before the Args
			// validator is ever consulted.
			if !cmd.Runnable() {
				t.Fatalf("%q has subcommands but no Run/RunE: cobra will print help and exit 0 for any unknown subcommand", cmd.CommandPath())
			}
			if err := cmd.ValidateArgs([]string{"definitely-not-a-subcommand"}); err == nil {
				t.Errorf("%q accepted an unknown subcommand", cmd.CommandPath())
			}
			if err := cmd.ValidateArgs(nil); err != nil {
				t.Errorf("%q rejected a bare invocation (help is a legitimate discovery path): %v", cmd.CommandPath(), err)
			}
		})
	}
}

// TestGroupCommandsAreAnnotated checks the inverse direction: every command
// enforceSubcommands touched must carry the annotation the usage template and
// PersistentPreRunE key off.
func TestGroupCommandsAreAnnotated(t *testing.T) {
	prepareRootCommand()

	for _, cmd := range walkCommands(rootCmd) {
		if cmd == rootCmd || !cmd.HasSubCommands() {
			continue
		}
		if !isGroupCommand(cmd) {
			t.Errorf("%q is a parent command but is not annotated as a group", cmd.CommandPath())
		}
	}
	if isGroupCommand(rootCmd) {
		t.Error("root command must not be treated as a group: it has its own Run")
	}
}

// TestGroupUsageOmitsFlagsLine renders the template rather than trusting that
// the annotation is set. The template reads the annotation by name, so a
// rename that missed it would leave the lookup empty, the negation always
// true, and every group advertising a "<path> [flags]" usage line it cannot
// honour — with nothing else to catch it.
func TestGroupUsageOmitsFlagsLine(t *testing.T) {
	prepareRootCommand()

	set, _, err := rootCmd.Find([]string{"config", "set"})
	if err != nil {
		t.Fatalf("find config set: %v", err)
	}
	if !isGroupCommand(set) {
		t.Fatal("config set should be a group command")
	}

	usage := set.UsageString()
	if strings.Contains(usage, "ndcli config set [flags]") {
		t.Errorf("a group must not advertise a [flags] usage line:\n%s", usage)
	}
	if !strings.Contains(usage, "ndcli config set [command]") {
		t.Errorf("the [command] usage line is missing:\n%s", usage)
	}

	// The same template must keep rendering the usage line for a genuine
	// leaf command, or the condition is simply suppressing it everywhere.
	org, _, err := rootCmd.Find([]string{"config", "set", "org"})
	if err != nil {
		t.Fatalf("find config set org: %v", err)
	}
	if leaf := org.UsageString(); !strings.Contains(leaf, "ndcli config set org") {
		t.Errorf("leaf command lost its usage line:\n%s", leaf)
	}
}

func TestUnknownSubcommandMessage(t *testing.T) {
	prepareRootCommand()

	set, _, err := rootCmd.Find([]string{"config", "set"})
	if err != nil {
		t.Fatalf("find config set: %v", err)
	}

	msg := unknownSubcommandMessage(set, "auth.storage")

	for _, want := range []string{
		`unknown subcommand "auth.storage"`,
		`for "ndcli config set"`,
		"Available subcommands:",
		"org",
		"output",
		"timezone",
		"Run 'ndcli config set --help' for more information.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestUnknownSubcommandMessageSuggests(t *testing.T) {
	prepareRootCommand()

	set, _, err := rootCmd.Find([]string{"config", "set"})
	if err != nil {
		t.Fatalf("find config set: %v", err)
	}

	msg := unknownSubcommandMessage(set, "outpt")
	if !strings.Contains(msg, "Did you mean this?") {
		t.Errorf("expected a suggestion for a near-miss, got:\n%s", msg)
	}
}

// TestGroupExecution drives the real root command end to end, which is the
// only way to prove the exit status: the bug was that cobra swallowed the
// unmatched argument and returned nil.
func TestGroupExecution(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantErr   bool
		wantInOut string
	}{
		{
			name:      "unknown nested subcommand errors",
			args:      []string{"config", "set", "auth.storage", "file"},
			wantErr:   true,
			wantInOut: `unknown subcommand "auth.storage"`,
		},
		{
			name:      "unknown top-level group subcommand errors",
			args:      []string{"auth", "bogus"},
			wantErr:   true,
			wantInOut: `unknown subcommand "bogus"`,
		},
		{
			name:      "unknown root command errors",
			args:      []string{"definitely-not-a-command"},
			wantErr:   true,
			wantInOut: `unknown command "definitely-not-a-command"`,
		},
		{
			name:      "bare group prints help and succeeds",
			args:      []string{"config", "set"},
			wantErr:   false,
			wantInOut: "Available Commands:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runRootCommand(t, tt.args...)
			if tt.wantErr && err == nil {
				t.Errorf("expected a non-nil error (non-zero exit) for %v", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %v: %v", tt.args, err)
			}
			if !strings.Contains(out, tt.wantInOut) {
				t.Errorf("output for %v missing %q:\n%s", tt.args, tt.wantInOut, out)
			}
		})
	}
}

// runRootCommand executes rootCmd with args and returns everything it wrote.
// The repo's custom help function writes straight to os.Stdout, so the real
// file descriptor has to be swapped, not just cobra's writers.
func runRootCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	prepareRootCommand()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	// Drain concurrently. Today's help text is far short of the pipe buffer,
	// but a writer that fills it would block inside Execute forever.
	piped := make(chan string, 1)
	go func() {
		var sink bytes.Buffer
		_, _ = io.Copy(&sink, r)
		piped <- sink.String()
	}()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)

	execErr := rootCmd.Execute()

	os.Stdout = origStdout
	w.Close()
	out := <-piped
	r.Close()

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)

	return out + buf.String(), execErr
}
