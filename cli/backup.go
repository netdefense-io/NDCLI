package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"syscall"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup management commands",
}

// Config subcommands
var backupConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Organization backup configuration",
}

var backupConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show backup configuration",
	RunE:  runBackupConfigShow,
}

var backupConfigSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Create or update backup configuration",
	RunE:  runBackupConfigSet,
}

var backupConfigDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete backup configuration",
	RunE:  runBackupConfigDelete,
}

var backupConfigEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable backup configuration",
	RunE:  runBackupConfigEnable,
}

var backupConfigDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable backup configuration",
	RunE:  runBackupConfigDisable,
}

var backupConfigTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test S3 connection",
	RunE:  runBackupConfigTest,
}

// Device backup commands
var backupStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List all device backup statuses",
	RunE:  runBackupStatus,
}

var backupShowCmd = &cobra.Command{
	Use:               "show [device]",
	Short:             "Show specific device backup status",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupShow,
}

var backupEnableCmd = &cobra.Command{
	Use:               "enable [device]",
	Short:             "Enable backup for a device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEnable,
}

var backupDisableCmd = &cobra.Command{
	Use:               "disable [device]",
	Short:             "Disable backup for a device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupDisable,
}

// Encryption key commands
var backupEncryptionKeyCmd = &cobra.Command{
	Use:   "encryption-key",
	Short: "Device backup encryption key management",
}

var backupEncryptionKeySetCmd = &cobra.Command{
	Use:               "set [device]",
	Short:             "Set device-specific encryption key",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEncryptionKeySet,
}

var backupEncryptionKeyRemoveCmd = &cobra.Command{
	Use:               "remove [device]",
	Short:             "Remove device encryption key override (use org default)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEncryptionKeyRemove,
}

func init() {
	// Register config subcommands
	backupConfigCmd.AddCommand(backupConfigShowCmd)
	backupConfigCmd.AddCommand(backupConfigSetCmd)
	backupConfigCmd.AddCommand(backupConfigDeleteCmd)
	backupConfigCmd.AddCommand(backupConfigEnableCmd)
	backupConfigCmd.AddCommand(backupConfigDisableCmd)
	backupConfigCmd.AddCommand(backupConfigTestCmd)

	// Register encryption-key subcommands
	backupEncryptionKeyCmd.AddCommand(backupEncryptionKeySetCmd)
	backupEncryptionKeyCmd.AddCommand(backupEncryptionKeyRemoveCmd)

	// Register main backup subcommands
	backupCmd.AddCommand(backupConfigCmd)
	backupCmd.AddCommand(backupStatusCmd)
	backupCmd.AddCommand(backupShowCmd)
	backupCmd.AddCommand(backupEnableCmd)
	backupCmd.AddCommand(backupDisableCmd)
	backupCmd.AddCommand(backupEncryptionKeyCmd)

	// Flags for backup config set
	backupConfigSetCmd.Flags().String("s3-endpoint", "", "S3 endpoint URL")
	backupConfigSetCmd.Flags().String("s3-bucket", "", "S3 bucket name")
	backupConfigSetCmd.Flags().String("s3-key-id", "", "S3 access key ID")
	backupConfigSetCmd.Flags().String("s3-access-key", "", "S3 secret access key (prompts if not provided)")
	backupConfigSetCmd.Flags().String("s3-folder", "", "S3 folder path (prefix) within the bucket")
	backupConfigSetCmd.Flags().String("schedule", "", "Cron schedule expression")
	backupConfigSetCmd.Flags().String("encryption-key", "", "Encryption key (prompts if not provided)")

	// Flags for backup status
	backupStatusCmd.Flags().Bool("enabled-only", false, "Show only devices with backup enabled")
	backupStatusCmd.Flags().String("status", "", "Filter by backup status (SUCCESS, FAILED, IN_PROGRESS)")
	backupStatusCmd.Flags().Int("page", 1, "Page number")
	backupStatusCmd.Flags().Int("per-page", 30, "Items per page (1-100)")
}

// Config commands

func runBackupConfigShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), nil)
	if err != nil {
		return err
	}

	var config models.BackupConfig
	if err := api.ParseResponse(resp, &config); err != nil {
		return err
	}

	return formatter.FormatBackupConfig(&config)
}

func runBackupConfigSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	s3Endpoint, _ := cmd.Flags().GetString("s3-endpoint")
	s3Bucket, _ := cmd.Flags().GetString("s3-bucket")
	s3KeyID, _ := cmd.Flags().GetString("s3-key-id")
	s3AccessKey, _ := cmd.Flags().GetString("s3-access-key")
	s3Folder, _ := cmd.Flags().GetString("s3-folder")
	schedule, _ := cmd.Flags().GetString("schedule")
	encryptionKey, _ := cmd.Flags().GetString("encryption-key")

	ctx := context.Background()

	// Check if config already exists to determine create vs update
	checkResp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), nil)
	isCreate := err != nil || checkResp.StatusCode == http.StatusNotFound
	if checkResp != nil {
		checkResp.Body.Close()
	}

	// Build payload with only provided fields
	payload := make(map[string]string)

	if isCreate {
		// For create, require all fields
		if s3Endpoint == "" || s3Bucket == "" || s3KeyID == "" || schedule == "" {
			return fmt.Errorf("for new configuration, all fields are required: --s3-endpoint, --s3-bucket, --s3-key-id, --schedule")
		}
		payload["s3_endpoint"] = s3Endpoint
		payload["s3_bucket"] = s3Bucket
		payload["s3_key_id"] = s3KeyID
		payload["schedule"] = schedule
		if s3Folder != "" {
			payload["s3_prefix"] = s3Folder
		}

		// Prompt for access key if not provided
		if s3AccessKey == "" {
			fmt.Print("S3 Access Key: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("failed to read access key: %w", err)
			}
			s3AccessKey = string(bytePassword)
		}
		payload["s3_access_key"] = s3AccessKey

		// Prompt for encryption key if not provided (required to enable backup)
		if encryptionKey == "" {
			fmt.Print("Encryption Key: ")
			byteKey, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("failed to read encryption key: %w", err)
			}
			encryptionKey = string(byteKey)
		}
		payload["encryption_key"] = encryptionKey
	} else {
		// For update, only include provided fields
		if s3Endpoint != "" {
			payload["s3_endpoint"] = s3Endpoint
		}
		if s3Bucket != "" {
			payload["s3_bucket"] = s3Bucket
		}
		if s3KeyID != "" {
			payload["s3_key_id"] = s3KeyID
		}
		if s3Folder != "" {
			payload["s3_prefix"] = s3Folder
		}
		if schedule != "" {
			payload["schedule"] = schedule
		}
		// For update, prompt for access key only if key ID is being changed
		if s3AccessKey == "" && s3KeyID != "" {
			fmt.Print("S3 Access Key: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("failed to read access key: %w", err)
			}
			s3AccessKey = string(bytePassword)
		}
		if s3AccessKey != "" {
			payload["s3_access_key"] = s3AccessKey
		}

		// Include encryption key if provided
		if encryptionKey != "" {
			payload["encryption_key"] = encryptionKey
		}

		if len(payload) == 0 {
			return fmt.Errorf("no fields provided to update")
		}
	}

	var resp *http.Response
	if isCreate {
		resp, err = apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), payload)
	} else {
		resp, err = apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), payload)
	}
	if err != nil {
		return err
	}

	var config models.BackupConfig
	if err := api.ParseResponse(resp, &config); err != nil {
		return err
	}

	if isCreate {
		color.Green("Backup configuration created")
	} else {
		color.Green("Backup configuration updated")
	}
	return nil
}

func runBackupConfigDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	if !helpers.Confirm("Delete backup configuration? This will also disable backup for all devices.") {
		fmt.Println("Cancelled")
		return nil
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Backup configuration deleted")
	return nil
}

func runBackupConfigEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	payload := map[string]string{"status": "ENABLED"}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config/status", org), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Backup configuration enabled")
	return nil
}

func runBackupConfigDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	payload := map[string]string{"status": "DISABLED"}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config/status", org), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Backup configuration disabled")
	return nil
}

func runBackupConfigTest(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config/test", org), nil)
	if err != nil {
		return err
	}

	var result models.BackupConfigTestResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatBackupConfigTest(&result)
}

// Device backup commands

func runBackupStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	enabledOnly, _ := cmd.Flags().GetBool("enabled-only")
	status, _ := cmd.Flags().GetString("status")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if enabledOnly {
		params["enabled"] = "true"
	}
	if status != "" {
		params["status"] = status
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/backups", org), params)
	if err != nil {
		return err
	}

	var result models.DeviceBackupListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatDeviceBackupStatuses(result.Items, result.Total, result.EnabledCount); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runBackupShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup", org, deviceName), nil)
	if err != nil {
		return err
	}

	var status models.DeviceBackupStatus
	if err := api.ParseResponse(resp, &status); err != nil {
		return err
	}

	return formatter.FormatDeviceBackupStatus(&status)
}

func runBackupEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	payload := map[string]bool{"enabled": true}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup", org, deviceName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Backup enabled for device: %s", deviceName)
	return nil
}

func runBackupDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]

	payload := map[string]bool{"enabled": false}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup", org, deviceName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Backup disabled for device: %s", deviceName)
	return nil
}

// Encryption key commands

func runBackupEncryptionKeySet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	deviceName := args[0]

	// Prompt for encryption key (secure input)
	fmt.Print("Encryption Key: ")
	byteKey, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("failed to read encryption key: %w", err)
	}

	if len(byteKey) == 0 {
		return fmt.Errorf("encryption key cannot be empty")
	}

	payload := map[string]string{"encryption_key": string(byteKey)}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx,
		fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup/encryption-key", org, deviceName),
		payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Encryption key set for device: %s", deviceName)
	return nil
}

func runBackupEncryptionKeyRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	deviceName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx,
		fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup/encryption-key", org, deviceName))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("Encryption key override removed for device: %s (will use organization default)", deviceName)
	return nil
}
