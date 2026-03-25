package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
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
	syncStatusCmd.Flags().String("org", "", "Filter by organization (regex pattern, defaults to current org)")

	// Apply flags - all support regex patterns
	syncApplyCmd.Flags().String("device", "", "Sync devices matching pattern (regex)")
	syncApplyCmd.Flags().String("ou", "", "Sync all devices in OUs matching pattern (regex)")
	syncApplyCmd.Flags().String("org", "", "Filter by organization (regex pattern, defaults to current org)")
	syncApplyCmd.Flags().Bool("force", false, "Force sync even if already synced")
	syncApplyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	device, _ := cmd.Flags().GetString("device")
	ou, _ := cmd.Flags().GetString("ou")
	orgFilter, _ := cmd.Flags().GetString("org")

	// Build query params
	params := map[string]string{}
	if device != "" {
		params["device"] = device
	}
	if ou != "" {
		params["ou"] = ou
	}
	// Use org filter or default to current org
	if orgFilter != "" {
		params["organization"] = orgFilter
	} else {
		params["organization"] = org
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/sync/status", params)
	if err != nil {
		return err
	}

	var result models.SyncStatusResponse
	if err := api.ParseResponse(resp, &result); err != nil {
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

	device, _ := cmd.Flags().GetString("device")
	ou, _ := cmd.Flags().GetString("ou")
	orgFilter, _ := cmd.Flags().GetString("org")
	force, _ := cmd.Flags().GetBool("force")
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	// Build query params
	params := map[string]string{}
	if device != "" {
		params["device"] = device
	}
	if ou != "" {
		params["ou"] = ou
	}
	// Use org filter or default to current org
	if orgFilter != "" {
		params["organization"] = orgFilter
	} else {
		params["organization"] = org
	}

	ctx := context.Background()

	// First, fetch matching devices to show user what will be affected
	if !skipConfirm {
		resp, err := apiClient.Get(ctx, "/api/v1/sync/status", params)
		if err != nil {
			return err
		}

		var status models.SyncStatusResponse
		if err := api.ParseResponse(resp, &status); err != nil {
			return err
		}

		if len(status.Items) == 0 {
			fmt.Println("No devices match the specified filters")
			return nil
		}

		// Show devices that will be affected
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

	// Add force flag for actual sync
	if force {
		params["force"] = "true"
	}

	resp, err := apiClient.PostWithParams(ctx, "/api/v1/sync", params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Parse response body - works for 200, 207 (mixed), and 400 (all fail)
	// All three return the same SyncApplyResponse structure
	var result models.SyncApplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Display the result (includes errors for 207 and 400)
	if err := formatter.FormatSyncApply(&result); err != nil {
		return err
	}

	// Return error for 400 status (all devices failed) to set non-zero exit code
	if resp.StatusCode == 400 && len(result.Errors) > 0 {
		return fmt.Errorf("sync failed for all devices")
	}

	return nil
}
