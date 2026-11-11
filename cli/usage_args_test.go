package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// usageArgRange reads the positional arguments a Use line advertises: <x> is
// required, [x] optional, and x... (also [x...], <x...>) repeats. Flags are
// skipped: [flags], a bracketed group such as [--track <a|b>], and a bare
// --track <a|b> with its value. A bare flag is assumed to take the <x> after
// it, so a boolean flag must not be written before a positional argument.
// maxArgs is -1 when the count is unbounded.
func usageArgRange(use string) (minArgs, maxArgs int) {
	tokens := strings.Fields(use)
	if len(tokens) == 0 {
		return 0, 0
	}
	inFlagGroup, flagValueNext := false, false
	for _, tok := range tokens[1:] {
		if inFlagGroup {
			inFlagGroup = !strings.HasSuffix(tok, "]")
			continue
		}
		if flagValueNext {
			flagValueNext = false
			if strings.HasPrefix(tok, "<") {
				continue
			}
		}
		switch {
		case tok == "[flags]":
			continue
		case strings.HasPrefix(tok, "[-"):
			inFlagGroup = !strings.HasSuffix(tok, "]")
			continue
		case strings.HasPrefix(tok, "-"):
			flagValueNext = !strings.Contains(tok, "=")
			continue
		}
		if strings.HasPrefix(tok, "<") {
			minArgs++
		}
		if maxArgs >= 0 {
			maxArgs++
		}
		if strings.HasSuffix(strings.TrimRight(tok, "]>"), "...") {
			maxArgs = -1
		}
	}
	return minArgs, maxArgs
}

func TestUsageArgRange(t *testing.T) {
	for _, tc := range []struct {
		use              string
		minArgs, maxArgs int
	}{
		{"", 0, 0},
		{"list", 0, 0},
		{"quota <org>", 1, 1},
		{"quota [org]", 0, 1},
		{"role <email> <role>", 2, 2},
		{"example <type> [id]", 1, 2},
		{"show [track] [flags]", 0, 1},
		{"set <device>... [--track <fast|regular|stable|unmanaged>] [--boot-environment=<bool>]", 1, -1},
		{"promote <version|series> --track <fast|regular|stable|all>", 1, 1},
		{"x --mode=<m> <name>", 1, 1},
		{"require-package [policy] [package...]", 0, -1},
		{"add <device...>", 1, -1},
	} {
		minArgs, maxArgs := usageArgRange(tc.use)
		if minArgs != tc.minArgs || maxArgs != tc.maxArgs {
			t.Errorf("usageArgRange(%q) = %d, %d, want %d, %d", tc.use, minArgs, maxArgs, tc.minArgs, tc.maxArgs)
		}
	}
}

// A Use line is the help a user reads to call the command, so it must
// describe what the Args rule accepts (issue #275: "quota [org]" read as
// optional on a command that takes exactly one argument). Group commands are
// TestEveryParentCommandRejectsUnknownSubcommand's.
func TestUsageLinesMatchTheirArgumentRules(t *testing.T) {
	prepareRootCommand()

	for _, root := range []*cobra.Command{orgCmd} {
		for _, cmd := range walkCommands(root) {
			if isGroupCommand(cmd) {
				continue
			}
			t.Run(cmd.CommandPath(), func(t *testing.T) {
				minArgs, maxArgs := usageArgRange(cmd.Use)
				if cmd.Args == nil {
					if minArgs != 0 || maxArgs != -1 {
						t.Errorf("Use %q has no Args rule, so cobra accepts any number of arguments", cmd.Use)
					}
					return
				}
				limit := maxArgs + 1
				if maxArgs < 0 {
					limit = minArgs + 2
				}
				for n := 0; n <= limit; n++ {
					args := make([]string, n)
					for i := range args {
						args[i] = fmt.Sprintf("arg%d", i)
					}
					want := n >= minArgs && (maxArgs < 0 || n <= maxArgs)
					got := cmd.Args(cmd, args) == nil
					if got != want {
						t.Errorf("Use %q: with %d argument(s) the usage line says accepted=%v, the Args rule says %v", cmd.Use, n, want, got)
					}
				}
			})
		}
	}
}
