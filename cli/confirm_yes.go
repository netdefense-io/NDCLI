package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
)

// addYesFlag gives a command the -y/--yes that skips its confirmation prompt,
// so a script does not have to pipe "y" into it.
func addYesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

// confirmUnlessYes asks message on stderr unless the command was given
// -y/--yes, and reports whether to go on. A "no" prints Cancelled on stderr
// and returns false with no error, so the command exits 0 having changed
// nothing. A prompt left unanswered on a stdin that is not a terminal is an
// error naming the flag: a script that forgot it fails instead of exiting 0.
// The usual call is
//
//	if ok, err := confirmUnlessYes(cmd, msg); !ok {
//		return err
//	}
func confirmUnlessYes(cmd *cobra.Command, message string) (bool, error) {
	yes, err := askUnlessYes(cmd, message, false)
	if err == nil && !yes {
		fmt.Fprintln(os.Stderr, "Cancelled")
	}
	return yes, err
}

// askUnlessYes asks message unless the command was given -y/--yes, and
// returns the answer, defaultYes deciding an empty line. It refuses an
// unanswered prompt as confirmUnlessYes does, and prints nothing for a no:
// the caller says what a no means.
func askUnlessYes(cmd *cobra.Command, message string, defaultYes bool) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true, nil
	}
	yes, answered := helpers.AskYesNo(message, defaultYes)
	if !answered && !helpers.StdinIsTerminal() {
		fmt.Fprintln(os.Stderr)
		return false, unansweredPromptError(cmd)
	}
	return yes, nil
}

// unansweredPromptError is the refusal of a prompt nobody could answer. It
// names the command's flag: -y where it has the shorthand, else --yes.
func unansweredPromptError(cmd *cobra.Command) error {
	flag := "--yes"
	if f := cmd.Flags().ShorthandLookup("y"); f != nil && f.Name == "yes" {
		flag = "-y"
	}
	return fmt.Errorf("the confirmation prompt got no answer (stdin is not a terminal): pass %s to confirm", flag)
}
