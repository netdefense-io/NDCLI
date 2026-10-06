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

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status for devices",
	RunE:  runSyncStatus,
}

var syncApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Trigger sync for devices",
	RunE:  runSyncApply,
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	syncCmd.AddCommand(syncApplyCmd)

	// --device and --template are regex patterns; --org and --ou are exact names.
	syncStatusCmd.Flags().String("device", "", "Filter by device name (regex pattern)")
	syncStatusCmd.Flags().String("ou", "", "Filter by exact OU name")
	syncStatusCmd.Flags().String("template", "", "Filter by template name (regex pattern) — devices whose effective OU→Template chain matches")
	syncStatusCmd.Flags().String("org", "", "Filter by exact organization name (defaults to current org)")
	syncStatusCmd.Flags().String("drift-status", "", "Filter by drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")

	// --device and --template are regex patterns; --org and --ou are exact names.
	syncApplyCmd.Flags().String("device", "", "Sync devices matching pattern (regex)")
	syncApplyCmd.Flags().String("ou", "", "Sync all devices in the OU with this exact name")
	syncApplyCmd.Flags().String("template", "", "Sync all devices whose OU→Template chain matches the template name (regex)")
	syncApplyCmd.Flags().String("org", "", "Filter by exact organization name (defaults to current org)")
	syncApplyCmd.Flags().String("drift-status", "", "Only sync devices with this drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")
	syncApplyCmd.Flags().Bool("force", false, "Force sync even if already synced")
	syncApplyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	syncApplyCmd.Flags().String("schedule", "", "Register as a recurring spec on this named schedule instead of syncing immediately.")
	_ = syncApplyCmd.RegisterFlagCompletionFunc("schedule", completeScheduleNames)

	// --org and --ou take exact names, and the common --device/--template
	// pattern is one, so completion against the live list helps either way.
	for _, sub := range []*cobra.Command{syncStatusCmd, syncApplyCmd} {
		_ = sub.RegisterFlagCompletionFunc("device", completeDevices)
		_ = sub.RegisterFlagCompletionFunc("ou", completeOUs)
		_ = sub.RegisterFlagCompletionFunc("template", completeTemplates)
		_ = sub.RegisterFlagCompletionFunc("org", completeOrganizations)
	}
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	filter := service.SyncFilter{}
	filter.Device, _ = cmd.Flags().GetString("device")
	filter.OU, _ = cmd.Flags().GetString("ou")
	filter.Template, _ = cmd.Flags().GetString("template")
	filter.Organization, _ = cmd.Flags().GetString("org")
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
	filter.Organization, _ = cmd.Flags().GetString("org")
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
	if applied.StatusCode == 400 && len(applied.Response.Errors) > 0 {
		return fmt.Errorf("sync failed for all devices")
	}
	return nil
}
