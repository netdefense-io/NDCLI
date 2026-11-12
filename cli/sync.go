package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronization commands",
}

// syncTargetsHint answers a word given to sync status or sync apply, which take
// their devices from flags only. --device is an unanchored regex, so the hint
// shows the anchored form for the word: `--device fw-01` would also sync
// fw-010.
func syncTargetsHint(word string) string {
	if plainName.MatchString(word) {
		return "choose devices with --device, a regex (--device '" + service.ExactNamePattern(word) + "' for that device alone), --ou or --template"
	}
	return "choose devices with --device (a regex), --ou or --template"
}

// syncOrgHelp says where a sync's organization comes from: the global
// -o/--org, as on every other command.
const syncOrgHelp = "The organization is the global -o/--org, or the configured default; " +
	"the filter flags select devices within it."

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status for devices",
	Long:  "Show sync status for the devices of an organization.\n\n" + syncOrgHelp,
	Args:  noArgsHint(syncTargetsHint),
	RunE:  runSyncStatus,
}

var syncApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Trigger sync for devices",
	Long:  "Trigger a sync for the devices of an organization.\n\n" + syncOrgHelp,
	Args:  noArgsHint(syncTargetsHint),
	RunE:  runSyncApply,
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	syncCmd.AddCommand(syncApplyCmd)

	// --device and --template are regex patterns; --ou is an exact name. No
	// local --org: it would hide the global -o/--org that selects the
	// organization.
	syncStatusCmd.Flags().String("device", "", "Filter by device name (regex pattern)")
	syncStatusCmd.Flags().String("ou", "", "Filter by exact OU name")
	syncStatusCmd.Flags().String("template", "", "Filter by template name (regex pattern) — devices whose effective OU→Template chain matches")
	syncStatusCmd.Flags().String("drift-status", "", "Filter by drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")

	syncApplyCmd.Flags().String("device", "", "Sync devices matching pattern (regex)")
	syncApplyCmd.Flags().String("ou", "", "Sync all devices in the OU with this exact name")
	syncApplyCmd.Flags().String("template", "", "Sync all devices whose OU→Template chain matches the template name (regex)")
	syncApplyCmd.Flags().String("drift-status", "", "Only sync devices with this drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")
	syncApplyCmd.Flags().Bool("force", false, "Force sync even if already synced")
	syncApplyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	syncApplyCmd.Flags().String("schedule", "", "Register as a recurring spec on this named schedule instead of syncing immediately.")
	_ = syncApplyCmd.RegisterFlagCompletionFunc("schedule", completeScheduleNames)

	// --ou takes an exact name, and the common --device/--template pattern
	// is one, so completion against the live list helps either way.
	for _, sub := range []*cobra.Command{syncStatusCmd, syncApplyCmd} {
		_ = sub.RegisterFlagCompletionFunc("device", completeDevices)
		_ = sub.RegisterFlagCompletionFunc("ou", completeOUs)
		_ = sub.RegisterFlagCompletionFunc("template", completeTemplates)
	}
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	filter := service.SyncFilter{}
	filter.Device, _ = cmd.Flags().GetString("device")
	filter.OU, _ = cmd.Flags().GetString("ou")
	filter.Template, _ = cmd.Flags().GetString("template")
	filter.DriftStatus, _ = cmd.Flags().GetString("drift-status")

	result, err := svc.SyncStatus(context.Background(), org, filter)
	if err != nil {
		return err
	}
	return formatter.FormatSyncStatus(result.Items, result.Total)
}

func runSyncApply(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	filter := service.SyncFilter{}
	filter.Device, _ = cmd.Flags().GetString("device")
	filter.OU, _ = cmd.Flags().GetString("ou")
	filter.Template, _ = cmd.Flags().GetString("template")
	filter.DriftStatus, _ = cmd.Flags().GetString("drift-status")
	filter.Schedule, _ = cmd.Flags().GetString("schedule")
	force, _ := cmd.Flags().GetBool("force")
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	ctx := context.Background()

	// The confirm prompt gates both the immediate-apply path and --schedule
	// registration: planting a persistent recurring spec deserves the same
	// device-list review as an immediate sync, and --yes still short-circuits
	// it for automation. The review is part of the prompt, so it goes to
	// stderr with it, as does a note that nothing matched: nothing was
	// applied, so there is no result for the formatter.
	if !skipConfirm {
		status, err := svc.SyncStatus(ctx, org, filter)
		if err != nil {
			return err
		}
		if len(status.Items) == 0 {
			fmt.Fprintln(os.Stderr, "No devices match the specified filters")
			return nil
		}

		fmt.Fprintf(os.Stderr, "Devices to sync (%d):\n", len(status.Items))
		table := output.NewStyledTable([]string{"Device", "OUs", "Last Sync"})
		for _, item := range status.Items {
			lastSync := "Never"
			if item.SyncedAt != nil {
				lastSync = item.SyncedAt.Format("2006-01-02 15:04")
			}
			table.Append([]string{
				item.DeviceName,
				item.GetOUsDisplay(),
				lastSync,
			})
		}
		table.RenderTo(os.Stderr)
		fmt.Fprintln(os.Stderr)

		prompt := fmt.Sprintf("Trigger sync for %d device(s)?", len(status.Items))
		if filter.Schedule != "" {
			prompt = fmt.Sprintf("Register a recurring sync spec on schedule %q for %d device(s)?", filter.Schedule, len(status.Items))
		}
		if !helpers.Confirm(prompt) {
			fmt.Fprintln(os.Stderr, "Cancelled")
			return nil
		}
	}

	if filter.Schedule != "" {
		spec, err := svc.SyncRegisterSpec(ctx, org, filter, force)
		if err != nil {
			return err
		}
		return formatter.FormatScheduledTaskRegisterResult(spec)
	}

	applied, err := svc.SyncApply(ctx, org, filter, force)
	if err != nil {
		return err
	}

	if err := formatter.FormatSyncApply(applied.Response); err != nil {
		return err
	}
	return service.SyncApplyRefusal(applied)
}
