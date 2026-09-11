package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// groupAnnotation marks a command that exists only to hold subcommands.
// enforceSubcommands installs it; PersistentPreRunE and the usage template
// both read it.
const groupAnnotation = "ndcli_group"

// enforceSubcommands walks the tree rooted at cmd and makes every parent
// ("group") command reject an unknown subcommand instead of printing help and
// exiting 0.
//
// Cobra's own behaviour is the bug being fixed here: a command with no Run/RunE
// is not Runnable(), and Command.execute() returns flag.ErrHelp for it *before*
// the Args validator ever runs. ExecuteC turns that into "print help, exit 0",
// so `ndcli config set auth.storage file` silently succeeded while doing
// nothing. legacyArgs() only catches unknown commands at the root, never on a
// nested group.
//
// The fix gives every group a RunE (so it becomes Runnable and reaches its Args
// validator) plus groupArgs, which errors on any leftover argument. A bare
// group invocation with no arguments still prints help and exits 0 — that is a
// legitimate discovery path.
//
// Call this after cobra has installed its own built-in commands, so that the
// generated `completion` group is covered too.
func enforceSubcommands(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		enforceSubcommands(sub)
	}

	if cmd.Runnable() || !cmd.HasSubCommands() {
		return
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[groupAnnotation] = "true"
	cmd.Args = groupArgs
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }

	// Cobra leaves this at 0 and fills in its own default lazily, from inside
	// findSuggestions. Our error path never reaches that, and a zero distance
	// suggests nothing, so set it here rather than mutating the command from
	// what should be a read-only lookup.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
}

// isGroupCommand reports whether cmd is a parent command wired by
// enforceSubcommands.
func isGroupCommand(cmd *cobra.Command) bool {
	return cmd != nil && cmd.Annotations[groupAnnotation] == "true"
}

// groupArgs is the Args validator installed on every group command: no
// arguments means "show me what is here", anything else is a typo worth an
// error.
func groupArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("%s", unknownSubcommandMessage(cmd, args[0]))
}

// unknownSubcommandMessage renders the error body for an unmatched subcommand:
// what was not understood, the closest matches cobra can suggest, and the full
// list of what the group does accept.
func unknownSubcommandMessage(cmd *cobra.Command, arg string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown subcommand %q for %q\n", arg, cmd.CommandPath())

	if !cmd.DisableSuggestions {
		if suggestions := cmd.SuggestionsFor(arg); len(suggestions) > 0 {
			b.WriteString("\nDid you mean this?\n")
			for _, s := range suggestions {
				fmt.Fprintf(&b, "\t%s\n", s)
			}
		}
	}

	b.WriteString("\nAvailable subcommands:")
	lines := availableSubcommands(cmd)
	if len(lines) == 0 {
		// Unreachable in the current tree, but a group whose every child is
		// hidden or deprecated is one `Hidden: true` away, and a bare header
		// under it would read as a rendering bug.
		b.WriteString("\n  (none)")
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "\n  %s", line)
	}
	fmt.Fprintf(&b, "\n\nRun '%s --help' for more information.", cmd.CommandPath())

	return b.String()
}

// availableSubcommands returns one padded "name  short" line per visible
// subcommand, in alphabetical order.
func availableSubcommands(cmd *cobra.Command) []string {
	subs := make([]*cobra.Command, 0, len(cmd.Commands()))
	width := 0
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		subs = append(subs, sub)
		if len(sub.Name()) > width {
			width = len(sub.Name())
		}
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name() < subs[j].Name() })

	lines := make([]string, 0, len(subs))
	for _, sub := range subs {
		lines = append(lines, strings.TrimRight(fmt.Sprintf("%-*s  %s", width, sub.Name(), sub.Short), " "))
	}
	return lines
}
