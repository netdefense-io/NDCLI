package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestIsCompletionCommand guards the gate that keeps startup warnings out of
// the completion exchange.
//
// The path that matters is `__complete`: it is cobra's hidden per-keystroke
// entry point and it runs the root PersistentPreRunE like any other command,
// so anything printed there lands in the middle of a Tab press. It reaches
// that code without going through initForCompletion at all, which is why
// silencing only initForCompletion would not have fixed the symptom.
func TestIsCompletionCommand(t *testing.T) {
	prepareRootCommand()

	// The hidden per-keystroke commands are installed by cobra's
	// initCompleteCmd during ExecuteC, so Find cannot reach them here; drive
	// the predicate with stand-ins carrying the same names. That the real
	// binary is quiet was confirmed by running it:
	//
	//	NDCLI_OAUTH2_DOMAIN=x ndcli __complete config set "" 2>err
	//
	// which left err holding only cobra's own directive line.
	for _, name := range []string{cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		stand := &cobra.Command{Use: name}
		rootCmd.AddCommand(stand)
		if !isCompletionCommand(stand) {
			t.Errorf("%q should be recognised as completion machinery", name)
		}
		rootCmd.RemoveCommand(stand)
	}

	for _, args := range [][]string{{"completion"}, {"completion", "bash"}, {"completion", "zsh"}} {
		cmd, _, err := rootCmd.Find(args)
		if err != nil {
			t.Errorf("find %v: %v", args, err)
			continue
		}
		if !isCompletionCommand(cmd) {
			t.Errorf("%q should be recognised as completion machinery", cmd.CommandPath())
		}
	}

	ordinaryPaths := [][]string{
		{"config", "show"},
		{"device", "list"},
		{"auth", "status"},
		{"version"},
	}
	for _, args := range ordinaryPaths {
		cmd, _, err := rootCmd.Find(args)
		if err != nil {
			continue // not every path exists in every build
		}
		if isCompletionCommand(cmd) {
			t.Errorf("%q is an ordinary command and must still get startup warnings", cmd.CommandPath())
		}
	}

	if isCompletionCommand(rootCmd) {
		t.Error("the root command must still get startup warnings")
	}
}
