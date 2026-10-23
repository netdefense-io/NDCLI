package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// `ndcli firmware` stages OPNsense firmware updates through three tracks. The
// commands are thin: each reads its flags, calls the matching service method
// and hands the result to a formatter, so a command that only changes state
// still prints through the formatter and `-f json` stays one document.

var firmwareCmd = &cobra.Command{
	Use:   "firmware",
	Short: "Firmware tracks: staged OPNsense updates",
	Long: `Stage OPNsense firmware updates across the devices of an organization.

Devices are moved, one by one, into one of three tracks - fast, regular and
stable - and each track has its own schedule of update windows. A device in a
track is held at an exact OPNsense release; a window moves it to the release
its track has approved and updates it. A release is approved for a track only
after it has soaked in the track before it (fast, then regular, then stable),
or when an operator promotes it.

Devices start out unmanaged. A track moves and updates its devices only while
the organization's plan includes firmware tracks and the configuration exists
and is enabled ('ndcli firmware config create'). Check the plan with
'ndcli org quota <org>' (Firmware Tracks).

A plan without firmware tracks refuses every firmware command as not available
on the current plan (FEATURE_NOT_AVAILABLE), reads included. The exception is an
organization that already has a configuration, one that lost the feature after
setting it up: no new steps are sent; devices still in a track are being
released. Its reads keep answering, so that wind-down stays visible, and four
changes still work: detaching a track from its schedule
('ndcli firmware track schedule <track> --no-schedule'), moving devices to
unmanaged ('ndcli firmware device set <device> --track unmanaged'), disabling
the configuration and deleting it. Every other change is refused.

Getting started:
  ndcli firmware config create
  ndcli firmware track schedule fast saturday-night
  ndcli firmware device set fw-a fw-b --track fast
  ndcli firmware status
  ndcli firmware rollout

A device in a track is refused 'ndcli run firmware-upgrade': take it out of its
track first ('ndcli firmware device set <device> --track unmanaged').

Major upgrades. A track can also move its devices to the next OPNsense series
(for example 26.1 to 26.7) at its update windows. It is off for every track
until an operator turns it on with 'ndcli firmware track set <track>
--auto-apply-major=true': an upgrade takes the device offline, reboots it three
times and has no automatic rollback. A series moves down the tracks like a
release does; the halt, promote and failure commands take --major to address a
series, and 'ndcli firmware rollout' shows where each one stands.`,
}

// firmwareCLIVocabulary is how the CLI names the way forward from a firmware
// refusal; service.FirmwareAdvice decides which refusal gets which advice.
var firmwareCLIVocabulary = service.FirmwareVocabulary{
	ConfigCreate:         "'ndcli firmware config create'",
	ConfigEnable:         "'ndcli firmware config enable'",
	DeviceStatus:         "'ndcli firmware status'",
	DeviceRelease:        "'ndcli firmware device set <device> --track unmanaged'",
	TrackSchedule:        "'ndcli firmware track schedule <track> <schedule>'",
	TrackDetach:          "'ndcli firmware track schedule <track> --no-schedule'",
	Rollout:              "'ndcli firmware rollout'",
	DeviceAdd:            "'ndcli firmware device set <device> --track <track>'",
	DeviceAddWithSetting: "'ndcli firmware device set <device> --track <track> --boot-environment=<bool>'",
}

// firmwareAdvice appends the CLI's way forward to a firmware refusal.
func firmwareAdvice(err error) error {
	return service.FirmwareAdvice(err, firmwareCLIVocabulary)
}

// ── config ───────────────────────────────────────────────────────────────────

var firmwareConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Organization firmware configuration",
}

var firmwareConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the firmware configuration and its three tracks",
	Args:  cobra.NoArgs,
	RunE:  runFirmwareConfigShow,
}

func runFirmwareConfigShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	cfg, err := svc.FirmwareConfigGet(context.Background(), org)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfig(cfg)
}

var firmwareConfigCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Enable firmware tracks for the organization",
	Long: `Create the organization's firmware configuration with its three tracks (fast,
regular, stable) at their default settings. No device is in a track and no
track has a schedule yet.

By default an update step applies the full update, base and kernel included,
and reboots the device. --no-reboot makes the steps apply the userland packages
only, leaving base and kernel for a later update.

Requires a plan that includes firmware tracks.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareConfigCreate,
}

func runFirmwareConfigCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	noReboot, _ := cmd.Flags().GetBool("no-reboot")
	cfg, err := svc.FirmwareConfigCreate(context.Background(), org, service.FirmwareConfigCreateOpts{NoReboot: noReboot})
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfig(cfg)
}

var firmwareConfigSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Change the default reboot behaviour of the update steps",
	Long: `Change whether the update steps reboot the device. With --reboot a step applies
the full update, base and kernel included, and reboots; with --no-reboot it
applies the userland packages only.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareConfigSet,
}

func runFirmwareConfigSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	reboot, _ := cmd.Flags().GetBool("reboot")
	noReboot, _ := cmd.Flags().GetBool("no-reboot")
	if reboot == noReboot {
		return &service.Error{Code: service.CodeInvalidInput, Message: "pass --reboot or --no-reboot"}
	}
	cfg, err := svc.FirmwareConfigUpdate(context.Background(), org, reboot)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfig(cfg)
}

var firmwareConfigEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable firmware tracks",
	Long: `Enable the firmware configuration. Enabling does not bring any device back into
a track; add them again with 'ndcli firmware device set'.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareConfigEnable,
}

func runFirmwareConfigEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	cfg, err := svc.FirmwareConfigSetStatus(context.Background(), org, true)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfig(cfg)
}

var firmwareConfigDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable firmware tracks and release every device in a track",
	Long: `Disable the firmware configuration. Every device in a track starts leaving it:
its agent restores the firmware setting the device had before it joined, and
the device becomes unmanaged. Enabling again does not bring them back.

Still works on a plan without firmware tracks while the organization has a
firmware configuration, so the configuration can be wound down.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareConfigDisable,
}

func runFirmwareConfigDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(
		"Disable firmware tracks? Every device in a track will leave it and its agent will restore the firmware setting it had.", force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	cfg, err := svc.FirmwareConfigSetStatus(context.Background(), org, false)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfig(cfg)
}

var firmwareConfigDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the firmware configuration",
	Long: `Delete the firmware configuration with its tracks, their schedules and the
approvals. Halts and the history stay.

Refused while any device is in a track or still leaving one: disable firmware
or move the devices to unmanaged first, and delete once they are released.

Still works on a plan without firmware tracks while the organization has a
firmware configuration, so the configuration can be wound down. Both ways of
releasing its devices, disabling firmware and moving devices to unmanaged, work
there too.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareConfigDelete,
}

func runFirmwareConfigDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(
		"Delete the firmware configuration with its tracks, schedules and approvals? This cannot be undone.", force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	result, err := svc.FirmwareConfigDelete(context.Background(), org)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && len(apiErr.BlockingResources) > 0 {
			header := apiErr.Message
			if apiErr.BlockingCount > len(apiErr.BlockingResources) {
				header = fmt.Sprintf("%s (%d devices in all, %d listed)", header, apiErr.BlockingCount, len(apiErr.BlockingResources))
			}
			return refusalWithList(err, header, "Devices still in a track:", apiErr.BlockingResources)
		}
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareConfigDeleted(result)
}

// ── tracks ───────────────────────────────────────────────────────────────────

var firmwareTrackCmd = &cobra.Command{
	Use:   "track",
	Short: "Track settings and schedules",
}

var firmwareTrackShowCmd = &cobra.Command{
	Use:   "show [track]",
	Short: "Show the tracks, or one track",
	Long: `Show a track's soak days, minimum evidence, schedule, next window, whether it
applies major upgrades, and device counts. Without a track, shows all three.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeFirmwareTrackArg(models.FirmwareTracks),
	RunE:              runFirmwareTrackShow,
}

func runFirmwareTrackShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	ctx := context.Background()
	if len(args) == 0 {
		tracks, err := svc.FirmwareTrackList(ctx, org)
		if err != nil {
			return firmwareAdvice(err)
		}
		return formatter.FormatFirmwareTracks(tracks)
	}
	track, err := svc.FirmwareTrackGet(ctx, org, args[0])
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareTrack(track)
}

var firmwareTrackSetCmd = &cobra.Command{
	Use:   "set <track>",
	Short: "Change a track's soak days, minimum evidence or major upgrades",
	Long: `Change when a release is taken by a track, or whether the track applies major
upgrades.

--soak-days is how long a release soaks before the track takes it: counted
from OPNsense publishing the release for the fast track, and from the release
meeting its evidence in the previous track for regular and stable.

--min-success is how many distinct devices of the previous track must have
reached a release before this track takes it. The fast track has no previous
track, so it takes 0 only.

Releases the track has already approved stay approved; the new values apply to
the decisions still to come.

--auto-apply-major=true makes the track's update windows move its devices to
the next OPNsense series (a major upgrade, for example 26.1 to 26.7) once the
series is approved for the track; off, the series is approved but no device is
asked to upgrade. It is off for every track, and turning it on asks for a
confirmation (--force skips the prompt, and is required when stdin is not a TTY)
because an upgrade takes the device offline, reboots it three times and has no
automatic rollback. --auto-apply-major=false cancels the upgrade steps that were
queued for the track's devices and not yet dispatched; one already running goes
on. It is changed on its own, not with --soak-days or --min-success. Turning it
on needs a plan that includes firmware tracks.`,
	Example: `  ndcli firmware track set regular --soak-days 7 --min-success 2
  ndcli firmware track set stable --soak-days 14
  ndcli firmware track set fast --auto-apply-major=true
  ndcli firmware track set fast --auto-apply-major=false`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeFirmwareTrackArg(models.FirmwareTracks),
	RunE:              runFirmwareTrackSet,
}

func runFirmwareTrackSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	var opts service.FirmwareTrackUpdateOpts
	if cmd.Flags().Changed("soak-days") {
		v, _ := cmd.Flags().GetInt("soak-days")
		opts.SoakDays = &v
	}
	if cmd.Flags().Changed("min-success") {
		v, _ := cmd.Flags().GetInt("min-success")
		opts.MinSuccess = &v
	}
	opts.AutoApplyMajor = explicitBoolFlag(cmd, "auto-apply-major")

	// Refuse a bad request before asking anyone anything.
	trackName, _, err := service.CheckFirmwareTrackUpdate(args[0], opts)
	if err != nil {
		return err
	}
	if opts.AutoApplyMajor != nil && *opts.AutoApplyMajor {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			fmt.Fprintln(os.Stderr, models.FirmwareMajorSwitchWarning())
		}
		ok, err := helpers.ConfirmOrForce(fmt.Sprintf("Turn major upgrades on for the %s track?", trackName), force)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "Cancelled")
			return nil
		}
	}
	track, err := svc.FirmwareTrackUpdate(context.Background(), org, trackName, opts)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareTrack(track)
}

var firmwareTrackScheduleCmd = &cobra.Command{
	Use:   "schedule <track> [schedule]",
	Short: "Attach a track's update windows to a schedule, or detach them",
	Long: `Run a track's update windows on a named schedule ('ndcli schedule create'
first). Each time the schedule fires, every managed device of the track gets one
update step. Tracks may share a schedule. Attaching again moves the track to
the new schedule; --no-schedule detaches it, and the track stops moving its
devices.

A track's schedule entry belongs to the track: it cannot be enabled, disabled
or removed through 'ndcli schedule tasks'.

Attaching a schedule needs a plan that includes firmware tracks. Detaching with
--no-schedule still works on a plan without them while the organization has a
firmware configuration, so the configuration can be wound down.`,
	Example: `  ndcli firmware track schedule fast saturday-night
  ndcli firmware track schedule stable --no-schedule`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: completeFirmwareTrackThenSchedule,
	RunE:              runFirmwareTrackSchedule,
}

func runFirmwareTrackSchedule(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	noSchedule, _ := cmd.Flags().GetBool("no-schedule")
	var schedule *string
	switch {
	case noSchedule && len(args) == 2:
		return &service.Error{Code: service.CodeInvalidInput, Message: "give a schedule name or --no-schedule, not both"}
	case !noSchedule && len(args) < 2:
		return &service.Error{Code: service.CodeInvalidInput, Message: "give the schedule name to attach, or --no-schedule to detach"}
	case !noSchedule:
		name := args[1]
		schedule = &name
	}
	track, err := svc.FirmwareTrackSetSchedule(context.Background(), org, args[0], schedule)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareTrack(track)
}

// ── devices ──────────────────────────────────────────────────────────────────

var firmwareDeviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Move devices between tracks, and their boot-environment setting",
}

var firmwareDeviceSetCmd = &cobra.Command{
	Use:   "set <device>... [--track <fast|regular|stable|unmanaged>] [--boot-environment=<bool>]",
	Short: "Move named devices into a track or back to unmanaged, or set their boot-environment setting",
	Long: `Move devices, named one by one, into a track or back to unmanaged, and/or set
their boot-environment setting. There is no OU and no pattern: a track is chosen
device by device. It is all or nothing for the names; an unknown device refuses
the whole request.

A device that is not in a track joins it as enrolling: its agent pins it at the
release it has installed, so a device never moves ahead of its track. Moving a
device between tracks needs no step, the pin stays where it is. Moving it to
unmanaged makes it leave the track: its agent restores the firmware setting the
device had.

Moving devices to a track needs a plan that includes firmware tracks and an
enabled configuration. Moving them to unmanaged still works on a plan without
firmware tracks while the organization has a firmware configuration, so the
configuration can be wound down. A device whose agent is too old for firmware
tracks, or that is disabled, is added but is sent nothing until that changes.

--boot-environment says whether the agent makes a ZFS boot environment before a
major upgrade of the device. It is on by default and best effort: only on ZFS,
only when the pool has room, and the agent never removes the environment it
made, so one copy accumulates per upgrade. It is a recovery path from the
console only. The setting belongs to the device's place in a track: give it
alone for devices already in a track, or together with --track to add them with
it; a device in no track has none (FIRMWARE_DEVICE_UNMANAGED), and it cannot be
given with --track unmanaged, because a device that leaves its track keeps no
setting. Changing it cancels that device's queued major-upgrade step, if it
has not been dispatched yet. Setting it is a change like moving a device into a
track, and needs a plan that includes firmware tracks.`,
	Example: `  ndcli firmware device set fw-a fw-b --track fast
  ndcli firmware device set fw-a --track unmanaged
  ndcli firmware device set fw-a --boot-environment=false
  ndcli firmware device set fw-c --track regular --boot-environment=false`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeFirmwareDevices,
	RunE:              runFirmwareDeviceSet,
}

func runFirmwareDeviceSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	var opts service.FirmwareDeviceAssignOpts
	opts.Track, _ = cmd.Flags().GetString("track")
	opts.BootEnvironment = explicitBoolFlag(cmd, "boot-environment")
	result, err := svc.FirmwareDeviceAssign(context.Background(), org, args, opts)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareAssignment(result)
}

var firmwareStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show where each device stands in the tracks",
	Long: `List every device of the organization with its track, state (enrolling,
managed, held or unenrolling), pinned release, the reason of the agent's last
hold, and the result of the last step. For a device in a track it also shows its
series, whether the next window would send it a major upgrade (and what stops
it when not), its boot-environment setting with what the last major upgrade did
about it, and the third-party repository or package that upgrade found. A device
the control plane released by itself (its release folder vanished, or it was out
of contact for 30 days) is unmanaged and says so until it is added again.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareStatus,
}

func runFirmwareStatus(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	opts := service.FirmwareDeviceListOpts{}
	opts.Track, _ = cmd.Flags().GetString("track")
	opts.State, _ = cmd.Flags().GetString("state")
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	list, err := svc.FirmwareDeviceList(context.Background(), org, opts)
	if err != nil {
		return firmwareAdvice(err)
	}
	if err := formatter.FormatFirmwareDevices(list); err != nil {
		return err
	}
	output.PrintPagination(list.Page, list.Total, list.PerPage)
	return nil
}

var firmwareHistoryCmd = &cobra.Command{
	Use:   "history <device>",
	Short: "Show a device's firmware history",
	Long: `Show every finished firmware step of a device, newest first: the action, its
outcome and reason, and the releases it moved between. A step that was a major
upgrade says so and names the series it moved to.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDevices,
	RunE:              runFirmwareHistory,
}

func runFirmwareHistory(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	history, err := svc.FirmwareHistory(context.Background(), org, args[0], page, perPage)
	if err != nil {
		return firmwareAdvice(err)
	}
	if err := formatter.FormatFirmwareHistory(history); err != nil {
		return err
	}
	output.PrintPagination(history.Page, history.Total, history.PerPage)
	return nil
}

// ── rollout ──────────────────────────────────────────────────────────────────

var firmwareRolloutCmd = &cobra.Command{
	Use:   "rollout",
	Short: "Show where every release stands on its way down the tracks",
	Long: `For each track and OPNsense series: the release the track moves its devices to
(the target), the next release it will take, the soak left on it (computed by
the control plane), the evidence reached against the one required, and whether
it has stalled. Below, the failures that block a release and the halts in force.

Then the major upgrades, per track and series: where each stands (soaking,
waiting for evidence, approved, halted, or path closed while OPNsense's upgrade
files are not open), whether the track applies it, how many of its devices are
eligible, already on the series, or upgraded by a track, what blocks it, and the
failures that block a series in the later tracks. Last, the control plane's check
of NetDefense's own package repository for the FreeBSD versions an upgrade
crosses.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareRollout,
}

func runFirmwareRollout(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	rollout, err := svc.FirmwareRollout(context.Background(), org)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareRollout(rollout)
}

var firmwareVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Halt, unhalt or promote a release or a major upgrade",
	Long: `Halt, unhalt or promote a release (26.7.5), or with --major a major upgrade,
which is addressed by its target series (26.7). A series is never a release:
26.7 is also the name of the GA release of that series, and --major is what says
which one is meant. Without --major these commands mean what they always meant.`,
}

var firmwareVersionHaltCmd = &cobra.Command{
	Use:   "halt <version|series>",
	Short: "Halt a release or a major upgrade: no track approves it",
	Long: `Halt a release (for example 26.7.5). No track approves it, and the steps queued
for it that have not been dispatched yet are cancelled. Devices already on it
stay on it: there is no rollback. The release must be one the control plane
knows from OPNsense's mirror. Lift the halt with 'ndcli firmware version unhalt'.

With --major the argument is the target series of a major upgrade (for example
26.7): no track approves the upgrade, and the upgrade steps queued for it that
have not been dispatched yet are cancelled. Devices already upgrading are left
alone, and devices already on the series stay on it. The series must be the
target of a supported upgrade.`,
	Example: `  ndcli firmware version halt 26.7.5 --reason "regression in the VPN stack"
  ndcli firmware version halt 26.7 --major --reason "the upgrade payload changed"`,
	Args: cobra.ExactArgs(1),
	RunE: runFirmwareVersionHalt,
}

// firmwareKindOf is the kind a --major flag selects: a major upgrade with it,
// a release without.
func firmwareKindOf(cmd *cobra.Command) string {
	if major, _ := cmd.Flags().GetBool("major"); major {
		return models.FirmwareKindMajor
	}
	return models.FirmwareKindMinor
}

func runFirmwareVersionHalt(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	reason, _ := cmd.Flags().GetString("reason")
	result, err := svc.FirmwareHalt(context.Background(), org, firmwareKindOf(cmd), args[0], reason)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareHalt(result)
}

var firmwareVersionUnhaltCmd = &cobra.Command{
	Use:   "unhalt <version|series>",
	Short: "Lift the halt on a release or a major upgrade",
	Long: `Lift the halt on a release; it competes for the tracks again. With --major the
argument is the target series of a major upgrade (for example 26.7).

Lifting the halt on a major upgrade lets a track with major upgrades on that has
approved the series upgrade every device that passes the gates at its next
window: offline for the length of the upgrade, three reboots, no automatic
rollback. So it asks for a confirmation (--force skips the prompt, and is
required when stdin is not a TTY). Lifting the halt on a release asks nothing.`,
	Example:           `  ndcli firmware version unhalt 26.7 --major`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeFirmwareHaltedVersions,
	RunE:              runFirmwareVersionUnhalt,
}

func runFirmwareVersionUnhalt(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	// Refuse a request that cannot run before asking anyone anything.
	kind, name, err := service.CheckFirmwareTarget(firmwareKindOf(cmd), args[0])
	if err != nil {
		return err
	}
	if kind == models.FirmwareKindMajor {
		force, _ := cmd.Flags().GetBool("force")
		ok, err := helpers.ConfirmOrForce(fmt.Sprintf("Lift the halt on the major upgrade to %s? "+
			"A track with major upgrades on that has approved it will then upgrade its eligible devices at its next window: "+
			"offline, three reboots, no automatic rollback.", name), force)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "Cancelled")
			return nil
		}
	}
	result, err := svc.FirmwareUnhalt(context.Background(), org, kind, name)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareHalt(result)
}

var firmwareVersionPromoteCmd = &cobra.Command{
	Use:   "promote <version|series> --track <fast|regular|stable|all>",
	Short: "Approve a release or a major upgrade in a track now, without waiting for its soak",
	Long: `Approve a release in one track, or in all three at once (--track all), without
waiting for its soak. This is the way an urgent security release goes out:
every device of a track that approved it installs it at the track's next
window. It is idempotent. The release must be one the control plane knows and
must not be halted; the configuration must be enabled.

With --major the argument is the target series of a major upgrade (for example
26.7). Approving it starts no upgrade by itself, but a track with major
upgrades on then upgrades every device that passes the gates at its next
window: offline for the length of the upgrade, three reboots, no automatic
rollback. The series must be the target of a supported upgrade, must not be
halted, and OPNsense's upgrade path for it must be open
(FIRMWARE_UPGRADE_NOT_OPEN otherwise).`,
	Example: `  ndcli firmware version promote 26.7.5 --track all
  ndcli firmware version promote 26.7.5 --track fast
  ndcli firmware version promote 26.7 --major --track fast`,
	Args: cobra.ExactArgs(1),
	RunE: runFirmwareVersionPromote,
}

func runFirmwareVersionPromote(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	track, _ := cmd.Flags().GetString("track")
	force, _ := cmd.Flags().GetBool("force")

	// Refuse a request that cannot run before asking anyone anything.
	normalized, err := service.NormalizeFirmwareChoice("track", track, models.FirmwarePromoteTracks)
	if err != nil {
		return err
	}
	kind, name, err := service.CheckFirmwareTarget(firmwareKindOf(cmd), args[0])
	if err != nil {
		return err
	}
	scope := "the " + normalized + " track"
	if normalized == models.FirmwareAllTracks {
		scope = "all three tracks"
	}
	question := fmt.Sprintf("Approve release %s in %s now, without waiting for its soak?", name, scope)
	if kind == models.FirmwareKindMajor {
		question = fmt.Sprintf("Approve the major upgrade to %s in %s now, without waiting for its soak? "+
			"A track with major upgrades on will then upgrade its eligible devices at its next window: "+
			"offline, three reboots, no automatic rollback.", name, scope)
	}
	ok, err := helpers.ConfirmOrForce(question, force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	result, err := svc.FirmwarePromote(context.Background(), org, kind, name, track)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwarePromotion(result)
}

var firmwareVersionHaltsCmd = &cobra.Command{
	Use:   "halts",
	Short: "List the halts on releases and major upgrades",
	Long: `List the halts of the organization, newest first. By default only the halts in
force; --all adds the ones that have been lifted.

By default only the halts of releases are listed. --kind major lists those of
major upgrades instead, each with its series, and --kind all lists both, told
apart by their kind.`,
	Example: `  ndcli firmware version halts --kind all`,
	Args:    cobra.NoArgs,
	RunE:    runFirmwareVersionHalts,
}

func runFirmwareVersionHalts(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	all, _ := cmd.Flags().GetBool("all")
	kind, _ := cmd.Flags().GetString("kind")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	list, err := svc.FirmwareHaltList(context.Background(), org, service.FirmwareHaltListOpts{
		IncludeLifted: all, Kind: kind, Page: page, PerPage: perPage,
	})
	if err != nil {
		return firmwareAdvice(err)
	}
	if err := formatter.FormatFirmwareHalts(list); err != nil {
		return err
	}
	output.PrintPagination(list.Page, list.Total, list.PerPage)
	return nil
}

// ── failures ─────────────────────────────────────────────────────────────────

var firmwareFailureCmd = &cobra.Command{
	Use:   "failure",
	Short: "Failures that block a release or a major upgrade",
}

var firmwareFailureClearCmd = &cobra.Command{
	Use:   "clear <version|series>",
	Short: "Clear the recorded failures that block a release or a major upgrade",
	Long: `A failed update step blocks the release it was moving a device to from reaching
the next track, until the same device later reaches it or an operator clears the
failure. Clearing lets the release move on; the records stay in the history.
Limit it to one track with --track, or clear every track.

With --major the argument is the target series of a major upgrade (for example
26.7): a failed upgrade, or one that never answered, blocks the series in the
later tracks only, and clearing the failures lets it move on. After a manual
rollback of a device, clear the failures it caused once the device is rebound.`,
	Example: `  ndcli firmware failure clear 26.7.4
  ndcli firmware failure clear 26.7 --major --track fast`,
	Args: cobra.ExactArgs(1),
	RunE: runFirmwareFailureClear,
}

func runFirmwareFailureClear(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	track, _ := cmd.Flags().GetString("track")
	result, err := svc.FirmwareFailuresClear(context.Background(), org, firmwareKindOf(cmd), args[0], track)
	if err != nil {
		return firmwareAdvice(err)
	}
	return formatter.FormatFirmwareFailuresCleared(result)
}

// ── completion ───────────────────────────────────────────────────────────────

// completeFirmwareTrackArg completes a track name as the first argument.
func completeFirmwareTrackArg(valid []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return valid, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFirmwareFlag completes the value of a flag that takes one of a fixed
// set of words (--track, --state).
func completeFirmwareFlag(valid []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return valid, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFirmwareTrackThenSchedule completes the track, then a schedule name.
func completeFirmwareTrackThenSchedule(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return models.FirmwareTracks, cobra.ShellCompDirectiveNoFileComp
	case 1:
		org := getOrganization()
		if org == "" || svc == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		result, err := svc.ScheduleList(context.Background(), org)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(result.Schedules))
		for _, s := range result.Schedules {
			names = append(names, s.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFirmwareDevices completes device names for a command that takes
// several, leaving out those already on the command line.
func completeFirmwareDevices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names, directive := fetchDeviceNames(cmd)
	remaining := make([]string, 0, len(names))
	for _, name := range names {
		if !slices.Contains(args, name) {
			remaining = append(remaining, name)
		}
	}
	return remaining, directive
}

// completeFirmwareHaltedVersions completes the releases whose halt is in force,
// or with --major the series.
func completeFirmwareHaltedVersions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	org := getOrganization()
	if org == "" || svc == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	list, err := svc.FirmwareHaltList(context.Background(), org, service.FirmwareHaltListOpts{Kind: firmwareKindOf(cmd), PerPage: 200})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	versions := make([]string, 0, len(list.Items))
	for _, h := range list.Items {
		versions = append(versions, h.Name())
	}
	return versions, cobra.ShellCompDirectiveNoFileComp
}

// ── wiring ───────────────────────────────────────────────────────────────────

func init() {
	firmwareConfigCreateCmd.Flags().Bool("no-reboot", false, "Make the update steps apply the userland packages only and not reboot (default: the full update, then a reboot)")

	firmwareConfigSetCmd.Flags().Bool("reboot", false, "Make the update steps apply the full update, base and kernel included, and reboot")
	firmwareConfigSetCmd.Flags().Bool("no-reboot", false, "Make the update steps apply the userland packages only and not reboot")
	firmwareConfigSetCmd.MarkFlagsMutuallyExclusive("reboot", "no-reboot")
	firmwareConfigSetCmd.MarkFlagsOneRequired("reboot", "no-reboot")

	firmwareConfigDisableCmd.Flags().Bool("force", false, "Skip the confirmation prompt (required when stdin is not a TTY)")
	firmwareConfigDeleteCmd.Flags().Bool("force", false, "Skip the confirmation prompt (required when stdin is not a TTY)")

	firmwareTrackSetCmd.Flags().Int("soak-days", 0, "Days a release soaks before the track takes it")
	firmwareTrackSetCmd.Flags().Int("min-success", 0, "Distinct devices of the previous track that must have reached a release first (0 for the fast track)")
	addExplicitBoolFlag(firmwareTrackSetCmd, "auto-apply-major", "Whether the track's windows move its devices to the next OPNsense series: --auto-apply-major=true (asks first; offline, three reboots, no automatic rollback) or =false (cancels the queued upgrade steps)")
	firmwareTrackSetCmd.Flags().Bool("force", false, "Skip the confirmation prompt of --auto-apply-major=true (required when stdin is not a TTY)")
	firmwareTrackSetCmd.MarkFlagsOneRequired("soak-days", "min-success", "auto-apply-major")
	firmwareTrackSetCmd.MarkFlagsMutuallyExclusive("auto-apply-major", "soak-days")
	firmwareTrackSetCmd.MarkFlagsMutuallyExclusive("auto-apply-major", "min-success")

	firmwareTrackScheduleCmd.Flags().Bool("no-schedule", false, "Detach the track from its schedule; it stops moving its devices")

	firmwareDeviceSetCmd.Flags().String("track", "", "Track to move the devices to: "+strings.Join(models.FirmwareAssignTracks, ", ")+" (this and/or --boot-environment)")
	addExplicitBoolFlag(firmwareDeviceSetCmd, "boot-environment", "Whether the agent makes a ZFS boot environment before a major upgrade of the devices: --boot-environment=true (the default for a device in a track) or =false")
	firmwareDeviceSetCmd.MarkFlagsOneRequired("track", "boot-environment")
	_ = firmwareDeviceSetCmd.RegisterFlagCompletionFunc("track", completeFirmwareFlag(models.FirmwareAssignTracks))

	firmwareStatusCmd.Flags().String("track", "", "Only devices in this track: "+strings.Join(models.FirmwareAssignTracks, ", "))
	firmwareStatusCmd.Flags().String("state", "", "Only devices in this state: "+strings.Join(models.FirmwareStates, ", "))
	firmwareStatusCmd.Flags().String("name", "", "Only devices whose name contains this text")
	firmwareStatusCmd.Flags().Int("page", 1, "Page number")
	firmwareStatusCmd.Flags().Int("per-page", 50, "Items per page (1-200)")
	_ = firmwareStatusCmd.RegisterFlagCompletionFunc("track", completeFirmwareFlag(models.FirmwareAssignTracks))
	_ = firmwareStatusCmd.RegisterFlagCompletionFunc("state", completeFirmwareFlag(models.FirmwareStates))

	firmwareHistoryCmd.Flags().Int("page", 1, "Page number")
	firmwareHistoryCmd.Flags().Int("per-page", 50, "Items per page (1-200)")

	firmwareVersionHaltCmd.Flags().String("reason", "", "Why the release or major upgrade is halted: plain text, one line (required)")
	firmwareVersionUnhaltCmd.Flags().Bool("force", false, "Skip the confirmation prompt of --major (required when stdin is not a TTY)")
	_ = firmwareVersionHaltCmd.MarkFlagRequired("reason")
	for _, cmd := range []*cobra.Command{
		firmwareVersionHaltCmd, firmwareVersionUnhaltCmd, firmwareVersionPromoteCmd, firmwareFailureClearCmd,
	} {
		cmd.Flags().Bool("major", false, "The argument is the target series of a major upgrade (for example 26.7), not a release")
	}

	firmwareVersionPromoteCmd.Flags().String("track", "", "Track to approve the release in: "+strings.Join(models.FirmwarePromoteTracks, ", ")+" (required)")
	firmwareVersionPromoteCmd.Flags().Bool("force", false, "Skip the confirmation prompt (required when stdin is not a TTY)")
	_ = firmwareVersionPromoteCmd.MarkFlagRequired("track")
	_ = firmwareVersionPromoteCmd.RegisterFlagCompletionFunc("track", completeFirmwareFlag(models.FirmwarePromoteTracks))

	firmwareVersionHaltsCmd.Flags().Bool("all", false, "Also list the halts that have been lifted")
	firmwareVersionHaltsCmd.Flags().String("kind", "", "Which halts to list: "+strings.Join(models.FirmwareHaltKinds, ", ")+" (default: releases only)")
	_ = firmwareVersionHaltsCmd.RegisterFlagCompletionFunc("kind", completeFirmwareFlag(models.FirmwareHaltKinds))
	firmwareVersionHaltsCmd.Flags().Int("page", 1, "Page number")
	firmwareVersionHaltsCmd.Flags().Int("per-page", 50, "Items per page (1-200)")

	firmwareFailureClearCmd.Flags().String("track", "", "Limit to one track: "+strings.Join(models.FirmwareTracks, ", ")+" (default: every track)")
	_ = firmwareFailureClearCmd.RegisterFlagCompletionFunc("track", completeFirmwareFlag(models.FirmwareTracks))

	firmwareConfigCmd.AddCommand(
		firmwareConfigShowCmd, firmwareConfigCreateCmd, firmwareConfigSetCmd,
		firmwareConfigEnableCmd, firmwareConfigDisableCmd, firmwareConfigDeleteCmd,
	)
	firmwareTrackCmd.AddCommand(firmwareTrackShowCmd, firmwareTrackSetCmd, firmwareTrackScheduleCmd)
	firmwareDeviceCmd.AddCommand(firmwareDeviceSetCmd)
	firmwareVersionCmd.AddCommand(
		firmwareVersionHaltCmd, firmwareVersionUnhaltCmd, firmwareVersionPromoteCmd, firmwareVersionHaltsCmd,
	)
	firmwareFailureCmd.AddCommand(firmwareFailureClearCmd)

	firmwareCmd.AddCommand(
		firmwareConfigCmd, firmwareTrackCmd, firmwareDeviceCmd, firmwareStatusCmd,
		firmwareRolloutCmd, firmwareHistoryCmd, firmwareVersionCmd, firmwareFailureCmd,
	)
}
