package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// backupRunCLIVocabulary is how the CLI names the way forward from a refused
// on-demand backup; service.BackupRunAdvice decides which refusal gets which.
var backupRunCLIVocabulary = service.BackupRunVocabulary{
	ConfigCreate: "'ndcli backup config set'",
	ConfigEnable: "'ndcli backup config enable'",
	DeviceEnable: "'ndcli backup enable <device>'",
}

// errBackupRunNotDeferrable refuses --at and --schedule. A backup requested here
// starts now: recurring backups belong to the backup schedule, and the
// control plane refuses a deferred or scheduled one.
var errBackupRunNotDeferrable = &service.Error{
	Code: service.CodeInvalidInput,
	Message: "ndcli run backup starts a backup now and cannot be deferred or scheduled (--at, --schedule). " +
		"Recurring backups are set with 'ndcli backup config set --schedule <name>'",
}

// runBackupCmd is `ndcli run backup`. It is not built by newRunSubcommand: a
// backup does not go through POST /tasks (which refuses BACKUP, because the
// task's payload is the encryption key only the control plane resolves) but
// through its own route, and it has no payload, time or schedule.
var runBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up the device(s) now",
	Long: `Start a configuration backup now on the targeted devices.

Only devices that have backup enabled are backed up (see 'ndcli backup status').
A targeted device that has backup off, is disabled or waiting for approval, has
a backup already waiting or running, or has no encryption key is not backed up;
the command lists it with the reason. Backup is enabled per device with
'ndcli backup enable <device>'.

  Target (required):
    --device <name>   Back up a specific device. May be repeated.
    --ou <name>       Back up the devices of the OU that have backup enabled. May be repeated.
    --all             Back up every device in the org that has backup enabled. Use it alone.

  --device and --ou combine; --all cannot be combined with either.

  -o <name> chooses the organization, as on other commands. --org (no value) is
  the deprecated spelling of --all.

The backup starts now and cannot be deferred or scheduled: --at and --schedule
are refused. Recurring backups are set with 'ndcli backup config set --schedule <name>'.

Each task expires a minute after it is created. A device that is offline does
not back up later: its task ends EXPIRED. Follow the tasks with
'ndcli task list --type BACKUP'.

The encryption key is the device's own, else the organization's, set on the
control plane. It is never sent from here.`,
	Example: `  ndcli run backup --device fw-prod-01
  ndcli run backup --ou edge-routers
  ndcli run backup --all
  ndcli run backup --all -o branch-office`,
	Args: cobra.NoArgs,
	RunE: runRunBackup,
}

func init() {
	runBackupCmd.Flags().StringSlice("device", nil, "Back up this device (repeatable)")
	runBackupCmd.Flags().StringSlice("ou", nil, "Back up the devices of this OU that have backup enabled (repeatable)")
	addAllDevicesFlags(runBackupCmd, "Back up every device in the org that has backup enabled (not with --device or --ou)")

	// The scheduling flags every other run command takes exist here only to be
	// refused with an explanation, rather than with cobra's "unknown flag".
	runBackupCmd.Flags().String("at", "", "Not supported: a backup starts now")
	runBackupCmd.Flags().String("schedule", "", "Not supported: recurring backups are set with 'ndcli backup config set --schedule'")
	_ = runBackupCmd.Flags().MarkHidden("at")
	_ = runBackupCmd.Flags().MarkHidden("schedule")

	_ = runBackupCmd.RegisterFlagCompletionFunc("device", completeDevices)
	_ = runBackupCmd.RegisterFlagCompletionFunc("ou", completeOUs)

	runCmd.AddCommand(runBackupCmd)
}

func runRunBackup(cmd *cobra.Command, args []string) error {
	if cmd.Flags().Changed("at") || cmd.Flags().Changed("schedule") {
		return errBackupRunNotDeferrable
	}
	requireAuth()
	org := requireOrganization()

	devices, _ := cmd.Flags().GetStringSlice("device")
	ous, _ := cmd.Flags().GetStringSlice("ou")
	all := allDevicesFlag(cmd)

	if !all && len(devices) == 0 && len(ous) == 0 {
		return &service.Error{Code: service.CodeInvalidInput, Message: "at least one of --device, --ou, or --all is required"}
	}
	if all && (len(devices) > 0 || len(ous) > 0) {
		return &service.Error{Code: service.CodeInvalidInput, Message: "--all cannot be combined with --device or --ou"}
	}

	opts := service.BackupRunOpts{Devices: devices, OUs: ous, AllDevices: all}
	result, err := svc.BackupRun(context.Background(), org, opts)
	if err != nil {
		return service.BackupRunAdvice(err, opts, backupRunCLIVocabulary)
	}
	return formatter.FormatBackupRunResult(result)
}
