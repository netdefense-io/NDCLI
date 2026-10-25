package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Firmware tracks: staged OPNsense updates. The LLM-facing twin of
// cli/firmware.go; every `ndcli firmware` command has a tool here, and both
// call the same service methods. Tools that change what a device will do —
// moving devices between tracks, promoting or halting a release or a major
// upgrade, changing a track's settings or schedule, disabling or deleting the
// configuration — return a preview unless confirm=true.
//
// A major upgrade (the move to the next OPNsense series, 26.1 to 26.7) is the
// same tools with more arguments, not more tools: track_set takes
// auto_apply_major, device_set takes boot_environment, and the tools that halt,
// unhalt, promote and clear failures take a kind (minor, the default, or major;
// with major the version argument names the target series).

// firmwareMCPVocabulary is how the MCP surface names the way forward from a
// firmware refusal; service.FirmwareAdvice decides which refusal gets which
// advice. A tool caller cannot run an ndcli command, so it is told the tool.
var firmwareMCPVocabulary = service.FirmwareVocabulary{
	ConfigCreate:         "ndcli.firmware.config_create",
	ConfigEnable:         "ndcli.firmware.config_enable",
	DeviceStatus:         "ndcli.firmware.status",
	DeviceRelease:        "ndcli.firmware.device_set with track=unmanaged",
	TrackSchedule:        "ndcli.firmware.track_schedule with track=<track>, schedule=<name>",
	TrackDetach:          "ndcli.firmware.track_schedule with track=<track>, no_schedule=true",
	Rollout:              "ndcli.firmware.rollout",
	DeviceAdd:            "ndcli.firmware.device_set with track=<track>",
	DeviceAddWithSetting: "ndcli.firmware.device_set with track=<track>, boot_environment=<bool>",
}

// firmwareError turns a service failure into the tool's error result, with the
// MCP way forward appended where the refusal has one.
func (s *Server) firmwareError(err error) (*mcp.CallToolResult, error) {
	return s.errorResult(service.FirmwareAdvice(err, firmwareMCPVocabulary))
}

// firmwareReasonGuide lists every reason code a device's firmware state or a
// ledger row can carry, with its meaning. It is in the descriptions of the
// tools that return them, so a caller can read held_reason or reason without
// guessing; a code not in the list is a newer server's.
var firmwareReasonGuide = func() string {
	parts := make([]string, 0, len(models.FirmwareReasons))
	for _, code := range models.FirmwareReasons {
		parts = append(parts, fmt.Sprintf("%s (%s)", code, models.FirmwareReasonText(code)))
	}
	return strings.Join(parts, "; ")
}()

// The notes tokens of a major upgrade's answer, a device row's blocked_by and
// boot_environment.last, and the board's states and blocked_reason, each with
// its meaning, for the descriptions of the tools that return them.
var (
	firmwareNoteGuide = func() string {
		parts := make([]string, 0, len(models.FirmwareNotes))
		for _, token := range models.FirmwareNotes {
			parts = append(parts, fmt.Sprintf("%s (%s)", token, models.FirmwareNoteText(token)))
		}
		return strings.Join(parts, "; ")
	}()
	// The holds a device can be blocked by are hold reasons, explained once in the
	// reason guide beside it, so they are named here and not explained again.
	firmwareDeviceMajorGuide = func() string {
		parts := make([]string, 0, len(models.FirmwareDeviceMajorBlocked))
		for _, code := range models.FirmwareDeviceMajorBlocked {
			if strings.HasPrefix(code, "held_") {
				parts = append(parts, code)
			} else {
				parts = append(parts, fmt.Sprintf("%s (%s)", code, models.FirmwareDeviceMajorBlockedText(code)))
			}
		}
		return strings.Join(parts, "; ")
	}()
	firmwareBootEnvironmentGuide = func() string {
		parts := make([]string, 0, len(models.FirmwareBootEnvironmentLasts))
		for _, last := range models.FirmwareBootEnvironmentLasts {
			parts = append(parts, fmt.Sprintf("%s (%s)", last, models.FirmwareBootEnvironmentLastText(last)))
		}
		return strings.Join(parts, "; ")
	}()
	firmwareMajorStateGuide = func() string {
		parts := make([]string, 0, len(models.FirmwareMajorStates))
		for _, state := range models.FirmwareMajorStates {
			parts = append(parts, fmt.Sprintf("%s (%s)", state, models.FirmwareMajorStateText(state)))
		}
		return strings.Join(parts, "; ")
	}()
	firmwareBlockedReasonGuide = func() string {
		parts := make([]string, 0, len(models.FirmwareMajorBlockedReasons))
		for _, code := range models.FirmwareMajorBlockedReasons {
			parts = append(parts, fmt.Sprintf("%s (%s)", code, models.FirmwareMajorBlockedReasonText(code)))
		}
		return strings.Join(parts, "; ")
	}()
)

// firmwareMajorWhat says once what a major upgrade is, for the descriptions.
const firmwareMajorWhat = "A major upgrade moves a device to the next OPNsense series (for example 26.1 to 26.7): it takes the device " +
	"offline for the length of the upgrade, reboots it three times and has no automatic rollback."

// firmwareKindProperty is the kind argument of the tools that address a release
// or a major upgrade.
func firmwareKindProperty() map[string]interface{} {
	return stringEnumProperty("minor (default): version names a release such as 26.7.5. major: version names the target "+
		"series of a major upgrade, such as 26.7 (a series is never a release: 26.7 is also the name of the GA release)",
		models.FirmwareKinds)
}

const firmwareOutcomeGuide = "Outcomes: success, noop (nothing to do), held (the agent declined, see the reason), " +
	"released (the agent gave the device back to unmanaged), failed (the step ended FAILED), " +
	"failed_no_response (the step ended FAILED without any answer from the device)."

// What a plan without firmware tracks does to these tools is decided by the
// control plane; the descriptions only word it. With no firmware configuration
// every ndcli.firmware.* call is refused with FEATURE_NOT_AVAILABLE, reads
// included and before FIRMWARE_NOT_CONFIGURED. With one (the organization lost
// the feature after setting it up) the reads keep answering and four writes
// stay open so it can wind down: detaching a track's schedule, moving devices
// to unmanaged, disabling the configuration and deleting it. Every other write
// is refused.
const (
	firmwareNeedsPlan = "Needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise)."
	firmwareWindDown  = "works on a plan without firmware tracks while the organization has a firmware configuration, " +
		"so the configuration can be wound down."
	firmwarePlanOff  = "no new steps are sent; devices still in a track are being released"
	firmwarePlanRule = "On a plan without firmware tracks (ndcli.org.quota shows firmware_tracks_enabled) every ndcli.firmware.* " +
		"tool answers FEATURE_NOT_AVAILABLE, reads included and before FIRMWARE_NOT_CONFIGURED. The exception is an " +
		"organization that already has a firmware configuration (it lost the feature after setting it up): " + firmwarePlanOff +
		". Its reads keep answering, so that wind-down stays visible, and four changes still work: detaching a track's " +
		"schedule (ndcli.firmware.track_schedule with no_schedule=true), moving devices to unmanaged " +
		"(ndcli.firmware.device_set with track=unmanaged), ndcli.firmware.config_disable and ndcli.firmware.config_delete. " +
		"Every other change is refused with FEATURE_NOT_AVAILABLE."
)

// ── inputs ───────────────────────────────────────────────────────────────────

type firmwareOrgInput struct {
	Organization string `json:"organization,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type firmwareConfigCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Reboot       *bool  `json:"reboot,omitempty"`
}

type firmwareConfigUpdateInput struct {
	Organization string `json:"organization,omitempty"`
	Reboot       *bool  `json:"reboot,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type firmwareTrackShowInput struct {
	Organization string `json:"organization,omitempty"`
	Track        string `json:"track,omitempty"`
}

type firmwareTrackSetInput struct {
	Organization   string `json:"organization,omitempty"`
	Track          string `json:"track"`
	SoakDays       *int   `json:"soak_days,omitempty"`
	MinSuccess     *int   `json:"min_success,omitempty"`
	AutoApplyMajor *bool  `json:"auto_apply_major,omitempty"`
	Confirm        bool   `json:"confirm,omitempty"`
}

type firmwareTrackScheduleInput struct {
	Organization string `json:"organization,omitempty"`
	Track        string `json:"track"`
	Schedule     string `json:"schedule,omitempty"`
	NoSchedule   bool   `json:"no_schedule,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type firmwareDeviceSetInput struct {
	Organization    string     `json:"organization,omitempty"`
	Devices         stringList `json:"devices"`
	Track           string     `json:"track,omitempty"`
	BootEnvironment *bool      `json:"boot_environment,omitempty"`
	Confirm         bool       `json:"confirm,omitempty"`
}

type firmwareStatusInput struct {
	Organization string `json:"organization,omitempty"`
	Track        string `json:"track,omitempty"`
	State        string `json:"state,omitempty"`
	Name         string `json:"name,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type firmwareHistoryInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type firmwareHaltInput struct {
	Organization string `json:"organization,omitempty"`
	Version      string `json:"version"`
	Kind         string `json:"kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type firmwareVersionTrackInput struct {
	Organization string `json:"organization,omitempty"`
	Version      string `json:"version"`
	Kind         string `json:"kind,omitempty"`
	Track        string `json:"track,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type firmwareHaltsInput struct {
	Organization string `json:"organization,omitempty"`
	All          bool   `json:"all,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

// firmwareHandler is the opening every firmware tool shares: authenticated,
// arguments decoded into In, organization resolved. run does the rest.
func firmwareHandler[In any](
	s *Server,
	orgOf func(*In) string,
	run func(ctx context.Context, in *In, org string) (*mcp.CallToolResult, error),
) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := s.svc.RequireAuth(); err != nil {
			return s.errorResult(err)
		}
		argsJSON, _ := json.Marshal(req.Params.Arguments)
		in, err := parseInput[In](argsJSON)
		if err != nil {
			return s.errorResult(err)
		}
		org, err := s.svc.ResolveOrg(orgOf(in))
		if err != nil {
			return s.errorResult(err)
		}
		return run(ctx, in, org)
	}
}

func firmwareIntProperty(description string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": description, "minimum": 0}
}

func firmwareOrgProps(extra map[string]interface{}) map[string]interface{} {
	props := map[string]interface{}{"organization": organizationProperty()}
	for k, v := range extra {
		props[k] = v
	}
	return props
}

// registerFirmwareTools registers the `ndcli firmware` tools.
func (s *Server) registerFirmwareTools() {
	add := func(name, description string, props map[string]interface{}, required []string, handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
		schema := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		s.mcpServer.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: schema}, handler)
	}
	orgOnly := func(in *firmwareOrgInput) string { return in.Organization }
	pageProps := map[string]interface{}{
		"page":     intProperty("Page number", 1),
		"per_page": intProperty("Items per page (max 200)", 50),
	}

	// ── configuration ──
	add("ndcli.firmware.config_show",
		"Show the organization's firmware configuration (staged OPNsense updates): status (ENABLED or DISABLED), "+
			"reboot (the default of every update step: true applies the full update, base and kernel included, and reboots; "+
			"false applies the userland packages only; a major upgrade needs it true), plan_enabled (whether the plan includes "+
			"firmware tracks; if false, "+
			firmwarePlanOff+") and the three "+
			"tracks (fast, regular, stable) with soak_days, min_success, the attached schedule, the next window, "+
			"auto_apply_major (whether the track's windows apply major upgrades; off by default; absent from an NDManager "+
			"that predates them) and device counts by state. On a plan that includes firmware tracks it returns "+
			"FIRMWARE_NOT_CONFIGURED until ndcli.firmware.config_create has run. "+firmwarePlanRule,
		firmwareOrgProps(nil), nil,
		firmwareHandler(s, orgOnly, s.firmwareConfigShow))

	add("ndcli.firmware.config_create",
		"Enable firmware tracks for the organization: creates the configuration with the three tracks (fast, regular, "+
			"stable) at their default settings. No device is in a track and no track has a schedule yet. reboot (default true) "+
			"is the default of every update step: true applies the full update and reboots; false applies the userland "+
			"packages only. "+firmwareNeedsPlan,
		firmwareOrgProps(map[string]interface{}{
			"reboot": boolProperty("Default reboot flag of the update steps (default true). false applies the userland packages only."),
		}), nil,
		firmwareHandler(s, func(in *firmwareConfigCreateInput) string { return in.Organization }, s.firmwareConfigCreate))

	add("ndcli.firmware.config_update",
		"Change the default reboot flag of the firmware update steps: true applies the full update, base and kernel "+
			"included, and reboots; false applies the userland packages only. "+firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"reboot":  boolProperty("New default reboot flag (required)"),
			"confirm": confirmProperty(),
		}), []string{"reboot"},
		firmwareHandler(s, func(in *firmwareConfigUpdateInput) string { return in.Organization }, s.firmwareConfigUpdate))

	add("ndcli.firmware.config_enable",
		"Enable the firmware configuration. Enabling does not bring any device back into a track; add them again "+
			"with ndcli.firmware.device_set. "+firmwareNeedsPlan,
		firmwareOrgProps(nil), nil,
		firmwareHandler(s, orgOnly, s.firmwareConfigEnable))

	add("ndcli.firmware.config_disable",
		"Disable the firmware configuration. Every device in a track starts leaving it: its agent restores the firmware "+
			"setting the device had before it joined, and the device becomes unmanaged. Enabling again does not bring them "+
			"back. Still "+firmwareWindDown+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{"confirm": confirmProperty()}), nil,
		firmwareHandler(s, orgOnly, s.firmwareConfigDisable))

	add("ndcli.firmware.config_delete",
		"Delete the firmware configuration with its tracks, their schedules and the approvals; halts and the history stay. "+
			"Refused with HAS_DEPENDENCIES (the devices are listed) while any device is in a track or still leaving one: "+
			"disable firmware or move the devices to unmanaged first. Still "+firmwareWindDown+" Both ways of releasing its "+
			"devices, ndcli.firmware.config_disable and ndcli.firmware.device_set with track=unmanaged, work there too. "+
			"Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{"confirm": confirmProperty()}), nil,
		firmwareHandler(s, orgOnly, s.firmwareConfigDelete))

	// ── tracks ──
	add("ndcli.firmware.track_show",
		"Show one track's soak_days, min_success, schedule, next window, auto_apply_major and device counts; without a track, "+
			"shows all three. soak_days counts from OPNsense publishing a release (fast) or from the release meeting its "+
			"evidence in the previous track (regular, stable); min_success is how many distinct devices of the previous track "+
			"must have reached a release first. auto_apply_major says whether the track's windows move its devices to the next "+
			"OPNsense series; off, the series is approved but no device is asked to upgrade.",
		firmwareOrgProps(map[string]interface{}{
			"track": stringEnumProperty("Track to show (omit for all three)", models.FirmwareTracks),
		}), nil,
		firmwareHandler(s, func(in *firmwareTrackShowInput) string { return in.Organization }, s.firmwareTrackShow))

	add("ndcli.firmware.track_set",
		"Change a track's soak_days and/or min_success (at least one), or its auto_apply_major switch, which is changed on "+
			"its own, in a call of its own. Releases the track already approved stay approved; the new values apply to the "+
			"decisions still to come. The fast track has no previous track and takes min_success 0 only. auto_apply_major=true "+
			"lets the track's windows move its devices to the next OPNsense series once the series is approved for the track "+
			"(off for every track until an operator turns it on). "+firmwareMajorWhat+" Turning it on is the operator's explicit "+
			"decision: the preview carries the consequences (lead and consequences in its data), show them to the operator and "+
			"call again with confirm=true only on their go-ahead. auto_apply_major=false cancels the major-upgrade steps queued "+
			"for the track's devices and not yet dispatched (cancelled_tasks); an upgrade already running goes on. An NDManager "+
			"that predates major upgrades is reported as such (SERVER_TOO_OLD) and nothing changes. "+
			firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"track":       stringEnumProperty("Track to change", models.FirmwareTracks),
			"soak_days":   firmwareIntProperty("Days a release soaks before the track takes it"),
			"min_success": firmwareIntProperty("Distinct devices of the previous track that must have reached a release first"),
			"auto_apply_major": boolProperty("Whether the track's windows move its devices to the next OPNsense series " +
				"(off by default). Give it alone, without soak_days or min_success. true needs the operator's explicit go-ahead"),
			"confirm": confirmProperty(),
		}), []string{"track"},
		firmwareHandler(s, func(in *firmwareTrackSetInput) string { return in.Organization }, s.firmwareTrackSet))

	add("ndcli.firmware.track_schedule",
		"Attach a track's update windows to a named schedule (it must already exist), move them to another, or detach them "+
			"with no_schedule=true. Each time the schedule fires, every managed device of the track gets one update step. "+
			"Tracks may share a schedule. Give exactly one of schedule and no_schedule. A track's schedule entry cannot be "+
			"changed through the ndcli.schedule.tasks tools. Attaching or moving needs a plan that includes firmware tracks "+
			"(FEATURE_NOT_AVAILABLE otherwise); detaching (no_schedule=true) still "+firmwareWindDown+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"track":       stringEnumProperty("Track to attach or detach", models.FirmwareTracks),
			"schedule":    stringProperty("Name of the schedule to run the track's windows on"),
			"no_schedule": boolProperty("Detach the track from its schedule; it stops moving its devices"),
			"confirm":     confirmProperty(),
		}), []string{"track"},
		firmwareHandler(s, func(in *firmwareTrackScheduleInput) string { return in.Organization }, s.firmwareTrackSchedule))

	// ── devices ──
	add("ndcli.firmware.device_set",
		"Move devices, named one by one, into a firmware track or back to unmanaged (no OU, no pattern), and/or set their "+
			"boot_environment setting (give track, boot_environment, or both). It is all or "+
			"nothing for the names. A device not in a track joins as enrolling and its agent pins it at the installed "+
			"release; moving it between tracks needs no step; moving it to unmanaged makes it leave the track and its agent "+
			"restores the setting it had. Moving devices to a track needs a plan that includes firmware tracks "+
			"(FEATURE_NOT_AVAILABLE otherwise) and an enabled configuration; moving them to unmanaged (track=unmanaged) still "+
			firmwareWindDown+" boot_environment is whether the agent makes a ZFS boot environment before a major upgrade of "+
			"the device (default true for a device in a track; ZFS only, best effort, the agent never removes it; a recovery "+
			"path from the console only). It belongs to the device's place in a track: a device in no track has none "+
			"(FIRMWARE_DEVICE_UNMANAGED names them; add them to a track first or give track too), it cannot be set with "+
			"track=unmanaged, changing it cancels the device's queued major-upgrade step that was not yet dispatched "+
			"(cancelled_tasks), and like moving into a track it is refused on a plan without firmware tracks. An NDManager "+
			"that predates the setting is reported as such (SERVER_TOO_OLD). Each result carries boot_environment (the setting "+
			"afterwards) and notes: agent_too_old, agent_upgrade_required or device_disabled mean no step is sent until that changes. "+
			"Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"devices": stringArrayProperty("Device names (required)"),
			"track":   stringEnumProperty("Track to move the devices to, or unmanaged to release them (omit to leave their track alone)", models.FirmwareAssignTracks),
			"boot_environment": boolProperty("Whether the agent makes a ZFS boot environment before a major upgrade of the devices " +
				"(omit to leave it alone)"),
			"confirm": confirmProperty(),
		}), []string{"devices"},
		firmwareHandler(s, func(in *firmwareDeviceSetInput) string { return in.Organization }, s.firmwareDeviceSet))

	add("ndcli.firmware.status",
		"List the organization's devices and where each stands in the firmware tracks: track and state (enrolling, "+
			firmwareDeviceRevisionGuide+" "+
			"managed, held, unenrolling; both absent when unmanaged), pinned_version, held_reason (the reason of the agent's "+
			"last hold), last_status and last_run_at, last_error, drift (the pin had been changed outside NetDefense), "+
			"blocked (agent_too_old, agent_upgrade_required or device_disabled: why no step is sent; the last means the release "+"folder changed after approval and the agent is below 1.28.0) and released ({at, reason} on an unmanaged "+
			"device the control plane released by itself). For a device with a pin it also returns series (the OPNsense series "+
			"of the pinned release); for a device in a track, managed and enabled, major {eligible, blocked_by}: whether the "+
			"next window would send it a major upgrade as far as the device itself goes, and when not the first thing that "+
			"stops it (the track's own gates are the blocked_reason of ndcli.firmware.rollout's majors). blocked_by: "+
			firmwareDeviceMajorGuide+". And, for a device in a track, boot_environment {setting, last}: the device's setting "+
			"for making a ZFS boot environment before a major upgrade, and what the newest major upgrade answer did about it ("+
			firmwareBootEnvironmentGuide+"; last is absent when that answer made no decision), and notes, the tokens of that "+
			"answer, kept past the next ordinary window; a third-party repository or package is a warning, never a hold, and "+
			"never named: "+firmwareNoteGuide+". These fields are absent from an NDManager that predates major upgrades. "+
			firmwareOutcomeGuide+" Reason codes: "+firmwareReasonGuide+".",
		firmwareOrgProps(mergeProps(map[string]interface{}{
			"track": stringEnumProperty("Only devices in this track, or unmanaged ones", models.FirmwareAssignTracks),
			"state": stringEnumProperty("Only devices in this state", models.FirmwareStates),
			"name":  stringProperty("Only devices whose name contains this text"),
		}, pageProps)), nil,
		firmwareHandler(s, func(in *firmwareStatusInput) string { return in.Organization }, s.firmwareStatus))

	add("ndcli.firmware.history",
		"Show a device's firmware ledger, newest first: every finished step with its action (enroll, advance, unenroll), "+
			"outcome, reason, target, the releases it moved between (from_version, to_version) and whether it applied "+
			"anything. kind is minor (an ordinary update) or major (a major upgrade; target is then the series); it is absent "+
			"from an NDManager that predates major upgrades. A failed step an operator cleared carries cleared_at and "+
			"cleared_by. revision (when present) is the revision of the release the step's answer reported. "+firmwareOutcomeGuide+" The reason codes are the ones ndcli.firmware.status lists.",
		firmwareOrgProps(mergeProps(map[string]interface{}{
			"device": stringProperty("Device name (required)"),
		}, pageProps)), []string{"device"},
		firmwareHandler(s, func(in *firmwareHistoryInput) string { return in.Organization }, s.firmwareHistory))

	// ── rollout ──
	add("ndcli.firmware.rollout",
		"Show where every release stands on its way down the tracks, per track and OPNsense series: the target (the "+
			"release the track moves its devices of that series to, with target_source auto or manual) and the "+
			"next_candidate with its state (ready, soaking, waiting_for_previous_track, waiting_for_evidence, "+
			"blocked_by_failure), soak_ends_at and soak_remaining_seconds (computed by the server), evidence {reached, "+
			"required} (absent for fast), and stalled with stall_reason (evidence_unreachable: too few devices in the "+
			"previous track can still reach it; blocked_by_failure). Also lists the failures that block a release and "+
			"the halts in force; these lists hold releases only. "+firmwareBoardRevisionGuide+" plan_enabled is whether the plan includes firmware tracks; "+
			"if false, "+firmwarePlanOff+". "+
			"Major upgrades are beside them: majors, one entry per track (in conveyor order) and series with source_series, "+
			"state ("+firmwareMajorStateGuide+"), the soak and evidence like a release's, stalled and stall_reason, "+
			"auto_apply_major (the track's own switch: off is approved but not applied), the device counts devices_eligible, "+
			"devices_on_series, devices_hopped, devices_awaiting_minor_evidence and offer_not_seen (devices whose mirror does "+
			"not offer the upgrade yet), blocked_reason (why no upgrade starts from the track although the operator wants "+
			"one; absent while nothing blocks: "+firmwareBlockedReasonGuide+"), upgrade_open_at, approved_at and "+
			"approval_source (auto or manual), source_series_end_of_life (OPNsense gives the old series no more updates while "+
			"the path is open) and notice (payload_changed: the upgrade payload was replaced upstream after the track "+
			"approved it); major_failures, the unresolved failures of major upgrades {kind, series, track, devices, count, "+
			"last_at}, which block the series in the later tracks only (clear them with ndcli.firmware.failure_clear, "+
			"kind=major); and agent_repo {configured, channel, trees: [{abi, ok, checked_at}]}, the control plane's check of "+
			"NetDefense's own package repository for the FreeBSD versions an upgrade crosses (a missing tree fails every "+
			"OPNsense check of an upgraded device; configured=false means the check is not running; trees is empty when "+
			"nothing was checked lately). majors, major_failures and agent_repo are absent from an NDManager that predates "+
			"major upgrades.",
		firmwareOrgProps(nil), nil,
		firmwareHandler(s, orgOnly, s.firmwareRollout))

	add("ndcli.firmware.version_halts",
		"List the halts on releases and major upgrades, newest first. By default only the halts in force; all=true adds "+
			"the ones that have been lifted (unhalted_at and unhalted_by set). By default only the halts of releases are "+
			"listed, each with its version; kind=major lists those of major upgrades, each with its series and no version, "+
			"and kind=all both, told apart by each row's kind. revision (when present) is the one revision halted; absent "+
			"means every revision of the release.",
		firmwareOrgProps(mergeProps(map[string]interface{}{
			"all":  boolProperty("Also list the halts that have been lifted"),
			"kind": stringEnumProperty("minor (default): halts of releases. major: halts of major upgrades. all: both", models.FirmwareHaltKinds),
		}, pageProps)), nil,
		firmwareHandler(s, func(in *firmwareHaltsInput) string { return in.Organization }, s.firmwareHalts))

	add("ndcli.firmware.version_halt",
		"Halt a release (for example 26.7.5): no track approves it, and the steps queued for it that have not been "+
			"dispatched yet are cancelled. Devices already on it stay on it; there is no rollback. The release must be one "+
			"the control plane knows from OPNsense's mirror. With kind=major, version is the target series of a major "+
			"upgrade (for example 26.7): no track approves the upgrade and the upgrade steps queued for it that were not "+
			"yet dispatched are cancelled; devices already upgrading are left alone, and the series must be the target of a "+
			"supported upgrade. reason is plain text, one line. Lift it with ndcli.firmware.version_unhalt (same kind). "+
			firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"version":  stringProperty("Release directory name, such as 26.7.5; with kind=major the target series, such as 26.7 (required)"),
			"kind":     firmwareKindProperty(),
			"reason":   stringProperty("Why the release or major upgrade is halted: plain text, one line (required)"),
			"revision": firmwareRevisionProperty("Halt only this revision of the release; omit to halt every revision"),
			"confirm":  confirmProperty(),
		}), []string{"version", "reason"},
		firmwareHandler(s, func(in *firmwareHaltInput) string { return in.Organization }, s.firmwareVersionHalt))

	add("ndcli.firmware.version_unhalt",
		"Lift the halt on a release, or with kind=major on a major upgrade (version is its target series, such as 26.7); "+
			"it competes for the tracks again. Lifting the halt on a major upgrade lets a track with auto_apply_major on that "+
			"has approved the series upgrade every device that passes the gates at its next window. "+firmwareMajorWhat+" "+
			firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"version":  stringProperty("Release directory name, such as 26.7.5; with kind=major the target series, such as 26.7 (required)"),
			"kind":     firmwareKindProperty(),
			"revision": firmwareRevisionProperty("Lift the halt on this revision only; omit to lift the whole-release halt"),
			"confirm":  confirmProperty(),
		}), []string{"version"},
		firmwareHandler(s, func(in *firmwareVersionTrackInput) string { return in.Organization }, s.firmwareVersionUnhalt))

	add("ndcli.firmware.version_promote",
		"Approve a release in one track, or in all three at once (track=all), without waiting for its soak: the way an "+
			"urgent security release goes out. Every device of a track that approved it installs it at the track's next "+
			"window. Idempotent: results say approved or already_approved per track. The release must be known to the "+
			"control plane and not halted, and the configuration must be enabled. With kind=major, version is the target "+
			"series of a major upgrade (for example 26.7): approving it starts no upgrade by itself, but a track with "+
			"auto_apply_major on then upgrades every device that passes the gates at its next window. "+firmwareMajorWhat+
			" The series must be the target of a supported upgrade, not halted, and OPNsense's upgrade path for it must be "+
			"open (FIRMWARE_UPGRADE_NOT_OPEN otherwise). "+firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"version":  stringProperty("Release directory name, such as 26.7.5; with kind=major the target series, such as 26.7 (required)"),
			"kind":     firmwareKindProperty(),
			"track":    stringEnumProperty("Track to approve the release in, or all", models.FirmwarePromoteTracks),
			"revision": firmwareRevisionProperty("Approve this revision of the release; omit for the newest recorded one"),
			"confirm":  confirmProperty(),
		}), []string{"version", "track"},
		firmwareHandler(s, func(in *firmwareVersionTrackInput) string { return in.Organization }, s.firmwareVersionPromote))

	add("ndcli.firmware.failure_clear",
		"Clear the recorded failures that block a release from moving past a track, in one track or (track omitted) in "+
			"every track, so it can move on. The records stay in the history. With kind=major, version is the target series "+
			"of a major upgrade (for example 26.7): a failed upgrade, or one that never answered, blocks the series in the "+
			"later tracks only; after a manual rollback of a device, clear the failures it caused once the device is rebound. "+
			firmwareNeedsPlan+" Requires confirm=true.",
		firmwareOrgProps(map[string]interface{}{
			"version":  stringProperty("Release whose recorded failures are cleared; with kind=major the target series, such as 26.7 (required)"),
			"kind":     firmwareKindProperty(),
			"track":    stringEnumProperty("Limit to one track (omit for every track)", models.FirmwareTracks),
			"revision": firmwareRevisionProperty("Clear the failures of this revision only; omit for every revision"),
			"confirm":  confirmProperty(),
		}), []string{"version"},
		firmwareHandler(s, func(in *firmwareVersionTrackInput) string { return in.Organization }, s.firmwareFailureClear))
}

// ── configuration handlers ───────────────────────────────────────────────────

func (s *Server) firmwareConfigShow(ctx context.Context, in *firmwareOrgInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	cfg, err := s.svc.FirmwareConfigGet(apiCtx, org)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"config": cfg}, "")
}

func (s *Server) firmwareConfigCreate(ctx context.Context, in *firmwareConfigCreateInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	cfg, err := s.svc.FirmwareConfigCreate(apiCtx, org, service.FirmwareConfigCreateOpts{
		NoReboot: in.Reboot != nil && !*in.Reboot,
	})
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"config": cfg}, cfg.Message)
}

func (s *Server) firmwareConfigUpdate(ctx context.Context, in *firmwareConfigUpdateInput, org string) (*mcp.CallToolResult, error) {
	if in.Reboot == nil {
		return s.errorResult(&service.Error{Code: service.CodeInvalidInput, Message: "reboot is required"})
	}
	if !in.Confirm {
		return s.previewResultWithData("change the default reboot flag of firmware update steps", org,
			map[string]interface{}{"organization": org, "reboot": *in.Reboot})
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	cfg, err := s.svc.FirmwareConfigUpdate(apiCtx, org, *in.Reboot)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"config": cfg}, cfg.Message)
}

func (s *Server) firmwareConfigEnable(ctx context.Context, in *firmwareOrgInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	cfg, err := s.svc.FirmwareConfigSetStatus(apiCtx, org, true)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"config": cfg}, cfg.Message)
}

func (s *Server) firmwareConfigDisable(ctx context.Context, in *firmwareOrgInput, org string) (*mcp.CallToolResult, error) {
	if !in.Confirm {
		return s.previewResultWithData("disable firmware tracks (every device in a track is released)", org,
			map[string]interface{}{"organization": org})
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	cfg, err := s.svc.FirmwareConfigSetStatus(apiCtx, org, false)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"config": cfg}, cfg.Message)
}

func (s *Server) firmwareConfigDelete(ctx context.Context, in *firmwareOrgInput, org string) (*mcp.CallToolResult, error) {
	if !in.Confirm {
		return s.previewResultWithData("delete the firmware configuration (tracks, schedules and approvals)", org,
			map[string]interface{}{"organization": org})
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwareConfigDelete(apiCtx, org)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"deleted": true, "organization": result.Organization}, result.Message)
}

// ── track handlers ───────────────────────────────────────────────────────────

func (s *Server) firmwareTrackShow(ctx context.Context, in *firmwareTrackShowInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	if strings.TrimSpace(in.Track) == "" {
		tracks, err := s.svc.FirmwareTrackList(apiCtx, org)
		if err != nil {
			return s.firmwareError(err)
		}
		return s.successResult(map[string]interface{}{"tracks": tracks}, "")
	}
	track, err := s.svc.FirmwareTrackGet(apiCtx, org, in.Track)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(map[string]interface{}{"track": track}, "")
}

func (s *Server) firmwareTrackSet(ctx context.Context, in *firmwareTrackSetInput, org string) (*mcp.CallToolResult, error) {
	opts := service.FirmwareTrackUpdateOpts{SoakDays: in.SoakDays, MinSuccess: in.MinSuccess, AutoApplyMajor: in.AutoApplyMajor}
	track, _, err := service.CheckFirmwareTrackUpdate(in.Track, opts)
	if err != nil {
		return s.errorResult(err)
	}
	if !in.Confirm {
		extra := map[string]interface{}{"organization": org}
		var changes []string
		if in.SoakDays != nil {
			extra["soak_days"] = *in.SoakDays
			changes = append(changes, fmt.Sprintf("soak_days=%d", *in.SoakDays))
		}
		if in.MinSuccess != nil {
			extra["min_success"] = *in.MinSuccess
			changes = append(changes, fmt.Sprintf("min_success=%d", *in.MinSuccess))
		}
		if in.AutoApplyMajor == nil {
			return s.previewResultWithData("change firmware track settings",
				fmt.Sprintf("%s (%s)", track, strings.Join(changes, ", ")), extra)
		}
		extra["track"] = track
		extra["auto_apply_major"] = *in.AutoApplyMajor
		if !*in.AutoApplyMajor {
			return s.previewResultWithNote("turn major upgrades off for firmware track", track,
				"Major-upgrade steps queued for the track's devices and not yet dispatched are cancelled; an upgrade already running goes on.", extra)
		}
		// Turning it on is the operator's explicit decision: the preview carries
		// what it means, as text for the model to show and as data.
		extra["lead"] = models.FirmwareMajorSwitchLead
		extra["consequences"] = models.FirmwareMajorConsequences
		return s.previewResultWithNote("turn major upgrades on for firmware track", track,
			"Show the operator these consequences and ask for their go-ahead: "+models.FirmwareMajorSwitchLead+" "+
				strings.Join(models.FirmwareMajorConsequences, " "), extra)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	updated, err := s.svc.FirmwareTrackUpdate(apiCtx, org, track, opts)
	if err != nil {
		return s.firmwareError(err)
	}
	message := fmt.Sprintf("Firmware track %s updated", updated.Track)
	switch {
	case in.AutoApplyMajor != nil && *in.AutoApplyMajor:
		message = fmt.Sprintf("Firmware track %s now applies major upgrades", updated.Track)
	case in.AutoApplyMajor != nil && updated.CancelledTasks != nil:
		message = fmt.Sprintf("Firmware track %s no longer applies major upgrades (%d queued upgrade step(s) cancelled)",
			updated.Track, *updated.CancelledTasks)
	case in.AutoApplyMajor != nil:
		message = fmt.Sprintf("Firmware track %s no longer applies major upgrades", updated.Track)
	}
	return s.successResult(map[string]interface{}{"track": updated}, message)
}

func (s *Server) firmwareTrackSchedule(ctx context.Context, in *firmwareTrackScheduleInput, org string) (*mcp.CallToolResult, error) {
	track, err := service.NormalizeFirmwareChoice("track", in.Track, models.FirmwareTracks)
	if err != nil {
		return s.errorResult(err)
	}
	schedule := strings.TrimSpace(in.Schedule)
	switch {
	case in.NoSchedule && schedule != "":
		return s.errorResult(&service.Error{Code: service.CodeInvalidInput, Message: "give schedule or no_schedule, not both"})
	case !in.NoSchedule && schedule == "":
		return s.errorResult(&service.Error{Code: service.CodeInvalidInput, Message: "give the schedule to attach, or no_schedule=true to detach"})
	}
	if !in.Confirm {
		target := fmt.Sprintf("%s → %s", track, schedule)
		extra := map[string]interface{}{"organization": org, "track": track, "schedule": schedule}
		if in.NoSchedule {
			target = fmt.Sprintf("%s → (detached)", track)
			extra = map[string]interface{}{"organization": org, "track": track, "no_schedule": true}
		}
		return s.previewResultWithData("change the schedule of firmware track", target, extra)
	}
	var attach *string
	if !in.NoSchedule {
		attach = &schedule
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	updated, err := s.svc.FirmwareTrackSetSchedule(apiCtx, org, track, attach)
	if err != nil {
		return s.firmwareError(err)
	}
	message := fmt.Sprintf("Firmware track %s now runs its windows on schedule %q", updated.Track, updated.Schedule)
	if in.NoSchedule {
		message = fmt.Sprintf("Firmware track %s detached from its schedule", updated.Track)
	}
	return s.successResult(map[string]interface{}{"track": updated}, message)
}

// ── device handlers ──────────────────────────────────────────────────────────

func (s *Server) firmwareDeviceSet(ctx context.Context, in *firmwareDeviceSetInput, org string) (*mcp.CallToolResult, error) {
	opts := service.FirmwareDeviceAssignOpts{Track: in.Track, BootEnvironment: in.BootEnvironment}
	names, track, err := service.CheckFirmwareDeviceAssign(in.Devices, opts)
	if err != nil {
		return s.errorResult(err)
	}
	opts.Track = track
	if !in.Confirm {
		extra := map[string]interface{}{"organization": org, "devices": names}
		var changes []string
		if track != "" {
			extra["track"] = track
			changes = append(changes, "→ "+track)
		}
		if in.BootEnvironment != nil {
			extra["boot_environment"] = *in.BootEnvironment
			changes = append(changes, fmt.Sprintf("boot_environment=%t", *in.BootEnvironment))
		}
		return s.previewResultWithData("change firmware devices",
			fmt.Sprintf("%s %s", strings.Join(names, ", "), strings.Join(changes, ", ")), extra)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwareDeviceAssign(apiCtx, org, names, opts)
	if err != nil {
		return s.firmwareError(err)
	}
	message := fmt.Sprintf("%d device(s)", len(result.Results))
	switch {
	case track != "" && in.BootEnvironment != nil:
		message += fmt.Sprintf(" → %s, boot_environment=%t", track, *in.BootEnvironment)
	case track != "":
		message += " → " + track
	case in.BootEnvironment != nil:
		message += fmt.Sprintf(": boot_environment=%t", *in.BootEnvironment)
	}
	return s.successResult(result, message)
}

func (s *Server) firmwareStatus(ctx context.Context, in *firmwareStatusInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	list, err := s.svc.FirmwareDeviceList(apiCtx, org, service.FirmwareDeviceListOpts{
		Track: in.Track, State: in.State, Name: in.Name, Page: in.Page, PerPage: in.PerPage,
	})
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResultWithPagination(map[string]interface{}{"devices": list.Items}, list.Page, list.PerPage, list.Total)
}

func (s *Server) firmwareHistory(ctx context.Context, in *firmwareHistoryInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	history, err := s.svc.FirmwareHistory(apiCtx, org, in.Device, in.Page, in.PerPage)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResultWithPagination(map[string]interface{}{
		"device":  history.DeviceName,
		"history": history.Items,
	}, history.Page, history.PerPage, history.Total)
}

// ── rollout handlers ─────────────────────────────────────────────────────────

func (s *Server) firmwareRollout(ctx context.Context, in *firmwareOrgInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	rollout, err := s.svc.FirmwareRollout(apiCtx, org)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(rollout, "")
}

func (s *Server) firmwareHalts(ctx context.Context, in *firmwareHaltsInput, org string) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	list, err := s.svc.FirmwareHaltList(apiCtx, org, service.FirmwareHaltListOpts{
		IncludeLifted: in.All, Kind: in.Kind, Page: in.Page, PerPage: in.PerPage,
	})
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResultWithPagination(map[string]interface{}{"halts": list.Items}, list.Page, list.PerPage, list.Total)
}

// firmwareDeviceRevisionGuide and firmwareBoardRevisionGuide describe the
// release-revision fields of the read tools; they are null or absent while the
// control plane's revision switch is off.
const (
	firmwareDeviceRevisionGuide = "Release revisions (absent while the control plane does not track them): revision is the revision " +
		"of the pinned release the device last proved; pending {count, kind, checked_at} is the package updates its last " +
		"completed check found, kind hotfix when installing them would not change the OPNsense version (present it as " +
		"\"Hotfix: N package updates pending\"), update otherwise; hotfix_warning {release, revision, detected_at, " +
		"approved_in_track} says the device is pinned below a release whose newest revision is a hotfix, and when " +
		"approved_in_track is false, version_promote moves it up."
	firmwareBoardRevisionGuide = "Release revisions (absent while the control plane does not track them): each series has " +
		"target_revision and target_fingerprint, and latest_release {version, latest_revision, revised_at}; next_candidate " +
		"has revision and fingerprint; failures and halts carry revision."
)

// firmwareRevisionProperty is the optional revision of a release. The server
// refuses it (422 on field revision) while revisions are not in force.
func firmwareRevisionProperty(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "integer",
		"minimum":     1,
		"description": description + ". A release's revision counts the times its folder changed after it first appeared; refused with 422 while revisions are off.",
	}
}

// withFirmwareRevision adds the revision to the data of a preview when named.
func withFirmwareRevision(extra map[string]interface{}, revision int) map[string]interface{} {
	if revision > 0 {
		extra["revision"] = revision
	}
	return extra
}

// firmwareThing names what a release-or-series tool addresses in a sentence:
// "release 26.7.5", or "major upgrade to 26.7".
func firmwareThing(kind, name string) string {
	if kind == models.FirmwareKindMajor {
		return "major upgrade to " + name
	}
	return "release " + name
}

func (s *Server) firmwareVersionHalt(ctx context.Context, in *firmwareHaltInput, org string) (*mcp.CallToolResult, error) {
	kind, name, err := service.CheckFirmwareTarget(in.Kind, in.Version)
	if err != nil {
		return s.errorResult(err)
	}
	if err := service.CheckFirmwareRevision(kind, name, in.Revision); err != nil {
		return s.errorResult(err)
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return s.errorResult(&service.Error{Code: service.CodeInvalidInput, Message: "reason is required"})
	}
	if !in.Confirm {
		action := "halt firmware release"
		extra := map[string]interface{}{"organization": org, "version": name, "reason": reason}
		if kind == models.FirmwareKindMajor {
			action = "halt firmware major upgrade to series"
			extra = map[string]interface{}{"organization": org, "version": name, "kind": kind, "reason": reason}
		}
		return s.previewResultWithData(action, name, withFirmwareRevision(extra, in.Revision))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwareHalt(apiCtx, org, kind, name, reason, in.Revision)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(result, fmt.Sprintf("%s halted (%d pending step(s) cancelled)",
		capitalize(firmwareThing(kind, result.Halt.Name())), result.CancelledTasks))
}

func (s *Server) firmwareVersionUnhalt(ctx context.Context, in *firmwareVersionTrackInput, org string) (*mcp.CallToolResult, error) {
	kind, name, err := service.CheckFirmwareTarget(in.Kind, in.Version)
	if err != nil {
		return s.errorResult(err)
	}
	if err := service.CheckFirmwareRevision(kind, name, in.Revision); err != nil {
		return s.errorResult(err)
	}
	if !in.Confirm {
		if kind == models.FirmwareKindMajor {
			return s.previewResultWithNote("lift the halt on firmware major upgrade to series", name,
				"A track with auto_apply_major on that has approved it will then upgrade its eligible devices at its next window: "+
					"offline for the length of the upgrade, three reboots, no automatic rollback.",
				map[string]interface{}{"organization": org, "version": name, "kind": kind})
		}
		return s.previewResultWithData("lift the halt on firmware release", name,
			withFirmwareRevision(map[string]interface{}{"organization": org, "version": name}, in.Revision))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwareUnhalt(apiCtx, org, kind, name, in.Revision)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(result, fmt.Sprintf("Halt on %s lifted", firmwareThing(kind, result.Halt.Name())))
}

func (s *Server) firmwareVersionPromote(ctx context.Context, in *firmwareVersionTrackInput, org string) (*mcp.CallToolResult, error) {
	kind, name, err := service.CheckFirmwareTarget(in.Kind, in.Version)
	if err != nil {
		return s.errorResult(err)
	}
	if err := service.CheckFirmwareRevision(kind, name, in.Revision); err != nil {
		return s.errorResult(err)
	}
	track, err := service.NormalizeFirmwareChoice("track", in.Track, models.FirmwarePromoteTracks)
	if err != nil {
		return s.errorResult(err)
	}
	if !in.Confirm {
		scope := track
		if track == models.FirmwareAllTracks {
			scope = "all three tracks"
		}
		if kind == models.FirmwareKindMajor {
			return s.previewResultWithNote("promote firmware major upgrade to series without waiting for its soak",
				fmt.Sprintf("%s → %s", name, scope),
				"A track with auto_apply_major on will then upgrade its eligible devices at its next window: "+
					"offline for the length of the upgrade, three reboots, no automatic rollback.",
				map[string]interface{}{"organization": org, "version": name, "kind": kind, "track": track})
		}
		return s.previewResultWithData("promote firmware release without waiting for its soak",
			fmt.Sprintf("%s → %s", name, scope),
			withFirmwareRevision(map[string]interface{}{"organization": org, "version": name, "track": track}, in.Revision))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwarePromote(apiCtx, org, kind, name, track, in.Revision)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(result, fmt.Sprintf("%s promoted", capitalize(firmwareThing(kind, result.Name()))))
}

func (s *Server) firmwareFailureClear(ctx context.Context, in *firmwareVersionTrackInput, org string) (*mcp.CallToolResult, error) {
	kind, name, err := service.CheckFirmwareTarget(in.Kind, in.Version)
	if err != nil {
		return s.errorResult(err)
	}
	if err := service.CheckFirmwareRevision(kind, name, in.Revision); err != nil {
		return s.errorResult(err)
	}
	track := strings.TrimSpace(in.Track)
	if track != "" {
		normalized, err := service.NormalizeFirmwareChoice("track", track, models.FirmwareTracks)
		if err != nil {
			return s.errorResult(err)
		}
		track = normalized
	}
	if !in.Confirm {
		scope := "every track"
		if track != "" {
			scope = track + " track"
		}
		action := "clear the failures blocking firmware release"
		extra := map[string]interface{}{"organization": org, "version": name, "track": track}
		if kind == models.FirmwareKindMajor {
			action = "clear the failures blocking firmware major upgrade to series"
			extra = map[string]interface{}{"organization": org, "version": name, "kind": kind, "track": track}
		}
		return s.previewResultWithData(action, fmt.Sprintf("%s (%s)", name, scope), withFirmwareRevision(extra, in.Revision))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.FirmwareFailuresClear(apiCtx, org, kind, name, track, in.Revision)
	if err != nil {
		return s.firmwareError(err)
	}
	return s.successResult(result, fmt.Sprintf("Cleared %d failure record(s) for %s", result.Cleared, firmwareThing(kind, result.Name())))
}

// capitalize upper-cases the first letter of a sentence.
func capitalize(sentence string) string {
	if sentence == "" {
		return sentence
	}
	return strings.ToUpper(sentence[:1]) + sentence[1:]
}
