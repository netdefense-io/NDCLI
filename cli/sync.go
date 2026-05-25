package cli

import (
	"context"
	"fmt"

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

	// Status flags - all support regex patterns
	syncStatusCmd.Flags().String("device", "", "Filter by device name (regex pattern)")
	syncStatusCmd.Flags().String("ou", "", "Filter by organizational unit (regex pattern)")
	syncStatusCmd.Flags().String("template", "", "Filter by template name (regex pattern) — devices whose effective OU→Template chain matches")
	syncStatusCmd.Flags().String("org", "", "Filter by organization (regex pattern, defaults to current org)")
	syncStatusCmd.Flags().String("drift-status", "", "Filter by drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")

	// Apply flags - all support regex patterns
	syncApplyCmd.Flags().String("device", "", "Sync devices matching pattern (regex)")
	syncApplyCmd.Flags().String("ou", "", "Sync all devices in OUs matching pattern (regex)")
	syncApplyCmd.Flags().String("template", "", "Sync all devices whose OU→Template chain matches the template name (regex)")
	syncApplyCmd.Flags().String("org", "", "Filter by organization (regex pattern, defaults to current org)")
	syncApplyCmd.Flags().String("drift-status", "", "Only sync devices with this drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")
	syncApplyCmd.Flags().Bool("force", false, "Force sync even if already synced")
	syncApplyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")

	// The --device/--ou flags accept regex patterns but the common case is an
	// exact name, so completion against the live list helps either way.
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

	if len(result.Items) == 0 {
		fmt.Println("No devices found")
		return nil
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
	force, _ := cmd.Flags().GetBool("force")
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	ctx := context.Background()

	if !skipConfirm {
		status, err := svc.SyncStatus(ctx, org, filter)
		if err != nil {
			return err
		}
		if len(status.Items) == 0 {
			fmt.Println("No devices match the specified filters")
			return nil
		}

		fmt.Printf("Devices to sync (%d):\n", len(status.Items))
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
		table.Render()
		fmt.Println()

		if !helpers.Confirm(fmt.Sprintf("Trigger sync for %d device(s)?", len(status.Items))) {
			fmt.Println("Cancelled")
			return nil
		}
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
