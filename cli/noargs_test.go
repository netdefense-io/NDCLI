package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// noArgsLeaves is every leaf of the tree whose Args rule refuses any
// positional argument.
func noArgsLeaves(t *testing.T) []*cobra.Command {
	t.Helper()
	prepareRootCommand()
	var leaves []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
		if !cmd.HasSubCommands() && takesNoArgs(cmd) {
			leaves = append(leaves, cmd)
		}
	}
	walk(rootCmd)
	if len(leaves) < 50 {
		t.Fatalf("found %d leaves that take no argument, want at least 50", len(leaves))
	}
	return leaves
}

// A word given to a command that takes none is refused as that, never as an
// unknown command: a bare cobra.NoArgs fails here. The shell scripts under
// `completion` are cobra's own commands and keep its rule.
func TestNoArgsLeavesRefuseInTheirOwnWords(t *testing.T) {
	for _, cmd := range noArgsLeaves(t) {
		if cmd.Parent().Name() == "completion" && cmd.Parent().Parent() == rootCmd {
			continue
		}
		err := cmd.Args(cmd, []string{"fw-01"})
		want := cmd.CommandPath() + ` takes no arguments, got "fw-01"`
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: refusal %v, want it to start with %q", cmd.CommandPath(), err, want)
		}
	}
}

// Tab on a command that takes no argument offers no file names.
func TestNoArgsLeavesCompleteNoFiles(t *testing.T) {
	for _, cmd := range noArgsLeaves(t) {
		if cmd.ValidArgsFunction == nil {
			t.Errorf("%s: no ValidArgsFunction, so Tab falls back to file names", cmd.CommandPath())
			continue
		}
		if comps, directive := cmd.ValidArgsFunction(cmd, nil, ""); len(comps) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s: completes %v with directive %d, want nothing and NoFileComp", cmd.CommandPath(), comps, directive)
		}
	}
}

// The commands whose targets come from flags name those flags in the refusal.
func TestNoArgsRefusalNamesTheTargetFlags(t *testing.T) {
	prepareRootCommand()
	const (
		syncExact = `choose devices with --device, a regex (--device '^fw\.01$' for that device alone), --ou or --template`
		syncPlain = "choose devices with --device (a regex), --ou or --template"
		run       = "choose devices with --device <name>, --ou <name> or --all"
	)
	for _, tc := range []struct{ path, word, hint string }{
		{"sync status", "fw.01", syncExact},
		{"sync apply", "fw.01", syncExact},
		{"sync apply", "fw 01", syncPlain},
		{"sync apply", "it's", syncPlain},
		{"device approve-all", "fw.01", "to approve one device, use ndcli device approve <device>"},
		{"run ping", "8.8.8.8", "the host to ping is --host; " + run},
		{"run restart", "fw.01", run},
		{"run backup", "fw.01", run},
	} {
		cmd, _, err := rootCmd.Find(strings.Fields(tc.path))
		if err != nil || cmd.CommandPath() != "ndcli "+tc.path {
			t.Fatalf("find %q: %v, %v", tc.path, cmd, err)
		}
		want := fmt.Sprintf("ndcli %s takes no arguments, got %q; %s", tc.path, tc.word, tc.hint)
		if err := cmd.Args(cmd, []string{tc.word}); err == nil || err.Error() != want {
			t.Errorf("%s %q: refusal %v, want %q", tc.path, tc.word, err, want)
		}
	}
}
