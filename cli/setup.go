package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/wizard"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the interactive setup wizard",
	Long: `Run the interactive setup wizard to configure NDCLI.

The wizard guides you through:
  - Authentication setup (login, credential storage)
  - Organization setup (create or join an organization)
  - Terminal preferences (output format)

This wizard runs automatically when NDCLI is first used without a config file.
You can also run it manually at any time with 'ndcli setup'.`,
	RunE: runSetup,
	// Override PersistentPreRunE to skip normal config loading
	// The wizard handles its own config initialization
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func runSetup(cmd *cobra.Command, args []string) error {
	w := wizard.New()
	return w.Run(context.Background())
}
