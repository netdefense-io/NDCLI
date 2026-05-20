package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// `ndcli run` issues a pre-defined command to one or more devices. The
// underlying NDManager TaskType enum strings (PING, SHUTDOWN, ...) are
// internal — the user-facing names are simpler. This map is the ONLY
// place that translation happens.
var runFriendlyToTaskType = map[string]string{
	"ping":           models.TaskTypePing,
	"poweroff":       models.TaskTypeShutdown,
	"restart":        models.TaskTypeReboot,
	"plugin-install": models.TaskTypePluginInstall,
	"plugin-reload":  models.TaskTypeRestart,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a command on one or more devices",
	Long: `Issue a pre-defined command to one or more devices.

Use ` + "`ndcli run <command> --help`" + ` to see per-command flags. Every
sub-command accepts the same target and scheduling flags:

  Target (at least one required, repeatable):
    --device <name>   Target a specific device. May be repeated.
    --ou <name>       Target every enabled device in the OU. May be repeated.
    --org             Target every enabled device in the current org.

  Scheduling:
    --at <when>       Defer execution. Omit to run immediately. Accepts:
                        Relative offset:   30m, 2h, 3d, 1w
                        Local time:        2026-05-12 03:00          (uses your
                                                                      configured
                                                                      timezone)
                        Explicit timezone: 2026-05-12T03:00:00Z
                                           2026-05-12T03:00:00-03:00

Targets are the UNION of all --device + --ou + --org. Duplicates are deduped
server-side. An unresolved name rejects the whole batch.`,
}

func init() {
	pingCmd := newRunSubcommand(
		"ping",
		"Ping a target IP or hostname from the device(s)",
		func(cmd *cobra.Command, opts *service.RunOpts) error {
			host, _ := cmd.Flags().GetString("host")
			count, _ := cmd.Flags().GetInt("count")
			if host == "" {
				return &service.Error{Code: service.CodeInvalidInput, Message: "--host is required for ping"}
			}
			if count < 1 || count > 1000 {
				return &service.Error{Code: service.CodeInvalidInput, Message: "--count must be between 1 and 1000"}
			}
			payload := map[string]interface{}{"target": host}
			if count != 4 {
				payload["count"] = count
			}
			opts.Payload = payload
			return nil
		},
	)
	pingCmd.Flags().String("host", "", "Target IP or hostname to ping (required)")
	pingCmd.Flags().Int("count", 4, "Number of ping packets")
	_ = pingCmd.MarkFlagRequired("host")

	powerCmd := newRunSubcommand(
		"poweroff",
		"Power off the device(s)",
		nil,
	)

	restartCmd := newRunSubcommand(
		"restart",
		"Restart (reboot) the device(s)",
		nil,
	)

	pluginInstallCmd := newRunSubcommand(
		"plugin-install",
		"(Re)install the NDAgent OPNsense plugin pkg",
		func(cmd *cobra.Command, opts *service.RunOpts) error {
			version, _ := cmd.Flags().GetString("version")
			payload := map[string]interface{}{}
			if version != "" {
				payload["target_version"] = version
			}
			opts.Payload = payload
			return nil
		},
	)
	pluginInstallCmd.Flags().String("version", "", "Semver to pin the install to (empty = upgrade to latest in the device's installed channel)")

	pluginReloadCmd := newRunSubcommand(
		"plugin-reload",
		"Reload (restart) the NDAgent service on the device(s)",
		nil,
	)

	runCmd.AddCommand(pingCmd, powerCmd, restartCmd, pluginInstallCmd, pluginReloadCmd)
}

// newRunSubcommand builds a `ndcli run <name>` subcommand wired with the
// shared target + scheduling flags. `extra` populates command-specific
// payload from flags; pass nil for commands that take no payload params.
func newRunSubcommand(name, short string, extra func(*cobra.Command, *service.RunOpts) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			requireAuth()
			org := requireOrganization()

			taskType, ok := runFriendlyToTaskType[name]
			if !ok {
				return fmt.Errorf("internal: unknown run command %q", name)
			}

			devices, _ := cmd.Flags().GetStringSlice("device")
			ous, _ := cmd.Flags().GetStringSlice("ou")
			all, _ := cmd.Flags().GetBool("org")
			at, _ := cmd.Flags().GetString("at")

			if !all && len(devices) == 0 && len(ous) == 0 {
				return &service.Error{Code: service.CodeInvalidInput, Message: "at least one of --device, --ou, or --org is required"}
			}
			if all && (len(devices) > 0 || len(ous) > 0) {
				return &service.Error{Code: service.CodeInvalidInput, Message: "--org cannot be combined with --device or --ou"}
			}

			if at != "" {
				t, err := helpers.ParseFutureTime(at, output.Location())
				if err != nil {
					return &service.Error{Code: service.CodeInvalidInput, Message: fmt.Sprintf("--at: %v", err)}
				}
				// Allow a small backward skew so clock drift doesn't bite,
				// but reject obviously stale timestamps that look like typos.
				if t.Before(time.Now().Add(-30 * time.Second)) {
					return &service.Error{Code: service.CodeInvalidInput, Message: "--at is in the past"}
				}
				at = t.UTC().Format(time.RFC3339)
			}

			opts := service.RunOpts{
				Type:        taskType,
				Devices:     devices,
				OUs:         ous,
				AllDevices:  all,
				ScheduledAt: at,
			}
			if extra != nil {
				if err := extra(cmd, &opts); err != nil {
					return err
				}
			}

			result, err := svc.Run(context.Background(), org, opts)
			if err != nil {
				return err
			}
			return formatter.FormatRunResult(result)
		},
	}

	cmd.Flags().StringSlice("device", nil, "Target device name (repeatable)")
	cmd.Flags().StringSlice("ou", nil, "Target OU name (repeatable; expands to enabled members)")
	cmd.Flags().Bool("org", false, "Target every enabled device in the current org")
	cmd.Flags().String("at", "", "Schedule execution. Accepts:  relative (30m, 2h, 3d, 1w);  date+time in your configured timezone (2026-05-12 03:00);  RFC3339 with tz (2026-05-12T03:00:00Z, 2026-05-12T03:00:00-03:00).")

	cmd.MarkFlagsMutuallyExclusive("org", "device")
	cmd.MarkFlagsMutuallyExclusive("org", "ou")

	_ = cmd.RegisterFlagCompletionFunc("device", completeDevices)
	_ = cmd.RegisterFlagCompletionFunc("ou", completeOUs)

	return cmd
}

