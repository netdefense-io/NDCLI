package cli

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
)

// noArgs is the Args rule of a command that takes no positional argument. It
// says so, where cobra.NoArgs answers "unknown command".
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("%s takes no arguments, got %q", cmd.CommandPath(), args[0])
}

// noArgsHint is noArgs for a command whose targets are chosen with flags: the
// refusal adds hint(word), the flags that do what the word most likely meant.
func noArgsHint(hint func(word string) string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := noArgs(cmd, args); err != nil {
			return fmt.Errorf("%w; %s", err, hint(args[0]))
		}
		return nil
	}
}

func staticHint(hint string) func(string) string {
	return func(string) string { return hint }
}

// plainName matches a word that can be shown inside single quotes as a shell
// argument and read as a device name.
var plainName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// takesNoArgs reports whether cmd's Args rule refuses any positional argument.
// It calls the rule before any flag is parsed, so a rule must be a pure check
// of the argument count, as every rule in cli is.
func takesNoArgs(cmd *cobra.Command) bool {
	return cmd.Args != nil && cmd.Args(cmd, nil) == nil && cmd.Args(cmd, []string{"x"}) != nil
}

// completeNothingWithoutArgs gives every leaf that takes no argument
// cobra.NoFileCompletions, so Tab offers its flags rather than file names.
// Without a ValidArgsFunction cobra falls back to the shell's file completion.
func completeNothingWithoutArgs(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		completeNothingWithoutArgs(sub)
	}
	if !cmd.HasSubCommands() && cmd.ValidArgsFunction == nil && takesNoArgs(cmd) {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}
}
