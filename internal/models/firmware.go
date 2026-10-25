package models

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/sanitize"
)

// Firmware tracks: staged OPNsense updates.
//
// These are the response models of NDManager's
// /organizations/{org}/firmware routes. Wire values are lowercase for tracks,
// states, actions and outcomes (fast, managed, success); only the
// configuration status is ENABLED / DISABLED, like the backup configuration it
// is modelled on. Timestamps stay the RFC 3339 strings the server sends, the
// way RunResult keeps its own: the formatters parse them for display and a JSON
// consumer gets back exactly what NDManager sent.
//
// A field the server sends as null is a plain string with omitempty (absent
// means unset), except where null and the zero value mean different things:
// drift, reinstall_entries, soak_remaining_seconds and the nested objects are
// pointers. No model carries an internal id; devices are named and
// identified by uuid.

// The three tracks, in conveyor order: a release enters fast first, then
// regular, then stable.
const (
	FirmwareTrackFast    = "fast"
	FirmwareTrackRegular = "regular"
	FirmwareTrackStable  = "stable"

	// FirmwareUnmanaged is not a track: it is where `device set` sends a
	// device that is to leave its track.
	FirmwareUnmanaged = "unmanaged"
	// FirmwareAllTracks is `version promote`'s shorthand for all three.
	FirmwareAllTracks = "all"
)

// FirmwareTracks, FirmwareAssignTracks and FirmwarePromoteTracks are the
// values the three track-taking inputs accept. They are the single source for
// the --track help, validation and completion and for the MCP enums. Treat
// them as read-only; each is a separate slice so none can alias another.
var (
	FirmwareTracks        = []string{FirmwareTrackFast, FirmwareTrackRegular, FirmwareTrackStable}
	FirmwareAssignTracks  = []string{FirmwareTrackFast, FirmwareTrackRegular, FirmwareTrackStable, FirmwareUnmanaged}
	FirmwarePromoteTracks = []string{FirmwareTrackFast, FirmwareTrackRegular, FirmwareTrackStable, FirmwareAllTracks}
)

// FirmwareStates are the lifecycle states of a device in a track, the values
// `firmware status --state` filters on.
const (
	FirmwareStateEnrolling   = "enrolling"
	FirmwareStateManaged     = "managed"
	FirmwareStateHeld        = "held"
	FirmwareStateUnenrolling = "unenrolling"
)

// FirmwareStates lists them in lifecycle order. Read-only.
var FirmwareStates = []string{
	FirmwareStateEnrolling, FirmwareStateManaged, FirmwareStateHeld, FirmwareStateUnenrolling,
}

// Outcomes of a firmware step, as the device's last status and the ledger
// report them. The first four are what an agent answers; failed and
// failed_no_response are the control plane's.
const (
	FirmwareOutcomeSuccess          = "success"
	FirmwareOutcomeNoop             = "noop"
	FirmwareOutcomeHeld             = "held"
	FirmwareOutcomeReleased         = "released"
	FirmwareOutcomeFailed           = "failed"
	FirmwareOutcomeFailedNoResponse = "failed_no_response"
)

// FirmwareOutcomes lists them. Read-only.
var FirmwareOutcomes = []string{
	FirmwareOutcomeSuccess, FirmwareOutcomeNoop, FirmwareOutcomeHeld, FirmwareOutcomeReleased,
	FirmwareOutcomeFailed, FirmwareOutcomeFailedNoResponse,
}

// The steps a FIRMWARE_TRACK task performs.
const (
	FirmwareActionEnroll   = "enroll"
	FirmwareActionAdvance  = "advance"
	FirmwareActionUnenroll = "unenroll"
)

// FirmwareConfigEnabled and FirmwareConfigDisabled are the configuration
// status values on the wire.
const (
	FirmwareConfigEnabled  = "ENABLED"
	FirmwareConfigDisabled = "DISABLED"
)

// The states of the next release a track will take in a series
// (FirmwareCandidate.State).
const (
	FirmwareCandidateReady                   = "ready"
	FirmwareCandidateSoaking                 = "soaking"
	FirmwareCandidateWaitingForPreviousTrack = "waiting_for_previous_track"
	FirmwareCandidateWaitingForEvidence      = "waiting_for_evidence"
	FirmwareCandidateBlockedByFailure        = "blocked_by_failure"
)

// Why a release cannot reach a track without an operator
// (FirmwareCandidate.StallReason).
const (
	FirmwareStallEvidenceUnreachable = "evidence_unreachable"
	FirmwareStallBlockedByFailure    = "blocked_by_failure"
)

// Why no step is sent to a device (FirmwareDevice.Blocked and the notes of an
// assignment result).
const (
	FirmwareBlockedAgentTooOld    = "agent_too_old"
	FirmwareBlockedDeviceDisabled = "device_disabled"
	// FirmwareBlockedAgentUpgradeRequired: the release folder changed after the
	// track approved it and the agent is below 1.28.0, which cannot prove a revision.
	FirmwareBlockedAgentUpgradeRequired = "agent_upgrade_required"
)

// What a device's pending package updates are (FirmwarePending.Kind).
const (
	FirmwarePendingHotfix = "hotfix"
	FirmwarePendingUpdate = "update"
)

// What moving a device did (FirmwareAssignmentResult.Result). "updated" is the
// membership as it was with the boot-environment setting changed; added, moved
// and released win over it.
const (
	FirmwareAssignmentAdded     = "added"
	FirmwareAssignmentMoved     = "moved"
	FirmwareAssignmentUnchanged = "unchanged"
	FirmwareAssignmentReleased  = "released"
	FirmwareAssignmentUpdated   = "updated"
)

// The two kinds of firmware step. A minor is the ordinary update to a release
// of the device's series; a major is the move to the next series (26.1 to 26.7),
// which a track performs only with its auto_apply_major switch on. The kind is
// on every history row and halt, and it selects what a halt, an unhalt, a
// promote and a failure clear address: a release (version) or a series
// (series). FirmwareKindAll is only a filter, for the list of halts.
const (
	FirmwareKindMinor = "minor"
	FirmwareKindMajor = "major"
	FirmwareKindAll   = "all"
)

// FirmwareKinds and FirmwareHaltKinds are the values the two kind-taking
// inputs accept: halt, unhalt, promote and failure clear take a kind; the list
// of halts also takes all. They are the single source for the --kind help,
// validation and completion and for the MCP enums. Read-only; separate slices.
var (
	FirmwareKinds     = []string{FirmwareKindMinor, FirmwareKindMajor}
	FirmwareHaltKinds = []string{FirmwareKindMinor, FirmwareKindMajor, FirmwareKindAll}
)

// What a promotion did in one track (FirmwarePromotionResult.Result).
const (
	FirmwarePromotionApproved        = "approved"
	FirmwarePromotionAlreadyApproved = "already_approved"
)

// FirmwareReasons is the closed set of reasons a device's firmware state or a
// ledger row can carry, in the order of NDDataModels v1.42.0's
// FIRMWARE_LEDGER_REASONS: what an agent may answer with (FIRMWARE_TRACK_REASONS)
// followed by the control plane's own dead_man. held_test_mode is at index 14;
// the five holds of the major upgrade (held_late_dispatch, held_insufficient_space,
// held_ha_member, held_agent_abi_unavailable, held_upgrade_aborted) follow it,
// then held_revision_mismatch (NDDataModels v1.44.0), before applied. An older server never sends the ones it predates. A reason not
// in the list still renders, as the code itself: the list is what has a sentence
// of its own, not a filter. models.FirmwareReasonText is the one place the
// sentences live.
var FirmwareReasons = []string{
	"at_or_above_target", "target_not_yet_offered", "series_not_approved", "pin_drift",
	"major_offered", "held_check_failed", "held_release_folder_missing", "release_folder_unavailable",
	"held_downgrade_risk", "held_unsupported_edition", "held_custom_flavour", "held_firmware_busy",
	"held_unexpected_plan", "held_reboot_pending", "held_test_mode",
	"held_late_dispatch", "held_insufficient_space", "held_ha_member", "held_agent_abi_unavailable", "held_upgrade_aborted",
	"held_revision_mismatch",
	"applied", "enrolled", "unenrolled",
	"dead_man",
}

var firmwareReasonText = map[string]string{
	"at_or_above_target":          "already at or above the target release",
	"target_not_yet_offered":      "the target release is not on the device's mirror yet",
	"series_not_approved":         "no release is approved for the device's series",
	"pin_drift":                   "the pin was changed outside NetDefense",
	"major_offered":               "a major upgrade is offered; the device stays on its release",
	"held_check_failed":           "the firmware check failed",
	"held_release_folder_missing": "the release folder is missing on the device's mirror",
	"release_folder_unavailable":  "the release folder is gone from the device's mirror",
	"held_downgrade_risk":         "the update would downgrade packages",
	"held_unsupported_edition":    "the device has a subscription or an edition that firmware tracks do not support",
	"held_custom_flavour":         "a custom firmware flavour is already set on the device",
	"held_firmware_busy":          "another firmware operation was running",
	"held_unexpected_plan":        "the update plan was not what the track expected",
	"held_reboot_pending":         "a reboot is pending on the device",
	"held_test_mode":              "the agent is in test mode and does not move firmware",
	"held_late_dispatch":          "the step reached the device with less time left than the update needs",
	"held_insufficient_space":     "the device has too little free disk space for the major upgrade",
	"held_ha_member":              "the device is a member of an HA pair, and a major upgrade is held for one",
	"held_agent_abi_unavailable":  "no usable NetDefense repository for the new FreeBSD version (the url is not ABI-nested, the host is unreachable, or the tree is missing)",
	"held_upgrade_aborted":        "a major upgrade attempt aborted before it changed the device, or the agent is waiting before trying again",
	"held_revision_mismatch":      "the release folder changed again after it was approved; the device holds until the new revision is approved",
	"applied":                     "the update was applied",
	"enrolled":                    "pinned at the installed release",
	"unenrolled":                  "the original firmware setting was restored",
	"dead_man":                    "no contact for 30 days",
}

// FirmwareReasonText is the plain-English sentence for a reason code, or the
// code itself when the code is not one this build knows (a newer server may
// send one): an unknown reason must still show, never turn into a blank.
func FirmwareReasonText(code string) string {
	if text, ok := firmwareReasonText[code]; ok {
		return text
	}
	return code
}

// ── the major upgrade (a "major") ───────────────────────────────────────────
//
// A track moves its devices to the next OPNsense series (26.1 to 26.7) only
// with its auto_apply_major switch on. The closed sets below are the lowercase
// strings NDManager's board and device rows use; each has a sentence of its own
// here, in one place, for the formatters and the MCP descriptions. A string a
// newer server sends that is not in a set still renders, as itself: the sets
// say what has a sentence, they are never a filter and never an error.

// FirmwareNotes is the closed set of tokens an answer to a major upgrade notes
// about the device, in the order of NDDataModels v1.42.0's FIRMWARE_TRACK_NOTES.
// The first four say what the agent did about a ZFS boot environment; the last
// two warn that the device has a repository or a package from outside OPNsense
// and NetDefense. Tokens only: a repository or package is never named on the
// wire.
var FirmwareNotes = []string{
	"boot_environment_made", "boot_environment_skipped_no_room", "boot_environment_off", "boot_environment_unavailable",
	"third_party_repository", "third_party_package",
}

// The two tokens that warn of something from outside OPNsense and NetDefense.
const (
	FirmwareNoteThirdPartyRepository = "third_party_repository"
	FirmwareNoteThirdPartyPackage    = "third_party_package"
)

var firmwareNoteText = map[string]string{
	"boot_environment_made":            "a ZFS boot environment was made before the upgrade",
	"boot_environment_skipped_no_room": "no boot environment was made: the pool had no room for one",
	"boot_environment_off":             "no boot environment was made: the device's setting is off",
	"boot_environment_unavailable":     "no boot environment was made: the device has no ZFS, or making one failed",
	"third_party_repository":           "a package repository from outside OPNsense and NetDefense is enabled on the device",
	"third_party_package":              "a package from outside OPNsense and NetDefense is installed on the device",
}

// FirmwareNoteText is the plain-English sentence for a note token, or the
// token itself when this build does not know it.
func FirmwareNoteText(token string) string {
	if text, ok := firmwareNoteText[token]; ok {
		return text
	}
	return token
}

// FirmwareNoteIsBootEnvironment reports whether a note token is about the boot
// environment. The device rows say that in boot_environment.last, so a view that
// shows both leaves these tokens out of the warnings.
func FirmwareNoteIsBootEnvironment(token string) bool {
	return strings.HasPrefix(token, "boot_environment_")
}

// Approval sources of a major upgrade on the board (FirmwareMajorEntry.ApprovalSource),
// lowercase on the wire like FirmwareSeriesStatus.TargetSource.
const (
	FirmwareApprovalAuto   = "auto"
	FirmwareApprovalManual = "manual"
)

// The states of one major upgrade in one track (FirmwareMajorEntry.State): the
// five candidate states, plus the path being closed, the series being halted
// and the series being approved.
const (
	FirmwareMajorPathClosed = "path_closed"
	FirmwareMajorHalted     = "halted"
	FirmwareMajorApproved   = "approved"
)

// FirmwareMajorStates lists them in the order NDManager documents. Read-only.
var FirmwareMajorStates = []string{
	FirmwareMajorPathClosed, FirmwareCandidateSoaking, FirmwareCandidateReady,
	FirmwareCandidateWaitingForPreviousTrack, FirmwareCandidateWaitingForEvidence,
	FirmwareCandidateBlockedByFailure, FirmwareMajorHalted, FirmwareMajorApproved,
}

var firmwareMajorStateText = map[string]string{
	FirmwareMajorPathClosed:                  "path closed: nothing approves it until OPNsense's upgrade files are seen on the mirror",
	FirmwareCandidateSoaking:                 "soaking: the track is waiting out its soak days",
	FirmwareCandidateReady:                   "ready to be approved",
	FirmwareCandidateWaitingForPreviousTrack: "waiting for the previous track",
	FirmwareCandidateWaitingForEvidence:      "waiting for evidence",
	FirmwareCandidateBlockedByFailure:        "blocked by a failure",
	FirmwareMajorHalted:                      "halted by an operator: no track approves it",
	FirmwareMajorApproved:                    "approved by the track; devices upgrade only while its major upgrades are on",
}

// FirmwareMajorStateText is the plain-English label for a state of a major
// upgrade, or the state itself when this build does not know it.
func FirmwareMajorStateText(state string) string {
	if text, ok := firmwareMajorStateText[state]; ok {
		return text
	}
	return state
}

// FirmwareMajorBlockedReasons are the values of FirmwareMajorEntry.BlockedReason,
// why no upgrade starts from a track although the operator wants one, in the
// order NDManager applies them (the first that holds is the one sent). Read-only.
var FirmwareMajorBlockedReasons = []string{
	"deployment_switch_off", "reboot_disabled", "agents_too_old", "no_supported_hop", "path_closed",
	"final_minor_not_approved", "no_device_at_final_build", "awaiting_minor_evidence",
	"previous_track_empty", "previous_track_applies_no_majors", "previous_track_devices_cannot_hop",
}

var firmwareMajorBlockedReasonText = map[string]string{
	"deployment_switch_off":             "major upgrades are switched off on this deployment",
	"reboot_disabled":                   "the firmware configuration has reboot off, and a major upgrade needs one",
	"agents_too_old":                    "every device of the track runs an agent too old for major upgrades",
	"no_supported_hop":                  "no device of the track is on a series that has a supported upgrade",
	"path_closed":                       "OPNsense's upgrade path is closed (the control plane has not seen its files on the mirror lately)",
	"final_minor_not_approved":          "the track has not approved the final release of the current series yet",
	"no_device_at_final_build":          "no device of the track is on the final release of its series yet",
	"awaiting_minor_evidence":           "devices are waiting for the ordinary update window that records the final release of their series",
	"previous_track_empty":              "the previous track has no device, so no evidence can come from it",
	"previous_track_applies_no_majors":  "the previous track does not apply major upgrades, so no evidence can come from it",
	"previous_track_devices_cannot_hop": "no device of the previous track can upgrade, so no evidence can come from it",
}

// FirmwareMajorBlockedReasonText is the plain-English sentence for a
// blocked_reason, or the code itself when this build does not know it.
func FirmwareMajorBlockedReasonText(code string) string {
	if text, ok := firmwareMajorBlockedReasonText[code]; ok {
		return text
	}
	return code
}

// FirmwareDeviceMajorBlocked are the values of FirmwareDeviceMajor.BlockedBy,
// the first thing that stops the next window sending a device a major
// upgrade, as far as the device itself goes: five gates, then the persistent
// hold of the device's last answer. Read-only.
var FirmwareDeviceMajorBlocked = []string{
	"agent_below_major_floor", "no_supported_hop", "final_minor_not_installed", "awaiting_minor_evidence", "step_in_flight",
	"held_ha_member", "held_agent_abi_unavailable", "held_insufficient_space", "held_upgrade_aborted",
	"held_unsupported_edition", "held_custom_flavour", "held_test_mode",
}

var firmwareDeviceMajorBlockedText = map[string]string{
	"agent_below_major_floor":   "the agent is too old for major upgrades",
	"no_supported_hop":          "no supported major upgrade starts from the device's series",
	"final_minor_not_installed": "the device is not on the final release of its series yet",
	"awaiting_minor_evidence":   "waiting for the ordinary update window that records the final release of its series",
	"step_in_flight":            "a firmware step is already in flight",
}

// FirmwareDeviceMajorBlockedText is the plain-English sentence for a device's
// blocked_by. The persistent holds read as the hold reason they are; a value
// this build does not know is shown as itself.
func FirmwareDeviceMajorBlockedText(code string) string {
	if text, ok := firmwareDeviceMajorBlockedText[code]; ok {
		return text
	}
	return FirmwareReasonText(code)
}

// FirmwareBootEnvironmentLasts are the values of FirmwareBootEnvironment.Last,
// what the newest major-upgrade answer did about the boot environment.
// Read-only.
var FirmwareBootEnvironmentLasts = []string{"made", "skipped_no_room", "off", "unavailable"}

var firmwareBootEnvironmentLastText = map[string]string{
	"made":            "made before the last major upgrade",
	"skipped_no_room": "skipped at the last major upgrade: the pool had no room",
	"off":             "not made at the last major upgrade: the setting was off",
	"unavailable":     "not made at the last major upgrade: no ZFS, or making one failed",
}

// FirmwareBootEnvironmentLastText is the plain-English sentence for a
// boot_environment.last, or the value itself when this build does not know it.
func FirmwareBootEnvironmentLastText(last string) string {
	if text, ok := firmwareBootEnvironmentLastText[last]; ok {
		return text
	}
	return last
}

// FirmwareMajorSwitchLead and FirmwareMajorConsequences are what turning a
// track's auto_apply_major on means for its devices, in the one wording the CLI
// prompt and the MCP preview share. Turning the switch on is the operator's
// explicit decision, so every front-end shows these before it sends anything.
// Read-only; no sentence names a flag or a tool, so each front-end can use them
// as they are.
const FirmwareMajorSwitchLead = "With major upgrades on, the track's update windows move its devices to the next OPNsense series. Each such upgrade:"

var FirmwareMajorConsequences = []string{
	"takes the device offline for the length of the upgrade and reboots it three times;",
	"has no automatic rollback;",
	"leaves a device that loses power during the base or package install in a state that needs the console or a VM snapshot to recover;",
	"needs a link of about 3 Mbit/s or better, so the download finishes inside the step;",
	"needs 3.25 GiB free on the filesystem that holds the update cache;",
	"on ZFS makes a boot environment first when the pool has room and the device's setting allows it; that is a recovery path from the console only, and the agent never removes it;",
	"after a manual rollback, needs the device rebound: stop the track from upgrading it again first (switch major upgrades off, or halt the series), and clear the failures it caused afterwards.",
}

// FirmwareMajorSwitchWarning is the lead and the consequences as one text, a
// line each.
func FirmwareMajorSwitchWarning() string {
	var b strings.Builder
	b.WriteString(FirmwareMajorSwitchLead)
	for _, c := range FirmwareMajorConsequences {
		b.WriteString("\n  - ")
		b.WriteString(c)
	}
	return b.String()
}

// FirmwareDeviceCounts says how many devices a track holds, by state.
type FirmwareDeviceCounts struct {
	Total       int `json:"total"`
	Enrolling   int `json:"enrolling"`
	Managed     int `json:"managed"`
	Held        int `json:"held"`
	Unenrolling int `json:"unenrolling"`
}

// FirmwareTrack is one of the organization's three tracks.
type FirmwareTrack struct {
	Track      string `json:"track"`
	SoakDays   int    `json:"soak_days"`
	MinSuccess int    `json:"min_success"`
	// Schedule names the schedule the track's windows run on; empty when none
	// is attached. Scheduled is true only when the schedule and its spec are
	// both enabled, so a window will fire.
	Schedule  string `json:"schedule,omitempty"`
	Scheduled bool   `json:"scheduled"`
	NextRunAt string `json:"next_run_at,omitempty"`
	// AutoApplyMajor is whether the track's windows move its devices to the next
	// OPNsense series; off by default. A pointer so that an NDManager that
	// predates major upgrades, which does not send it, is not read as "off".
	AutoApplyMajor *bool                `json:"auto_apply_major,omitempty"`
	Devices        FirmwareDeviceCounts `json:"devices"`
	// CancelledTasks is set by the write that switched auto_apply_major off
	// (also when it was off already): how many major-upgrade steps that had not
	// been dispatched were cancelled. A running upgrade is left alone.
	CancelledTasks *int `json:"cancelled_tasks,omitempty"`
}

// FirmwareConfig is the organization's firmware configuration with its three
// tracks. It is also the answer of every configuration write.
type FirmwareConfig struct {
	OrganizationName string `json:"organization_name"`
	Status           string `json:"status"` // ENABLED, DISABLED
	// Reboot is the default of every advance step: true applies the full
	// update, base and kernel included, and reboots; false applies the
	// userland packages only.
	Reboot bool `json:"reboot"`
	// PlanEnabled is whether the organization's plan includes firmware
	// tracks. While it is false no new steps are sent and the devices still in
	// a track are being released.
	PlanEnabled bool            `json:"plan_enabled"`
	Tracks      []FirmwareTrack `json:"tracks"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
	Message     string          `json:"message,omitempty"`
	// ReleasedDevices is set when Firmware was disabled: how many devices
	// began leaving their track.
	ReleasedDevices *int `json:"released_devices,omitempty"`
}

// FirmwareConfigDeleted is the answer of deleting the configuration.
// Organization is filled in by the client; the server sends only the message.
type FirmwareConfigDeleted struct {
	Organization string `json:"organization,omitempty"`
	Message      string `json:"message"`
}

// FirmwareAssignmentResult is what PUT /firmware/devices did with one device.
type FirmwareAssignmentResult struct {
	DeviceName string `json:"device_name"`
	Result     string `json:"result"` // added, moved, unchanged, released, updated
	// Track is the device's track afterwards; empty while it is leaving or
	// unmanaged. State is enrolling, managed, held or unenrolling, or empty
	// when the device is unmanaged.
	Track string `json:"track,omitempty"`
	State string `json:"state,omitempty"`
	// BootEnvironment is the device's boot-environment setting afterwards: nil
	// for a device released by a move to unmanaged, and for an NDManager that
	// predates the setting. A request that sent the setting never releases, and a
	// current NDManager refuses it for a device in no track, so there a nil is
	// the older NDManager; the client checks that echo.
	BootEnvironment *bool `json:"boot_environment,omitempty"`
	// Notes are fixed codes: agent_too_old, device_disabled.
	Notes []string `json:"notes"`
}

// FirmwareAssignment is the answer of moving devices into a track or out of
// one.
type FirmwareAssignment struct {
	OrganizationName string `json:"organization_name"`
	// Track is where the request moved the devices; empty when it carried only
	// the boot-environment setting.
	Track   string                     `json:"track,omitempty"`
	Results []FirmwareAssignmentResult `json:"results"`
	// CancelledTasks counts firmware steps that had not been dispatched and
	// were cancelled by the move.
	CancelledTasks int `json:"cancelled_tasks"`
}

// FirmwareReleasedInfo says why a device left its track without the operator
// asking.
type FirmwareReleasedInfo struct {
	At     string `json:"at"`
	Reason string `json:"reason"` // release_folder_unavailable, dead_man
}

// FirmwareDevice is one device of the organization and where it stands in the
// tracks. Track and State are empty for a device that is unmanaged.
type FirmwareDevice struct {
	DeviceName    string `json:"device_name"`
	DeviceUUID    string `json:"device_uuid"`
	DeviceStatus  string `json:"device_status"`
	AgentVersion  string `json:"agent_version,omitempty"`
	Track         string `json:"track,omitempty"`
	State         string `json:"state,omitempty"`
	PinnedVersion string `json:"pinned_version,omitempty"`
	// HeldReason is the closed reason of the agent's last hold.
	HeldReason string `json:"held_reason,omitempty"`
	LastRunAt  string `json:"last_run_at,omitempty"`
	// LastStatus is success, noop, held, released, failed or failed_no_response.
	LastStatus string `json:"last_status,omitempty"`
	// LastError is the agent's plain-text message when the last step failed.
	LastError string `json:"last_error,omitempty"`
	// Drift is whether the pin had been changed outside NetDefense at the last
	// step; nil when the last step did not say.
	Drift *bool `json:"drift,omitempty"`
	// ReinstallEntries is how many same-version reinstalls the last check
	// listed. Information only: a pinned device may show them harmlessly.
	ReinstallEntries *int `json:"reinstall_entries,omitempty"`
	// Blocked is why no step is sent to the device: agent_too_old or
	// device_disabled.
	Blocked string `json:"blocked,omitempty"`
	// Revision is the revision of the pinned release the device last proved;
	// nil when unknown, unmanaged, or revisions are not in force.
	Revision *int `json:"revision,omitempty"`
	// Pending is the package updates the last completed check found; nil when
	// no check is known.
	Pending *FirmwarePending `json:"pending,omitempty"`
	// HotfixWarning is set when the device is pinned below a release whose
	// newest revision is a hotfix.
	HotfixWarning *FirmwareHotfixWarning `json:"hotfix_warning,omitempty"`
	// Released is set on an unmanaged device the control plane released by
	// itself, until it is added again.
	Released *FirmwareReleasedInfo `json:"released,omitempty"`
	// Series is the OPNsense series of the pinned release (26.1); empty without
	// a pin, and from an NDManager that predates major upgrades.
	Series string `json:"series,omitempty"`
	// Major is nil unless the device is in a track, managed and enabled; else
	// whether the next window would send it a major upgrade, as far as the
	// device itself goes. The track's own gates are the board's blocked_reason.
	Major *FirmwareDeviceMajor `json:"major,omitempty"`
	// BootEnvironment is the device's boot-environment setting and what the
	// newest major-upgrade answer did about it; nil for a device in no track.
	BootEnvironment *FirmwareBootEnvironment `json:"boot_environment,omitempty"`
	// Notes are the tokens of the newest major-upgrade answer (see
	// FirmwareNotes), kept past the next ordinary window. Never a repository or
	// package name. omitzero keeps the difference between absent (an NDManager
	// that predates them) and the empty list a current one sends.
	Notes []string `json:"notes,omitzero"`
}

// FirmwarePending counts the package updates a device's last completed check
// found, in any repository. Kind is hotfix when installing them would not
// change the OPNsense version, update otherwise, empty when Count is 0.
type FirmwarePending struct {
	Count     int    `json:"count"`
	Kind      string `json:"kind,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
}

// FirmwareHotfixWarning says a device is pinned below the newest release of its
// series and that release was revised after it appeared. ApprovedInTrack false
// means promoting it now moves the device up.
type FirmwareHotfixWarning struct {
	Release         string `json:"release"`
	Revision        int    `json:"revision"`
	DetectedAt      string `json:"detected_at,omitempty"`
	ApprovedInTrack bool   `json:"approved_in_track"`
}

// FirmwareDeviceMajor says whether the next window would send a device a
// major upgrade, as far as the device itself goes.
type FirmwareDeviceMajor struct {
	Eligible bool `json:"eligible"`
	// BlockedBy is empty when eligible, else one of FirmwareDeviceMajorBlocked.
	BlockedBy string `json:"blocked_by,omitempty"`
}

// FirmwareBootEnvironment is a device's setting for making a ZFS boot
// environment before a major upgrade, and what the last upgrade did about it.
type FirmwareBootEnvironment struct {
	// Setting is whether a major upgrade makes a boot environment first; on by
	// default, and the agent does it only on ZFS, best effort.
	Setting bool `json:"setting"`
	// Last is one of FirmwareBootEnvironmentLasts; empty when the newest
	// major-upgrade answer made no decision, or there is none yet.
	Last string `json:"last,omitempty"`
}

// FirmwareDeviceList is a page of FirmwareDevice.
type FirmwareDeviceList struct {
	Items   []FirmwareDevice `json:"items"`
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
	Pages   int              `json:"pages"`
}

// FirmwareHistoryItem is one ledger row: a finished step of a device.
type FirmwareHistoryItem struct {
	CompletedAt string `json:"completed_at"`
	Track       string `json:"track"`
	Action      string `json:"action"`  // enroll, advance, unenroll
	Outcome     string `json:"outcome"` // see FirmwareOutcomes
	Reason      string `json:"reason,omitempty"`
	// Kind is minor (an ordinary update) or major (a major upgrade); empty from
	// an NDManager that predates major upgrades, which only had minors.
	Kind string `json:"kind,omitempty"`
	// Target is the release an advance was moving the device to; the series for
	// a major row.
	Target      string `json:"target,omitempty"`
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
	Applied     bool   `json:"applied"`
	// Revision is the revision of the release the step's answer reported.
	Revision *int `json:"revision,omitempty"`
	// ClearedAt and ClearedBy say when an operator cleared the failure.
	ClearedAt string `json:"cleared_at,omitempty"`
	ClearedBy string `json:"cleared_by,omitempty"`
}

// FirmwareHistory is a page of one device's ledger, newest first.
type FirmwareHistory struct {
	DeviceName string                `json:"device_name"`
	Items      []FirmwareHistoryItem `json:"items"`
	Total      int                   `json:"total"`
	Page       int                   `json:"page"`
	PerPage    int                   `json:"per_page"`
	Pages      int                   `json:"pages"`
}

// FirmwareEvidence is the evidence a release has gathered in the previous
// track against the one the track requires.
type FirmwareEvidence struct {
	// Reached counts the distinct devices of the previous track that reached
	// the release.
	Reached  int `json:"reached"`
	Required int `json:"required"`
}

// FirmwareCandidate is the release a track will take next in a series, and
// what holds it.
type FirmwareCandidate struct {
	Version string `json:"version"`
	// Revision and Fingerprint identify the content of the release folder the
	// candidate is; nil while revisions are not in force.
	Revision    *int   `json:"revision,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	State       string `json:"state"`
	// SoakEndsAt is when the soak is over; empty until its clock has started.
	SoakEndsAt string `json:"soak_ends_at,omitempty"`
	// SoakRemainingSeconds is computed by the server and 0 once the soak is
	// over; nil until the clock has started.
	SoakRemainingSeconds *int `json:"soak_remaining_seconds,omitempty"`
	// Evidence is nil for the fast track, which has no previous track.
	Evidence *FirmwareEvidence `json:"evidence,omitempty"`
	// Stalled is true when the release cannot reach the track without an
	// operator.
	Stalled     bool   `json:"stalled"`
	StallReason string `json:"stall_reason,omitempty"` // evidence_unreachable, blocked_by_failure
}

// FirmwareLatestRelease is the newest release of a series with its newest revision.
type FirmwareLatestRelease struct {
	Version        string `json:"version"`
	LatestRevision int    `json:"latest_revision"`
	RevisedAt      string `json:"revised_at,omitempty"`
}

// FirmwareSeriesStatus is a track's standing in one OPNsense series.
type FirmwareSeriesStatus struct {
	Series string `json:"series"`
	// Target is the release the track moves its devices of this series to.
	Target           string `json:"target,omitempty"`
	TargetApprovedAt string `json:"target_approved_at,omitempty"`
	TargetSource     string `json:"target_source,omitempty"` // auto, manual
	// TargetRevision and TargetFingerprint are the revision the track approved
	// for the target; nil while revisions are not in force.
	TargetRevision    *int   `json:"target_revision,omitempty"`
	TargetFingerprint string `json:"target_fingerprint,omitempty"`
	// LatestRelease is the newest release of the series and its newest revision.
	LatestRelease *FirmwareLatestRelease `json:"latest_release,omitempty"`
	NextCandidate *FirmwareCandidate     `json:"next_candidate,omitempty"`
}

// FirmwareTrackStatus is one track's part of the rollout status.
type FirmwareTrackStatus struct {
	Track      string                 `json:"track"`
	SoakDays   int                    `json:"soak_days"`
	MinSuccess int                    `json:"min_success"`
	Schedule   string                 `json:"schedule,omitempty"`
	Scheduled  bool                   `json:"scheduled"`
	Devices    FirmwareDeviceCounts   `json:"devices"`
	Series     []FirmwareSeriesStatus `json:"series"`
}

// FirmwareFailure is a set of unresolved failures that block a release from
// moving past a track.
type FirmwareFailure struct {
	Version string `json:"version"`
	// Track is the track the failures happened in.
	Track    string   `json:"track"`
	Revision *int     `json:"revision,omitempty"`
	Devices  []string `json:"devices"`
	Count    int      `json:"count"`
	LastAt   string   `json:"last_at"`
}

// FirmwareMajorFailure is a set of unresolved failures of a major upgrade.
// Like FirmwareFailure it blocks the upgrade in the later tracks only, but it
// names the upgrade by its target series and has no version: a series is never
// shown as a release.
type FirmwareMajorFailure struct {
	Kind   string `json:"kind"` // major
	Series string `json:"series"`
	// Track is the track the failures happened in.
	Track   string   `json:"track"`
	Devices []string `json:"devices"`
	Count   int      `json:"count"`
	LastAt  string   `json:"last_at"`
}

// FirmwareHalt is a halt on a release (Version set, Kind minor) or on a major
// upgrade (Series set, Kind major, no Version); UnhaltedAt is set once it is
// lifted.
type FirmwareHalt struct {
	Version string `json:"version,omitempty"`
	Series  string `json:"series,omitempty"`
	// Revision is the one revision halted; nil halts every revision.
	Revision   *int   `json:"revision,omitempty"`
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	HaltedAt   string `json:"halted_at"`
	HaltedBy   string `json:"halted_by"`
	UnhaltedAt string `json:"unhalted_at,omitempty"`
	UnhaltedBy string `json:"unhalted_by,omitempty"`
}

// IsMajor reports whether the halt is on a major upgrade: it names a series, the
// same field Name() reads, so the two cannot disagree.
func (h FirmwareHalt) IsMajor() bool { return h.Series != "" }

// Name is what the halt addresses: the series of a major upgrade, else the
// release.
func (h FirmwareHalt) Name() string {
	if h.Series != "" {
		return h.Series
	}
	return h.Version
}

// FirmwareMajorEntry is where one major upgrade stands in one track.
type FirmwareMajorEntry struct {
	Track string `json:"track"`
	// Series is the series the devices are upgraded to; SourceSeries the one
	// they are upgraded from.
	Series       string `json:"series"`
	SourceSeries string `json:"source_series,omitempty"`
	// State is one of FirmwareMajorStates.
	State      string `json:"state"`
	SoakEndsAt string `json:"soak_ends_at,omitempty"`
	// SoakRemainingSeconds is computed by the server and 0 once the soak is
	// over; nil until its clock has started.
	SoakRemainingSeconds *int `json:"soak_remaining_seconds,omitempty"`
	// Evidence is nil for the fast track, and once the series is approved.
	Evidence    *FirmwareEvidence `json:"evidence,omitempty"`
	Stalled     bool              `json:"stalled"`
	StallReason string            `json:"stall_reason,omitempty"` // evidence_unreachable, blocked_by_failure
	// AutoApplyMajor is the track's own switch; off is "approved, not applied".
	AutoApplyMajor bool `json:"auto_apply_major"`
	// The device counts of the track: those whose major.eligible is true, those
	// whose pin is on the series however they got there, the distinct ones a
	// major upgrade of ours moved onto it, those waiting for the ordinary window
	// that records the final release, and those whose last answer says their
	// mirror does not offer the upgrade yet.
	DevicesEligible              int `json:"devices_eligible"`
	DevicesOnSeries              int `json:"devices_on_series"`
	DevicesHopped                int `json:"devices_hopped"`
	DevicesAwaitingMinorEvidence int `json:"devices_awaiting_minor_evidence"`
	OfferNotSeen                 int `json:"offer_not_seen"`
	// BlockedReason is one of FirmwareMajorBlockedReasons; empty while nothing
	// blocks.
	BlockedReason string `json:"blocked_reason,omitempty"`
	UpgradeOpenAt string `json:"upgrade_open_at,omitempty"`
	ApprovedAt    string `json:"approved_at,omitempty"`
	// ApprovalSource is auto or manual; empty until the track approved the series.
	ApprovalSource string `json:"approval_source,omitempty"`
	// SourceSeriesEndOfLife is true while the path from the source series is
	// open: OPNsense then gives that series no more updates.
	SourceSeriesEndOfLife bool `json:"source_series_end_of_life"`
	// Notice is payload_changed when the upgrade payload was replaced upstream
	// after the track approved the series.
	Notice string `json:"notice,omitempty"`
}

// FirmwareAgentRepoTree is the last probe of one FreeBSD ABI tree of
// NetDefense's own package repository.
type FirmwareAgentRepoTree struct {
	ABI       string `json:"abi"`
	OK        bool   `json:"ok"`
	CheckedAt string `json:"checked_at"`
}

// FirmwareAgentRepo is NetDefense's own package repository as the control plane
// last probed it for the FreeBSD versions a major upgrade crosses. After an
// upgrade across a FreeBSD version a device's NetDefense repository names the
// new version's tree, and a missing tree fails every OPNsense check of the
// device until it is fixed; the device cannot tell that from a flat url or an
// unreachable host, so the probe is the control plane's.
type FirmwareAgentRepo struct {
	// Configured is false while the deployment has no channel configured: the
	// probe is not running.
	Configured bool `json:"configured"`
	// Channel is the repository url the probe guards, so a wrong-environment
	// value shows.
	Channel string `json:"channel,omitempty"`
	// Trees is empty when no result was stored in the last 30 minutes.
	Trees []FirmwareAgentRepoTree `json:"trees"`
}

// FirmwareRollout is where every release stands on the conveyor: per track and
// series, plus the failures that block a release and the active halts. The
// major upgrades (Majors, MajorFailures) and NetDefense's own repository
// (AgentRepo) are beside them, never in the release lists.
type FirmwareRollout struct {
	OrganizationName string                `json:"organization_name"`
	Status           string                `json:"status"` // ENABLED, DISABLED
	PlanEnabled      bool                  `json:"plan_enabled"`
	Tracks           []FirmwareTrackStatus `json:"tracks"`
	Failures         []FirmwareFailure     `json:"failures"`
	Halts            []FirmwareHalt        `json:"halts"`
	// Majors are the major upgrades, by track in conveyor order and then series.
	// Like the keys beside it they are absent from an NDManager that predates
	// them, and an empty list from one that has none to show; omitzero keeps the
	// two apart.
	Majors []FirmwareMajorEntry `json:"majors,omitzero"`
	// MajorFailures are the unresolved failures of major upgrades.
	MajorFailures []FirmwareMajorFailure `json:"major_failures,omitzero"`
	AgentRepo     *FirmwareAgentRepo     `json:"agent_repo,omitempty"`
}

// FirmwareHaltList is a page of halts, newest first. Without a kind it holds the
// halts of releases only; kind major or all adds those of major upgrades, told
// apart by each row's Kind.
type FirmwareHaltList struct {
	Items   []FirmwareHalt `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
	Pages   int            `json:"pages"`
}

// FirmwareHaltResult is the answer of halting or lifting the halt on a release
// or a major upgrade.
type FirmwareHaltResult struct {
	Halt FirmwareHalt `json:"halt"`
	// CancelledTasks counts the steps for the release (or the major upgrade)
	// that had not been dispatched and were cancelled; 0 when a halt is lifted.
	CancelledTasks int `json:"cancelled_tasks"`
}

// FirmwarePromotionResult is what a promotion did in one track.
type FirmwarePromotionResult struct {
	Track  string `json:"track"`
	Result string `json:"result"` // approved, already_approved
	// Target is the track's target for the release's series afterwards.
	Target string `json:"target,omitempty"`
	// TargetRevision is the revision approved; nil while revisions are not in force.
	TargetRevision *int `json:"target_revision,omitempty"`
}

// FirmwarePromotion is the answer of promoting a release (Version) or a major
// upgrade (Series, no Version).
type FirmwarePromotion struct {
	Version string `json:"version,omitempty"`
	Series  string `json:"series,omitempty"`
	// Revision is the revision promoted; nil while revisions are not in force.
	Revision *int                      `json:"revision,omitempty"`
	Results  []FirmwarePromotionResult `json:"results"`
}

// IsMajor reports whether a major upgrade was promoted.
func (p FirmwarePromotion) IsMajor() bool { return p.Series != "" }

// Name is what was promoted: the series, else the release.
func (p FirmwarePromotion) Name() string {
	if p.Series != "" {
		return p.Series
	}
	return p.Version
}

// FirmwareFailuresCleared is the answer of clearing the failures that block a
// release (Version) or a major upgrade (Series, no Version). Track is filled
// in by the client: the track the clear was limited to, empty for every track.
type FirmwareFailuresCleared struct {
	Version string `json:"version,omitempty"`
	Series  string `json:"series,omitempty"`
	Track   string `json:"track,omitempty"`
	// Revision is the revision the clear was limited to, filled in by the
	// client; nil for every revision.
	Revision *int `json:"revision,omitempty"`
	Cleared  int  `json:"cleared"`
}

// IsMajor reports whether the failures of a major upgrade were cleared.
func (c FirmwareFailuresCleared) IsMajor() bool { return c.Series != "" }

// Name is what the clear addressed: the series, else the release.
func (c FirmwareFailuresCleared) Name() string {
	if c.Series != "" {
		return c.Series
	}
	return c.Version
}

// FirmwareTrackResult is the result data block of a COMPLETED FIRMWARE_TRACK
// task. The agent stores it serialised as JSON inside Task.Message. Field
// names mirror NDDataModels' validate_firmware_track_result exactly; the
// version keys are omitted by the agent when it has no value for them.
type FirmwareTrackResult struct {
	Action  string `json:"action"`
	Track   string `json:"track"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	// Drift says the pin was found changed outside NetDefense.
	Drift bool `json:"drift"`
	// ReinstallEntries is how many same-version reinstall entries the check
	// listed. Information only: it never held a step.
	ReinstallEntries int    `json:"reinstall_entries"`
	FromVersion      string `json:"from_version,omitempty"`
	ToVersion        string `json:"to_version,omitempty"`
	// TargetVersion is the release the step was moving to; for a major upgrade
	// (Kind major) it is the series.
	TargetVersion string `json:"target_version,omitempty"`
	PinnedVersion string `json:"pinned_version,omitempty"`
	// Kind is "major" on every answer to a step that carried a major upgrade
	// from the moment the agent decided to run it, a hold included; absent on
	// the ordinary step's answers.
	Kind string `json:"kind,omitempty"`
	// Notes are what the agent found on the way (see FirmwareNotes), valid only
	// with Kind.
	Notes []string `json:"notes,omitempty"`
}

// IsMajor reports whether the result answers a major upgrade.
func (r FirmwareTrackResult) IsMajor() bool { return r.Kind == FirmwareKindMajor }

// firmwareTrackResultMaxBytes is the most a task message may hold and still be
// read as a result. A real one is a few hundred bytes (the contract allows 2 KiB)
// and the broker cuts any message at 16 KiB, so a longer one is not a result and
// is not worth decoding for every row of a task list.
const firmwareTrackResultMaxBytes = 16 << 10

// ParseFirmwareTrackResult reads a FIRMWARE_TRACK task's message. It returns
// nil when the message is not a result block: a task still pending carries the
// plain sentence the control plane wrote, and a failed one carries the agent's
// plain-text error, both of which render as the message they are.
//
// Task.Message is JSON held in a string. The decode of the Task sanitized
// that outer string only, so a \uXXXX escape inside it was still literal text
// then and becomes a live control byte here; the result is sanitized again
// before anyone can print it.
func ParseFirmwareTrackResult(raw string) *FirmwareTrackResult {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' || len(raw) > firmwareTrackResultMaxBytes {
		return nil
	}
	var result FirmwareTrackResult
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&result); err != nil {
		return nil
	}
	// Claim the format only when the keys that make it a result are there, so
	// a generic JSON message is never taken for one.
	if result.Action == "" || result.Outcome == "" {
		return nil
	}
	sanitize.Struct(reflect.ValueOf(&result))
	return &result
}
