package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Device management commands",
}

var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List devices",
	RunE:  runDeviceList,
}

var deviceApproveCmd = &cobra.Command{
	Use:               "approve [device]",
	Short:             "Approve a pending device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completePendingDevices,
	RunE:              runDeviceApprove,
}

var deviceRenameCmd = &cobra.Command{
	Use:               "rename [device] [new-name]",
	Short:             "Rename a device",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceRename,
}

var deviceRemoveCmd = &cobra.Command{
	Use:               "remove [device]",
	Short:             "Remove a device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceRemove,
}

var deviceDescribeCmd = &cobra.Command{
	Use:               "describe [device]",
	Short:             "Show device details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceDescribe,
}

var deviceApproveAllCmd = &cobra.Command{
	Use:   "approve-all",
	Short: "Approve all pending devices",
	RunE:  runDeviceApproveAll,
}

var deviceConnectCmd = &cobra.Command{
	Use:               "connect [device]",
	Short:             "Connect to a device via Pathfinder",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceConnect,
}

var deviceRebindTokenCmd = &cobra.Command{
	Use:               "rebind-token [device]",
	Short:             "Issue a one-time re-bind token for the device's signing key",
	Long: `Issue a one-time Re-bind Token authorising a fresh signing-key binding for a device.

Use this when:
  • A device's signing privkey is suspected leaked (key rotation).
  • The device hardware was replaced and the new install needs to bind a fresh keypair.
  • The agent's persistent state was lost and the per-device replay counter must reset.
  • You're cutting over an existing v=1 device to v=2.

The command atomically clears the device's bound pubkey, resets its response sequence
counter, flips status to UNREGISTERED, and stores a SHA-256 hash of the issued token
(default 24h validity, capped at 7d). The raw token is printed once — there is no way
to recover it later.

Operator workflow:
  1. Run this command to receive the raw token.
  2. Paste it into the device's OPNsense plugin under "Re-bind Token" (Show Advanced).
  3. The agent automatically rotates its keypair and re-registers with the new pubkey.
  4. Re-approve the device with 'ndcli device approve <name>'.
  5. Operator clears the GUI Re-bind Token field after the device shows ENABLED.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceRebindToken,
}

func init() {
	deviceCmd.AddCommand(deviceListCmd)
	deviceCmd.AddCommand(deviceApproveCmd)
	deviceCmd.AddCommand(deviceRenameCmd)
	deviceCmd.AddCommand(deviceRemoveCmd)
	deviceCmd.AddCommand(deviceDescribeCmd)
	deviceCmd.AddCommand(deviceApproveAllCmd)
	deviceCmd.AddCommand(deviceConnectCmd)
	deviceCmd.AddCommand(deviceRebindTokenCmd)

	// Connect flags
	deviceConnectCmd.Flags().Duration("timeout", 5*time.Minute, "Connection timeout")
	deviceConnectCmd.Flags().Int("webadmin-port", 0, "Local port for webadmin tunnel (default: auto-assign)")
	deviceConnectCmd.Flags().Bool("no-webadmin", false, "Disable webadmin tunnel")

	// Rebind-token flags
	deviceRebindTokenCmd.Flags().Duration("ttl", 24*time.Hour, "Token validity window (max 7d)")

	// List flags
	deviceListCmd.Flags().String("status", "", "Filter by status (PENDING, ENABLED, DISABLED)")
	deviceListCmd.Flags().String("ou", "", "Filter by organizational unit")
	deviceListCmd.Flags().String("name", "", "Filter by name (regex)")
	deviceListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction (e.g., name:asc, created_at:desc)")
	deviceListCmd.Flags().Int("page", 1, "Page number")
	deviceListCmd.Flags().Int("per-page", 30, "Items per page")
	deviceListCmd.Flags().String("heartbeat-after", "", "Filter by heartbeat date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	deviceListCmd.Flags().String("heartbeat-before", "", "Filter by heartbeat date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	deviceListCmd.Flags().String("synced-after", "", "Filter by synced date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	deviceListCmd.Flags().String("synced-before", "", "Filter by synced date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	deviceListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	deviceListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
}

func runDeviceList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	status, _ := cmd.Flags().GetString("status")
	ou, _ := cmd.Flags().GetString("ou")
	name, _ := cmd.Flags().GetString("name")
	sortBy, _ := cmd.Flags().GetString("sort-by")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	heartbeatAfter, _ := cmd.Flags().GetString("heartbeat-after")
	heartbeatBefore, _ := cmd.Flags().GetString("heartbeat-before")
	syncedAfter, _ := cmd.Flags().GetString("synced-after")
	syncedBefore, _ := cmd.Flags().GetString("synced-before")
	createdAfter, _ := cmd.Flags().GetString("created-after")
	createdBefore, _ := cmd.Flags().GetString("created-before")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	if status != "" {
		params["status"] = status
	}
	if ou != "" {
		params["ou"] = ou
	}
	if name != "" {
		params["name"] = name
	}
	if sortBy != "" {
		params["sort_by"] = sortBy
	}
	if heartbeatAfter != "" {
		parsed, err := helpers.ParseTimeFilter(heartbeatAfter)
		if err != nil {
			return fmt.Errorf("invalid heartbeat-after value: %w", err)
		}
		params["heartbeat_after"] = parsed
	}
	if heartbeatBefore != "" {
		parsed, err := helpers.ParseTimeFilter(heartbeatBefore)
		if err != nil {
			return fmt.Errorf("invalid heartbeat-before value: %w", err)
		}
		params["heartbeat_before"] = parsed
	}
	if syncedAfter != "" {
		parsed, err := helpers.ParseTimeFilter(syncedAfter)
		if err != nil {
			return fmt.Errorf("invalid synced-after value: %w", err)
		}
		params["synced_after"] = parsed
	}
	if syncedBefore != "" {
		parsed, err := helpers.ParseTimeFilter(syncedBefore)
		if err != nil {
			return fmt.Errorf("invalid synced-before value: %w", err)
		}
		params["synced_before"] = parsed
	}
	if createdAfter != "" {
		parsed, err := helpers.ParseTimeFilter(createdAfter)
		if err != nil {
			return fmt.Errorf("invalid created-after value: %w", err)
		}
		params["created_after"] = parsed
	}
	if createdBefore != "" {
		parsed, err := helpers.ParseTimeFilter(createdBefore)
		if err != nil {
			return fmt.Errorf("invalid created-before value: %w", err)
		}
		params["created_before"] = parsed
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return err
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	devices := result.GetItems()
	if err := formatter.FormatDevices(devices, result.Total, result.Quota); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runDeviceApprove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/approve", org, deviceName), nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Device approved: %s", deviceName)
	return nil
}

func runDeviceRename(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	newName := args[1]

	payload := map[string]string{"new_name": newName}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/rename", org, deviceName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Device renamed: %s → %s", deviceName, newName)
	return nil
}

func runDeviceRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	if !helpers.Confirm(fmt.Sprintf("Remove device '%s'?", deviceName)) {
		fmt.Println("Cancelled")
		return nil
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, deviceName))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Device removed: %s", deviceName)
	return nil
}

func runDeviceDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, deviceName), nil)
	if err != nil {
		return err
	}

	var device models.Device
	if err := api.ParseResponse(resp, &device); err != nil {
		return err
	}

	return formatter.FormatDevice(&device)
}

func runDeviceApproveAll(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	params := map[string]string{
		"status":   "PENDING",
		"per_page": "500",
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return err
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	devices := result.GetItems()
	if len(devices) == 0 {
		fmt.Println("No pending devices found")
		return nil
	}

	if !helpers.Confirm(fmt.Sprintf("Approve all %d pending devices?", len(devices))) {
		fmt.Println("Cancelled")
		return nil
	}

	approved := 0
	failed := 0

	for _, device := range devices {
		resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/approve", org, device.Name), nil)
		if err != nil {
			color.Red("✗ Failed to approve %s: %s", device.Name, err)
			failed++
			continue
		}

		if err := api.ParseResponse(resp, nil); err != nil {
			color.Red("✗ Failed to approve %s: %s", device.Name, err)
			failed++
			continue
		}

		color.Green("✓ Approved: %s", device.Name)
		approved++
	}

	fmt.Println()
	fmt.Printf("Approved: %d, Failed: %d\n", approved, failed)
	return nil
}

func runDeviceConnect(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	timeout, _ := cmd.Flags().GetDuration("timeout")
	webadminPort, _ := cmd.Flags().GetInt("webadmin-port")
	noWebadmin, _ := cmd.Flags().GetBool("no-webadmin")

	ctx := context.Background()

	// Create spinner for connection progress
	spinner := output.NewConnectSpinner(deviceName)
	defer spinner.Stop()

	// Step 1: Initiate connection
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect", org, deviceName), nil)
	if err != nil {
		spinner.Stop()
		return err
	}

	var initResp models.ConnectInitResponse
	if err := api.ParseResponse(resp, &initResp); err != nil {
		spinner.Stop()
		return err
	}

	// Step 2: Poll for session readiness
	spinner.UpdateMessage("Waiting for device...")

	pollInterval := 3 * time.Second
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			spinner.Stop()
			return fmt.Errorf("connection timeout after %s", timeout)
		}

		resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s/connect-status", initResp.Task), nil)
		if err != nil {
			spinner.Stop()
			return err
		}

		var statusResp models.ConnectStatusResponse
		if err := api.ParseResponse(resp, &statusResp); err != nil {
			spinner.Stop()
			return err
		}

		switch statusResp.Status {
		case models.TaskStatusCompleted:
			if statusResp.Payload == "" {
				spinner.Stop()
				return fmt.Errorf("connection completed but no payload received")
			}

			var payload models.ConnectPayload
			if err := json.Unmarshal([]byte(statusResp.Payload), &payload); err != nil {
				spinner.Stop()
				return fmt.Errorf("failed to parse connect payload: %w", err)
			}

			if payload.PathfinderSession == "" {
				spinner.Stop()
				return fmt.Errorf("connection completed but no session ID in payload")
			}

			// Step 3: Connect via Pathfinder
			// Pass spinner callback to pathfinder
			client, err := pathfinder.NewClient(pathfinder.ClientConfig{
				SessionID:       payload.PathfinderSession,
				WebAdminEnabled: !noWebadmin,
				WebAdminPort:    webadminPort,
				OnProgress:      spinner.UpdateMessage,
				IsTTY:           spinner.IsTTY(),
			})
			if err != nil {
				spinner.Stop()
				return err
			}

			// Stop spinner before shell starts (pathfinder will show WebAdmin box)
			spinner.Stop()
			return client.Connect()

		case models.TaskStatusFailed:
			spinner.Stop()
			msg := statusResp.Message
			if msg == "" {
				msg = "unknown error"
			}
			return fmt.Errorf("connection failed: %s", msg)

		case models.TaskStatusCancelled:
			spinner.Stop()
			return fmt.Errorf("connection was cancelled")

		case models.TaskStatusExpired:
			spinner.Stop()
			return fmt.Errorf("connection task expired")
		}

		time.Sleep(pollInterval)
	}
}

// rebindTokenResponse mirrors the JSON returned by NDManager's
// /devices/{name}/issue-rebind-token endpoint.
type rebindTokenResponse struct {
	BootstrapToken string `json:"bootstrap_token"`
	ExpiresAt      string `json:"expires_at"`
	Message        string `json:"message"`
}

func runDeviceRebindToken(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	ttl, _ := cmd.Flags().GetDuration("ttl")
	if ttl <= 0 {
		return fmt.Errorf("--ttl must be positive")
	}

	body := map[string]int{"ttl_seconds": int(ttl.Seconds())}

	ctx := context.Background()
	resp, err := apiClient.Post(
		ctx,
		fmt.Sprintf("/api/v1/organizations/%s/devices/%s/issue-rebind-token", org, deviceName),
		body,
	)
	if err != nil {
		return err
	}

	var parsed rebindTokenResponse
	if err := api.ParseResponse(resp, &parsed); err != nil {
		return err
	}

	// JSON output: just the parsed body, machine-friendly.
	if outputFmt == "json" {
		out, _ := json.MarshalIndent(parsed, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	color.Green("✓ Re-bind token issued for device: %s", deviceName)
	fmt.Println()
	color.Yellow("Token (single-use, store securely — printed only once):")
	fmt.Printf("  %s\n", parsed.BootstrapToken)
	fmt.Println()
	color.Cyan("Expires:  %s", parsed.ExpiresAt)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. On the device, open the OPNsense plugin's NetDefense settings page.\n")
	fmt.Printf("  2. Toggle 'Show advanced fields'.\n")
	fmt.Printf("  3. Paste the token into the 'Re-bind Token' field and Save.\n")
	fmt.Printf("  4. The agent will rotate its keypair and re-register automatically.\n")
	fmt.Printf("  5. Approve with: ndcli device approve %s\n", deviceName)
	fmt.Printf("  6. Clear the OPNsense Re-bind Token GUI field once the device shows ENABLED.\n")

	return nil
}
