package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
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

var backupConfigShowCmd = &cobra.Command{Use: "show", Short: "Show backup configuration", Args: noArgs, RunE: runBackupConfigShow}
var backupConfigSetCmd = &cobra.Command{Use: "set", Short: "Create or update backup configuration", Args: noArgs, RunE: runBackupConfigSet}
var backupConfigDeleteCmd = &cobra.Command{Use: "delete", Short: "Delete backup configuration", Args: noArgs, RunE: runBackupConfigDelete}
var backupConfigEnableCmd = &cobra.Command{Use: "enable", Short: "Enable backup configuration", Args: noArgs, RunE: runBackupConfigEnable}
var backupConfigDisableCmd = &cobra.Command{Use: "disable", Short: "Disable backup configuration", Args: noArgs, RunE: runBackupConfigDisable}
var backupConfigTestCmd = &cobra.Command{Use: "test", Short: "Test S3 connection", Args: noArgs, RunE: runBackupConfigTest}

// Device backup commands
var backupStatusCmd = &cobra.Command{Use: "status", Short: "List all device backup statuses", Args: noArgs, RunE: runBackupStatus}

var backupShowCmd = &cobra.Command{
	Use:               "show <device>",
	Short:             "Show specific device backup status",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupShow,
}

var backupEnableCmd = &cobra.Command{
	Use:               "enable <device>",
	Short:             "Enable backup for a device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEnable,
}

var backupDisableCmd = &cobra.Command{
	Use:               "disable <device>",
	Short:             "Disable backup for a device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupDisable,
}

// Encryption key commands
var backupEncryptionKeyCmd = &cobra.Command{Use: "encryption-key", Short: "Device backup encryption key management"}

var backupEncryptionKeySetCmd = &cobra.Command{
	Use:               "set <device>",
	Short:             "Set device-specific encryption key",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEncryptionKeySet,
}

var backupEncryptionKeyRemoveCmd = &cobra.Command{
	Use:               "remove <device>",
	Short:             "Remove device encryption key override (use org default)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runBackupEncryptionKeyRemove,
}

func init() {
	backupConfigCmd.AddCommand(backupConfigShowCmd)
	backupConfigCmd.AddCommand(backupConfigSetCmd)
	backupConfigCmd.AddCommand(backupConfigDeleteCmd)
	addYesFlag(backupConfigDeleteCmd)
	backupConfigCmd.AddCommand(backupConfigEnableCmd)
	backupConfigCmd.AddCommand(backupConfigDisableCmd)
	backupConfigCmd.AddCommand(backupConfigTestCmd)

	backupEncryptionKeyCmd.AddCommand(backupEncryptionKeySetCmd)
	backupEncryptionKeyCmd.AddCommand(backupEncryptionKeyRemoveCmd)
	addYesFlag(backupEncryptionKeyRemoveCmd)

	backupCmd.AddCommand(backupConfigCmd)
	backupCmd.AddCommand(backupStatusCmd)
	backupCmd.AddCommand(backupShowCmd)
	backupCmd.AddCommand(backupEnableCmd)
	backupCmd.AddCommand(backupDisableCmd)
	backupCmd.AddCommand(backupEncryptionKeyCmd)

	backupConfigSetCmd.Flags().String("s3-endpoint", "", "S3 endpoint URL")
	backupConfigSetCmd.Flags().String("s3-bucket", "", "S3 bucket name")
	backupConfigSetCmd.Flags().String("s3-key-id", "", "S3 access key ID")
	backupConfigSetCmd.Flags().String("s3-access-key", "", "S3 secret access key (prompts if not provided)")
	backupConfigSetCmd.Flags().String("s3-folder", "", "S3 folder path (prefix) within the bucket")
	backupConfigSetCmd.Flags().String("schedule", "", "Attach to a named Schedule (use 'ndcli schedule create' first). Pass an empty string or --no-schedule to detach.")
	backupConfigSetCmd.Flags().Bool("no-schedule", false, "Detach the backup from any attached schedule")
	backupConfigSetCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt of a detach")
	backupConfigSetCmd.Flags().String("encryption-key", "", "Encryption key (prompts if not provided)")
	backupConfigSetCmd.MarkFlagsMutuallyExclusive("schedule", "no-schedule")

	backupStatusCmd.Flags().Bool("enabled-only", false, "Show only devices with backup enabled")
	backupStatusCmd.Flags().String("status", "", "Filter by backup status (SUCCESS, FAILED, IN_PROGRESS)")
	backupStatusCmd.Flags().Int("page", 1, "Page number")
	backupStatusCmd.Flags().Int("per-page", 30, "Items per page (1-100)")
}

// warnPlaintextSecretFlag warns that a secret was passed inline on the
// command line, where it is visible in process listings and shell history.
func warnPlaintextSecretFlag(w io.Writer, flagName string) {
	color.New(color.FgYellow).Fprintf(w, "Warning: --%s was passed on the command line; its value is visible in process listings and shell history. Omit the flag to be prompted securely.\n", flagName)
}

// promptSecret reads a single line of secret input from the terminal.
func promptSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func runBackupConfigShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	cfg, err := svc.BackupConfigGet(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatBackupConfig(cfg)
}

func runBackupConfigSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	s3Endpoint, _ := cmd.Flags().GetString("s3-endpoint")
	s3Bucket, _ := cmd.Flags().GetString("s3-bucket")
	s3KeyID, _ := cmd.Flags().GetString("s3-key-id")
	s3AccessKey, _ := cmd.Flags().GetString("s3-access-key")
	s3Folder, _ := cmd.Flags().GetString("s3-folder")
	scheduleName, _ := cmd.Flags().GetString("schedule")
	noSchedule, _ := cmd.Flags().GetBool("no-schedule")
	encryptionKey, _ := cmd.Flags().GetString("encryption-key")

	if cmd.Flags().Changed("s3-access-key") && s3AccessKey != "" {
		warnPlaintextSecretFlag(os.Stderr, "s3-access-key")
	}
	if cmd.Flags().Changed("encryption-key") && encryptionKey != "" {
		warnPlaintextSecretFlag(os.Stderr, "encryption-key")
	}

	scheduleChanged := cmd.Flags().Changed("schedule") || noSchedule
	// Effective schedule target: empty string means detach.
	targetSchedule := scheduleName
	if noSchedule {
		targetSchedule = ""
	}

	ctx := context.Background()
	var result *models.ActionResult

	// ── S3 / key config (create or update) ───────────────────────────────────

	s3FlagsSet := s3Endpoint != "" || s3Bucket != "" || s3KeyID != "" ||
		s3AccessKey != "" || s3Folder != "" || encryptionKey != "" ||
		cmd.Flags().Changed("s3-endpoint") || cmd.Flags().Changed("s3-bucket") ||
		cmd.Flags().Changed("s3-key-id") || cmd.Flags().Changed("s3-folder") ||
		cmd.Flags().Changed("encryption-key")

	if s3FlagsSet || !scheduleChanged {
		// Decide create vs update by probing for an existing config.
		existing, err := svc.BackupConfigGet(ctx, org)
		isCreate := false
		if err != nil {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
				isCreate = true
			} else {
				isCreate = true
			}
		}

		if isCreate {
			if s3Endpoint == "" || s3Bucket == "" || s3KeyID == "" {
				return fmt.Errorf("for new configuration, --s3-endpoint, --s3-bucket, and --s3-key-id are required")
			}
			if s3AccessKey == "" {
				pw, err := promptSecret("S3 Access Key: ")
				if err != nil {
					return fmt.Errorf("failed to read access key: %w", err)
				}
				s3AccessKey = pw
			}
			if encryptionKey == "" {
				k, err := promptSecret("Encryption Key: ")
				if err != nil {
					return fmt.Errorf("failed to read encryption key: %w", err)
				}
				encryptionKey = k
			}
			cfg, err := svc.BackupConfigCreate(ctx, org, service.BackupConfigCreateOpts{
				S3Endpoint:    s3Endpoint,
				S3Bucket:      s3Bucket,
				S3KeyID:       s3KeyID,
				S3AccessKey:   s3AccessKey,
				S3Folder:      s3Folder,
				EncryptionKey: encryptionKey,
			})
			if err != nil {
				return err
			}
			result = &models.ActionResult{Resource: "config", Name: org, Value: cfg, Action: "created", Message: "Backup configuration created"}
		} else {
			opts := service.BackupConfigUpdateOpts{
				S3Endpoint:    s3Endpoint,
				S3Bucket:      s3Bucket,
				S3KeyID:       s3KeyID,
				S3AccessKey:   s3AccessKey,
				S3Folder:      s3Folder,
				EncryptionKey: encryptionKey,
			}
			// Prompt for access key only if key ID is being changed and the
			// key itself wasn't supplied.
			if opts.S3AccessKey == "" && opts.S3KeyID != "" {
				pw, err := promptSecret("S3 Access Key: ")
				if err != nil {
					return fmt.Errorf("failed to read access key: %w", err)
				}
				opts.S3AccessKey = pw
			}
			if opts.S3Endpoint == "" && opts.S3Bucket == "" && opts.S3KeyID == "" &&
				opts.S3AccessKey == "" && opts.S3Folder == "" && opts.EncryptionKey == "" {
				// No S3 fields changed; only schedule is being modified — skip
				// the update call entirely.
				if !scheduleChanged {
					// Nothing to change: the human formats print nothing, as
					// they always have, and -f json still gets its document.
					return formatter.FormatActionResult(&models.ActionResult{Resource: "config", Name: org, Value: existing, Action: "unchanged"})
				}
			} else {
				cfg, err := svc.BackupConfigUpdate(ctx, org, opts)
				if err != nil {
					return err
				}
				result = &models.ActionResult{Resource: "config", Name: org, Value: cfg, Action: "updated", Message: "Backup configuration updated"}
			}
		}
	}

	// ── Schedule attachment / detachment ──────────────────────────────────────

	if !scheduleChanged {
		return formatter.FormatActionResult(result)
	}

	// A create or update that already happened is reported even when the
	// schedule step is cancelled or fails.
	reportFirst := func(err error) error {
		if result != nil {
			if ferr := formatter.FormatActionResult(result); ferr != nil {
				return ferr
			}
		}
		return err
	}

	// When detaching, warn the user that backups will stop running.
	if targetSchedule == "" {
		warn := color.New(color.FgYellow)
		_, _ = warn.Fprintln(os.Stderr, "⚠  Detaching the schedule stops scheduled backups.")
		_, _ = warn.Fprintln(os.Stderr, "   The backup config stays configured but won't run until re-attached.")
		if ok, err := confirmUnlessYes(cmd, "Detach backup from its schedule?"); !ok {
			return reportFirst(err)
		}
	}

	sched, err := svc.BackupConfigSetSchedule(ctx, org, targetSchedule)
	if err != nil {
		return reportFirst(err)
	}
	return formatter.FormatActionResult(withScheduleResult(result, sched))
}

// withScheduleResult adds the schedule step to config set's result. Alone it
// is ndcli.backup.config_set_schedule's result; after a create or update it
// goes under "schedule", and its line under the config's.
func withScheduleResult(r *models.ActionResult, sched *service.BackupScheduleResult) *models.ActionResult {
	step := &models.ActionResult{Resource: "organization_name", Action: "detached"}
	if a := sched.Attached; a != nil {
		step.Resource, step.Name, step.Action = "schedule_name", a.ScheduleName, "attached"
		step.Details = map[string]interface{}{"code": a.Code, "kind": a.Kind, "enabled": a.Enabled, "created_by": a.CreatedBy, "created_at": a.CreatedAt}
		step.Message = fmt.Sprintf("Backup attached to schedule %q — spec code: %s", a.ScheduleName, a.Code)
	} else {
		d := sched.Detached
		step.Name = d.OrganizationName
		step.Details = map[string]interface{}{"detached": d.Detached}
		step.Message = fmt.Sprintf("Backup detached from schedule (org: %s)", d.OrganizationName)
	}
	if r == nil {
		return step
	}
	if r.Details == nil {
		r.Details = map[string]interface{}{}
	}
	r.Details["schedule"] = step
	r.Message += "\n" + step.Message
	return r
}

func runBackupConfigDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if ok, err := confirmUnlessYes(cmd, "Delete backup configuration? This will also disable backup for all devices."); !ok {
		return err
	}
	if err := svc.BackupConfigDelete(context.Background(), org); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "deleted",
		Message: "Backup configuration deleted"})
}

func runBackupConfigEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if err := svc.BackupConfigSetStatus(context.Background(), org, true); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "enabled",
		Details: map[string]interface{}{"status": "ENABLED"}, Message: "Backup configuration enabled"})
}

func runBackupConfigDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if err := svc.BackupConfigSetStatus(context.Background(), org, false); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "disabled",
		Details: map[string]interface{}{"status": "DISABLED"}, Message: "Backup configuration disabled"})
}

func runBackupConfigTest(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	result, err := svc.BackupConfigTest(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatBackupConfigTest(result)
}

func runBackupStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.BackupStatusOpts{}
	opts.EnabledOnly, _ = cmd.Flags().GetBool("enabled-only")
	opts.Status, _ = cmd.Flags().GetString("status")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")

	result, err := svc.BackupStatusList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatDeviceBackupStatuses(result.Items, result.Total, result.EnabledCount); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runBackupShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	st, err := svc.BackupStatusGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatDeviceBackupStatus(st)
}

func runBackupEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if err := svc.BackupSetEnabled(context.Background(), org, args[0], true); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "device", Name: args[0], Action: "enabled",
		Details: map[string]interface{}{"enabled": true}, Message: fmt.Sprintf("Backup enabled for device: %s", args[0])})
}

func runBackupDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if err := svc.BackupSetEnabled(context.Background(), org, args[0], false); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "device", Name: args[0], Action: "disabled",
		Details: map[string]interface{}{"enabled": false}, Message: fmt.Sprintf("Backup disabled for device: %s", args[0])})
}

func runBackupEncryptionKeySet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	deviceName := args[0]

	key, err := promptSecret("Encryption Key: ")
	if err != nil {
		return fmt.Errorf("failed to read encryption key: %w", err)
	}
	if err := svc.BackupEncryptionKeySet(context.Background(), org, deviceName, key); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "device", Name: deviceName, Action: "encryption_key_set",
		Message: fmt.Sprintf("Encryption key set for device: %s", deviceName)})
}

func runBackupEncryptionKeyRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	deviceName := args[0]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Remove the encryption key override of device '%s'? Its backups will use the organization default.", deviceName)); !ok {
		return err
	}
	if err := svc.BackupEncryptionKeyRemove(context.Background(), org, deviceName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "device", Name: deviceName, Action: "encryption_key_removed",
		Message: fmt.Sprintf("Encryption key override removed for device: %s (will use organization default)", deviceName)})
}
