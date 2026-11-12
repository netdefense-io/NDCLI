package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Device management commands",
}

var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List devices",
	Long:  "List the devices of an organization.\n\n" + models.DriftStatusMeaning,
	Args:  noArgs,
	RunE:  runDeviceList,
}

var deviceApproveCmd = &cobra.Command{
	Use:               "approve <device>",
	Short:             "Approve a pending device",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completePendingDevices,
	RunE:              runDeviceApprove,
}

var deviceRenameCmd = &cobra.Command{
	Use:               "rename <device> <new-name>",
	Short:             "Rename a device",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceRename,
}

var deviceRemoveCmd = &cobra.Command{
	Use:   "remove <device>",
	Short: "Remove a device",
	Long: `Remove a device.

Removal is permanent and triggers self-decommission on the device.

` + service.DeviceRemoveConsequence,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceRemove,
}

var deviceDescribeCmd = &cobra.Command{
	Use:               "describe <device>",
	Short:             "Show device details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceDescribe,
}

var deviceApproveAllCmd = &cobra.Command{
	Use:   "approve-all",
	Short: "Approve all pending devices",
	Args:  noArgsHint(staticHint("to approve one device, use ndcli device approve <device>")),
	RunE:  runDeviceApproveAll,
}

var deviceConnectCmd = &cobra.Command{
	Use:   "connect <device>",
	Short: "Connect to a device through the connection relay",
	Long: `Connect to a device through the NetDefense connection relay.

Opens an interactive PTY shell to the remote device and (unless --no-webadmin
is set) starts a local WebAdmin tunnel on an automatically assigned port.

In-session key bindings:
  Ctrl-C   Interrupt the foreground command on the remote device (does NOT
           close the session — the remote shell handles it normally).
  ~.       Force-quit the session. Type tilde then dot at the start of a new
           line (immediately after pressing Enter). Use this if the session is
           wedged and Ctrl-C has no effect.
  ~~       Send a literal ~ character (escape the escape).

The WebAdmin tunnel remains active after the shell exits until you press
Ctrl-C at the keep-alive prompt (or run with --webadmin-only to skip the
shell entirely). If stdin or stdout is not a terminal, as in a script or
under nohup, the shell is skipped; if stdout is not one, the tunnel's URL is
printed on stderr.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceConnect,
}

var deviceRebindTokenCmd = &cobra.Command{
	Use:   "rebind-token <device>",
	Short: "Issue a one-time re-bind token for the device's signing key",
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

// `ndcli device health <name>` — per-device drill-down backed by the
// dashboard endpoint's drill-down sibling. Returns the full agent
// snapshot: resources, disks, services, pending updates, cert expiries.
var deviceHealthCmd = &cobra.Command{
	Use:   "health <device>",
	Short: "Show the device's instantaneous health snapshot",
	Long: `Show the instantaneous health snapshot for a device.

Returns the full agent telemetry: uptime, load averages, mem/swap, disk
usage per mountpoint, OPNsense service states, pending OS/plugin
updates, and certificate expiries. No history — this is "right now".

The snapshot updates on every 60s heartbeat; heavy fields (services,
updates, certs) refresh every 15min on the agent and ride every
subsequent heartbeat. Snapshot age is rendered against the broker's
wallclock so the freshness is honest.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runDeviceHealth,
}

func init() {
	deviceCmd.AddCommand(deviceListCmd)
	deviceCmd.AddCommand(deviceApproveCmd)
	deviceCmd.AddCommand(deviceRenameCmd)
	deviceCmd.AddCommand(deviceRemoveCmd)
	addYesFlag(deviceRemoveCmd)
	deviceCmd.AddCommand(deviceDescribeCmd)
	deviceCmd.AddCommand(deviceHealthCmd)
	deviceCmd.AddCommand(deviceApproveAllCmd)
	deviceCmd.AddCommand(deviceConnectCmd)
	deviceCmd.AddCommand(deviceRebindTokenCmd)

	// Connect flags
	deviceConnectCmd.Flags().Duration("timeout", 5*time.Minute, "Connection timeout")
	deviceConnectCmd.Flags().Int("webadmin-port", 0, "Local port for webadmin tunnel (default: auto-assign)")
	deviceConnectCmd.Flags().Bool("no-webadmin", false, "Disable webadmin tunnel")
	deviceConnectCmd.Flags().Bool("webadmin-only", false, "Skip the interactive terminal and keep the webadmin tunnel open until you quit (Ctrl-C). Auto-enabled when the device refuses a terminal (e.g. read-only sessions) or when stdin or stdout is not a terminal.")

	// Rebind-token flags
	deviceRebindTokenCmd.Flags().Duration("ttl", 24*time.Hour, "Token validity window, 1s to 7d (a fraction of a second is rounded up)")

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
	deviceListCmd.Flags().String("drift-status", "", "Filter by drift status (IN_SYNC, DRIFT, NEVER_SYNCED, UNKNOWN, ERROR)")
	_ = deviceListCmd.RegisterFlagCompletionFunc("ou", completeOUs)
}

func runDeviceList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.DeviceListOpts{}
	opts.Status, _ = cmd.Flags().GetString("status")
	opts.OU, _ = cmd.Flags().GetString("ou")
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	opts.HeartbeatAfter, _ = cmd.Flags().GetString("heartbeat-after")
	opts.HeartbeatBefore, _ = cmd.Flags().GetString("heartbeat-before")
	opts.SyncedAfter, _ = cmd.Flags().GetString("synced-after")
	opts.SyncedBefore, _ = cmd.Flags().GetString("synced-before")
	opts.CreatedAfter, _ = cmd.Flags().GetString("created-after")
	opts.CreatedBefore, _ = cmd.Flags().GetString("created-before")
	opts.DriftStatus, _ = cmd.Flags().GetString("drift-status")

	result, err := svc.DeviceList(context.Background(), org, opts)
	if err != nil {
		return err
	}

	if err := formatter.FormatDevices(result.Devices, result.Total, result.Quota); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runDeviceApprove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	if err := svc.DeviceApprove(context.Background(), org, deviceName); err != nil {
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
	if err := svc.DeviceRename(context.Background(), org, deviceName, newName); err != nil {
		return err
	}
	color.Green("✓ Device renamed: %s → %s", deviceName, newName)
	return nil
}

// deviceRemoveWarningLines is the pre-confirmation warning for a removal. It
// is returned rather than printed so the wording is testable, and it renders
// service.DeviceRemoveConsequence verbatim rather than a paraphrase, so this
// surface cannot drift from the MCP tool and the TUI modal.
func deviceRemoveWarningLines(deviceName string) []string {
	return []string{
		fmt.Sprintf("⚠ Removing device '%s':", deviceName),
		"  " + service.DeviceRemoveConsequence,
	}
}

func runDeviceRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	for _, line := range deviceRemoveWarningLines(deviceName) {
		_, _ = color.New(color.FgRed).Fprintln(os.Stderr, line)
	}
	if !confirmUnlessYes(cmd, fmt.Sprintf("Remove device '%s'?", deviceName)) {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	if err := svc.DeviceRemove(context.Background(), org, deviceName); err != nil {
		return err
	}
	color.Green("✓ Device removed: %s", deviceName)
	return nil
}

func runDeviceDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	device, err := svc.DeviceGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatDevice(device)
}

func runDeviceHealth(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	result, err := svc.DeviceHealth(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatDeviceHealth(result)
}

func runDeviceApproveAll(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ctx := context.Background()

	// Pre-flight count so we can prompt before issuing the bulk approval.
	listing, err := svc.DeviceList(ctx, org, service.DeviceListOpts{
		Status:  models.DeviceStatusPending,
		PerPage: 500,
	})
	if err != nil {
		return err
	}
	if len(listing.Devices) == 0 {
		fmt.Println("No pending devices found")
		return nil
	}
	if !helpers.Confirm(fmt.Sprintf("Approve all %d pending devices?", len(listing.Devices))) {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}

	approved, failed := 0, 0
	for _, d := range listing.Devices {
		if err := svc.DeviceApprove(ctx, org, d.Name); err != nil {
			color.Red("✗ Failed to approve %s: %s", d.Name, errorText(err))
			failed++
			continue
		}
		color.Green("✓ Approved: %s", d.Name)
		approved++
	}
	fmt.Println()
	fmt.Printf("Approved: %d, Failed: %d\n", approved, failed)
	return nil
}

// connectTerminals reports whether stdout and stdin are terminals. It is a
// variable so a test can stand in for them.
var connectTerminals = func() (stdout, stdin bool) {
	return term.IsTerminal(int(os.Stdout.Fd())), term.IsTerminal(int(os.Stdin.Fd()))
}

// connectRefusal refuses, before a CONNECT task is created, a connect that
// would open neither a shell nor a webadmin tunnel.
func connectRefusal(noWebadmin, webadminOnly, stdoutTTY, stdinTTY bool) error {
	if !noWebadmin {
		return nil
	}
	if webadminOnly {
		return &service.Error{Code: service.CodeInvalidInput, Message: "--webadmin-only and --no-webadmin leave nothing to connect"}
	}
	if !stdoutTTY || !stdinTTY {
		return &service.Error{Code: service.CodeInvalidInput, Message: "--no-webadmin leaves only the shell, and a shell needs a terminal on stdin and stdout"}
	}
	return nil
}

// bindWebAdminPort holds --webadmin-port before POST /connect, so a port in
// use is found before the device is asked for a session. With no shell to
// run, a busy port leaves nothing to serve and is refused; with one, it is
// left to the tunnel, which says so on stderr and lets the shell go on. A
// variable so a test can leave the port to the tunnel.
var bindWebAdminPort = func(port int, noWebadmin, webadminOnly, stdoutTTY, stdinTTY bool) (net.Listener, error) {
	if port <= 0 || noWebadmin {
		return nil, nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		return ln, nil
	}
	if stdoutTTY && stdinTTY && !webadminOnly {
		return nil, nil
	}
	return nil, &service.Error{Code: service.CodeInvalidInput, Message: fmt.Sprintf("--webadmin-port %d cannot be used: %v", port, err)}
}

// connectClientConfig is the relay client's configuration. The terminal
// reads are the ones connectRefusal judged, so the refusal and the shell
// decision follow one rule.
func connectClientConfig(sessionID string, webadminPort int, noWebadmin, webadminOnly bool, onProgress pathfinder.ProgressCallback, stdoutTTY, stdinTTY bool) pathfinder.ClientConfig {
	return pathfinder.ClientConfig{
		SessionID:       sessionID,
		WebAdminEnabled: !noWebadmin,
		WebAdminPort:    webadminPort,
		WebAdminOnly:    webadminOnly,
		OnProgress:      onProgress,
		IsTTY:           stdoutTTY,
		StdinIsTTY:      stdinTTY,
	}
}

// connectSignalContext ends when the user interrupts (Ctrl-C) or the process
// is told to stop, until stop is called. A variable so a test can stand in for
// the signal.
var connectSignalContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runDeviceConnect(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	timeout, _ := cmd.Flags().GetDuration("timeout")
	webadminPort, _ := cmd.Flags().GetInt("webadmin-port")
	noWebadmin, _ := cmd.Flags().GetBool("no-webadmin")
	webadminOnly, _ := cmd.Flags().GetBool("webadmin-only")
	stdoutTTY, stdinTTY := connectTerminals()
	if err := connectRefusal(noWebadmin, webadminOnly, stdoutTTY, stdinTTY); err != nil {
		return err
	}
	webadminListener, err := bindWebAdminPort(webadminPort, noWebadmin, webadminOnly, stdoutTTY, stdinTTY)
	if err != nil {
		return err
	}
	if webadminListener != nil {
		defer func() { _ = webadminListener.Close() }()
	}

	// Until the device pairs, an interrupt gives the connect up, so the CONNECT
	// task is cancelled below rather than left for the device to join. Once
	// paired, the session's own handling of Ctrl-C and signals takes over.
	ctx, stopSignals := connectSignalContext()
	defer stopSignals()

	// Create spinner for connection progress
	spinner := output.NewConnectSpinner(deviceName)
	defer spinner.Stop()

	// Step 1: Initiate connection
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect", org, deviceName), nil)
	if err != nil {
		spinner.Stop()
		return connectError(ctx, err)
	}

	var initResp models.ConnectInitResponse
	if err := api.ParseResponse(resp, &initResp); err != nil {
		spinner.Stop()
		return service.ConnectAdvice(err, connectAgentTooOldHint(deviceName))
	}

	// Whatever ends the connect before the user has a session (a timeout, an
	// interrupt, a failed poll, a relay that refused or never paired) leaves the
	// task to a device that would join a session nobody uses, or the device's
	// leg waiting on the relay. Cancelling it closes both; a task that already
	// ended on its own (FAILED, CANCELED, EXPIRED) or a session the user ended
	// needs nothing.
	settled := false
	defer func() {
		if !settled {
			// Hand the signals back first: a second Ctrl-C while the cancel
			// waits on a slow control plane must still end the process.
			stopSignals()
			_ = svc.CloseConnectSession(initResp.Task)
		}
	}()

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
			return connectError(ctx, err)
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
			cfg := connectClientConfig(payload.PathfinderSession, webadminPort, noWebadmin, webadminOnly, spinner.UpdateMessage, stdoutTTY, stdinTTY)
			cfg.WebAdminListener = webadminListener
			// Stopping the signal context also cancels ctx, so from here an
			// error is the session's own, not an interrupt.
			paired := false
			cfg.OnPaired = func() {
				paired = true
				stopSignals()
			}
			client, err := pathfinder.NewClient(cfg)
			if err != nil {
				spinner.Stop()
				return err
			}

			// Stop spinner before shell starts (pathfinder will show WebAdmin box)
			spinner.Stop()
			if err := client.ConnectContext(ctx); err != nil {
				if paired {
					return err
				}
				return connectError(ctx, err)
			}
			settled = true
			return nil

		case models.TaskStatusFailed:
			settled = true
			spinner.Stop()
			msg := statusResp.Message
			if msg == "" {
				msg = "unknown error"
			}
			return fmt.Errorf("connection failed: %s", msg)

		case models.TaskStatusCanceled:
			settled = true
			spinner.Stop()
			return fmt.Errorf("connection was cancelled")

		case models.TaskStatusExpired:
			settled = true
			spinner.Stop()
			return fmt.Errorf("connection task expired")
		}

		select {
		case <-ctx.Done():
			spinner.Stop()
			return connectError(ctx, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// connectAgentTooOldHint is the CLI's way forward when the control plane
// refuses a read-only session to the device's agent.
func connectAgentTooOldHint(device string) string {
	return "your role on this organization is read-only, so the control plane makes the session read-only; 'ndcli device describe " +
		device + "' shows the agent's version, and shows none when the device has not reported one"
}

// connectError is err, or "connect interrupted" once the user's interrupt is
// what ended it.
func connectError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("connect interrupted")
	}
	return err
}

func runDeviceRebindToken(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	ttl, _ := cmd.Flags().GetDuration("ttl")

	parsed, err := svc.DeviceRebindToken(context.Background(), org, deviceName, ttl)
	if err != nil {
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
