package cli

import (
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
)

// addYesFlag gives a command the -y/--yes that skips its confirmation prompt,
// so a script does not have to pipe "y" into it.
func addYesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

// confirmUnlessYes asks message on stderr unless the command was given -y/--yes.
func confirmUnlessYes(cmd *cobra.Command, message string) bool {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true
	}
	return helpers.Confirm(message)
}
