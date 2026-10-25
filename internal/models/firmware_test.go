package models

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The firmware models mirror NDManager's /firmware response models and
// NDDataModels' FIRMWARE_TRACK contract. NDCLI-go's CI cannot read either
// repository, so the wire values below are literal copies: when one of them
// adds, renames or drops a value, these tests fail until the lists in
// firmware.go and the copies here change together.

var (
	// NDDataModels Schema.FIRMWARE_TRACK_NAMES.
	nddataModelsFirmwareTracks = []string{"fast", "regular", "stable"}
	// NDDataModels DB.FirmwareTrackState, lowercased the way NDManager sends it.
	nddataModelsFirmwareStates = []string{"enrolling", "managed", "held", "unenrolling"}
	// NDDataModels DB.FirmwareOutcome, lowercased.
	nddataModelsFirmwareOutcomes = []string{"success", "noop", "held", "released", "failed", "failed_no_response"}
	// NDDataModels Schema.FIRMWARE_TRACK_ACTIONS.
	nddataModelsFirmwareActions = []string{"enroll", "advance", "unenroll"}
	// NDDataModels v1.42.0 (7a847f0) Schema.FIRMWARE_LEDGER_REASONS in the tag's
	// order: FIRMWARE_TRACK_REASONS, the agent's closed set, then the control
	// plane's dead_man. held_test_mode is at index 14; the five holds of the
	// major upgrade (held_late_dispatch, held_insufficient_space, held_ha_member,
	// held_agent_abi_unavailable, held_upgrade_aborted) follow it, before applied,
	// and all of them are in FIRMWARE_HELD_REASONS. A server that predates the tag
	// never sends the ones it does not have.
	nddataModelsFirmwareReasons = []string{
		"at_or_above_target", "target_not_yet_offered", "series_not_approved", "pin_drift",
		"major_offered", "held_check_failed", "held_release_folder_missing", "release_folder_unavailable",
		"held_downgrade_risk", "held_unsupported_edition", "held_custom_flavour", "held_firmware_busy",
		"held_unexpected_plan", "held_reboot_pending", "held_test_mode",
		"held_late_dispatch", "held_insufficient_space", "held_ha_member", "held_agent_abi_unavailable", "held_upgrade_aborted",
		"held_revision_mismatch",
		"applied", "enrolled", "unenrolled",
		"dead_man",
	}
	// NDDataModels v1.42.0 Schema.FIRMWARE_TRACK_NOTES, in the tag's order: what
	// an answer to a major upgrade notes about the device. Tokens only.
	nddataModelsFirmwareNotes = []string{
		"boot_environment_made", "boot_environment_skipped_no_room", "boot_environment_off", "boot_environment_unavailable",
		"third_party_repository", "third_party_package",
	}
	// NDManager b4e3de2 (1.97.0) openapi.json: the closed sets of the board and
	// of the device rows, as the field descriptions list them. Lowercase on the
	// wire.
	ndmanagerMajorStates = []string{
		"path_closed", "soaking", "ready", "waiting_for_previous_track", "waiting_for_evidence",
		"blocked_by_failure", "halted", "approved",
	}
	ndmanagerMajorBlockedReasons = []string{
		"deployment_switch_off", "reboot_disabled", "agents_too_old", "no_supported_hop", "path_closed",
		"final_minor_not_approved", "no_device_at_final_build", "awaiting_minor_evidence",
		"previous_track_empty", "previous_track_applies_no_majors", "previous_track_devices_cannot_hop",
	}
	ndmanagerDeviceMajorBlocked = []string{
		"agent_below_major_floor", "no_supported_hop", "final_minor_not_installed", "awaiting_minor_evidence", "step_in_flight",
		"held_ha_member", "held_agent_abi_unavailable", "held_insufficient_space", "held_upgrade_aborted",
		"held_unsupported_edition", "held_custom_flavour", "held_test_mode",
	}
	ndmanagerBootEnvironmentLasts = []string{"made", "skipped_no_room", "off", "unavailable"}
)

// TestScheduledTaskKindsMatchNDDataModels pins the spec kinds to a literal copy
// of NDDataModels' ScheduledTaskKind (DB.py): FIRMWARE is a track's update
// window, bound to the track by the control plane.
func TestScheduledTaskKindsMatchNDDataModels(t *testing.T) {
	want := []string{"RUN", "SYNC", "BACKUP", "FIRMWARE"}
	got := []string{ScheduledTaskKindRun, ScheduledTaskKindSync, ScheduledTaskKindBackup, ScheduledTaskKindFirmware}
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("spec kinds = %v, want the NDDataModels set %v", got, want)
	}
}

func TestFirmwareWireValuesMatchNDDataModels(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want []string
	}{
		{"tracks", FirmwareTracks, nddataModelsFirmwareTracks},
		{"states", FirmwareStates, nddataModelsFirmwareStates},
		{"outcomes", FirmwareOutcomes, nddataModelsFirmwareOutcomes},
	} {
		if !reflect.DeepEqual(sortedCopy(tc.got), sortedCopy(tc.want)) {
			t.Errorf("%s = %v, want the NDDataModels set %v", tc.name, tc.got, tc.want)
		}
	}
	// The reasons are held to the tag's order too: it is part of NDDataModels' contract.
	if !slices.Equal(FirmwareReasons, nddataModelsFirmwareReasons) {
		t.Errorf("reasons = %v, want the NDDataModels v1.44.0 list, in its order: %v", FirmwareReasons, nddataModelsFirmwareReasons)
	}
	if !slices.Equal(FirmwareNotes, nddataModelsFirmwareNotes) {
		t.Errorf("notes = %v, want the NDDataModels v1.42.0 list, in its order: %v", FirmwareNotes, nddataModelsFirmwareNotes)
	}
	for name, got := range map[string]string{
		"enroll": FirmwareActionEnroll, "advance": FirmwareActionAdvance, "unenroll": FirmwareActionUnenroll,
	} {
		if got != name || !slices.Contains(nddataModelsFirmwareActions, got) {
			t.Errorf("action constant %q is not an NDDataModels action", got)
		}
	}
	// The board's strings are one closed lowercase set each. NDManager's own
	// descriptions list them; a string outside a set is shown as sent, never an
	// error (TestFirmwareVocabulariesShowAnUnknownStringAsItself).
	for _, tc := range []struct {
		name      string
		got, want []string
	}{
		{"major states", FirmwareMajorStates, ndmanagerMajorStates},
		{"blocked reasons", FirmwareMajorBlockedReasons, ndmanagerMajorBlockedReasons},
		{"device major blocked_by", FirmwareDeviceMajorBlocked, ndmanagerDeviceMajorBlocked},
		{"boot-environment last", FirmwareBootEnvironmentLasts, ndmanagerBootEnvironmentLasts},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s = %v, want NDManager's set %v", tc.name, tc.got, tc.want)
		}
	}
	for _, set := range [][]string{FirmwareMajorStates, FirmwareMajorBlockedReasons, FirmwareDeviceMajorBlocked, FirmwareBootEnvironmentLasts} {
		for _, word := range set {
			if word != strings.ToLower(word) || strings.TrimSpace(word) != word {
				t.Errorf("%q must be lowercase and trimmed: the board's strings are", word)
			}
		}
	}
	// The persistent holds a device's blocked_by can name are hold reasons.
	for _, code := range FirmwareDeviceMajorBlocked {
		if strings.HasPrefix(code, "held_") && !slices.Contains(FirmwareReasons, code) {
			t.Errorf("blocked_by %q is a hold, and no reason of that name exists", code)
		}
	}
	// A kind is minor or major; only the list of halts takes all.
	if !slices.Equal(FirmwareKinds, []string{"minor", "major"}) || !slices.Equal(FirmwareHaltKinds, []string{"minor", "major", "all"}) {
		t.Errorf("kinds = %v / %v", FirmwareKinds, FirmwareHaltKinds)
	}
	if FirmwareAssignmentUpdated != "updated" || FirmwareApprovalAuto != "auto" || FirmwareApprovalManual != "manual" {
		t.Errorf("assignment/approval constants: %q %q %q", FirmwareAssignmentUpdated, FirmwareApprovalAuto, FirmwareApprovalManual)
	}
	// The configuration status is the one wire value that is upper case.
	if FirmwareConfigEnabled != "ENABLED" || FirmwareConfigDisabled != "DISABLED" {
		t.Errorf("config status = %q/%q, want ENABLED/DISABLED", FirmwareConfigEnabled, FirmwareConfigDisabled)
	}
}

// TestFirmwareTrackInputListsAreDistinct: the three lists a --track flag draws
// from share their first three words and differ in the fourth, and none may
// alias another (an append to one would show in the others).
func TestFirmwareTrackInputListsAreDistinct(t *testing.T) {
	if want := []string{"fast", "regular", "stable", "unmanaged"}; !reflect.DeepEqual(FirmwareAssignTracks, want) {
		t.Errorf("FirmwareAssignTracks = %v, want %v", FirmwareAssignTracks, want)
	}
	if want := []string{"fast", "regular", "stable", "all"}; !reflect.DeepEqual(FirmwarePromoteTracks, want) {
		t.Errorf("FirmwarePromoteTracks = %v, want %v", FirmwarePromoteTracks, want)
	}
	if &FirmwareTracks[0] == &FirmwareAssignTracks[0] || &FirmwareAssignTracks[0] == &FirmwarePromoteTracks[0] {
		t.Error("the track lists must be separate slices")
	}
}

// The five holds of the major upgrade sit after held_test_mode and before
// applied, in this order (NDDataModels v1.42.0), and each is a hold with a
// sentence of its own.
func TestFirmwareReasonsCarryTheFiveHoldsOfTheMajorUpgradeInPlace(t *testing.T) {
	five := []string{"held_late_dispatch", "held_insufficient_space", "held_ha_member", "held_agent_abi_unavailable", "held_upgrade_aborted"}
	start := slices.Index(FirmwareReasons, "held_test_mode") + 1
	if start == 0 || !slices.Equal(FirmwareReasons[start:start+5], five) {
		t.Fatalf("the five holds are not right after held_test_mode: %v", FirmwareReasons)
	}
	if FirmwareReasons[start+5] != "held_revision_mismatch" || FirmwareReasons[start+6] != "applied" {
		t.Errorf("held_revision_mismatch then applied must follow them, got %q %q", FirmwareReasons[start+5], FirmwareReasons[start+6])
	}
	if FirmwareReasonText("held_revision_mismatch") == "held_revision_mismatch" {
		t.Error("held_revision_mismatch has no sentence")
	}
	for _, code := range five {
		if FirmwareReasonText(code) == code {
			t.Errorf("%s has no sentence", code)
		}
	}
	if got := FirmwareReasonText("held_agent_abi_unavailable"); !strings.Contains(got, "not ABI-nested") ||
		!strings.Contains(got, "unreachable") || !strings.Contains(got, "tree is missing") {
		t.Errorf("held_agent_abi_unavailable should give its three causes in plain words: %q", got)
	}
	// major_offered stays valid: a phase 1 agent still emits it.
	if !slices.Contains(FirmwareReasons, "major_offered") {
		t.Error("major_offered must stay a known reason")
	}
}

func TestFirmwareReasonText(t *testing.T) {
	for _, code := range FirmwareReasons {
		text := FirmwareReasonText(code)
		if text == "" || text == code {
			t.Errorf("reason %q has no sentence of its own", code)
		}
		// A sentence is embedded in tables and lines: no leading capital, no
		// trailing full stop.
		if text != strings.ToLower(text[:1])+text[1:] || strings.HasSuffix(text, ".") {
			t.Errorf("reason %q: %q should start in lower case and carry no full stop", code, text)
		}
	}
	// A reason this build has never heard of still shows, as itself.
	if got := FirmwareReasonText("held_from_the_future"); got != "held_from_the_future" {
		t.Errorf("an unknown reason = %q, want the code itself", got)
	}
	if got := FirmwareReasonText(""); got != "" {
		t.Errorf("an empty reason = %q, want it left empty", got)
	}
}

func TestFirmwareNoteText(t *testing.T) {
	for _, token := range FirmwareNotes {
		text := FirmwareNoteText(token)
		if text == "" || text == token {
			t.Errorf("note %q has no sentence of its own", token)
		}
		if text != strings.ToLower(text[:1])+text[1:] || strings.HasSuffix(text, ".") {
			t.Errorf("note %q: %q should start in lower case and carry no full stop", token, text)
		}
	}
	// A token a newer server sends still shows, as itself.
	if got := FirmwareNoteText("third_party_firmware"); got != "third_party_firmware" {
		t.Errorf("an unknown note = %q, want the token itself", got)
	}
	// The sentences say what the token means and nothing it does not: the two
	// third-party ones never name a repository or a package.
	for token, wants := range map[string][]string{
		"boot_environment_made":            {"boot environment", "made"},
		"boot_environment_skipped_no_room": {"no room"},
		"boot_environment_off":             {"setting is off"},
		"boot_environment_unavailable":     {"no ZFS"},
		"third_party_repository":           {"repository", "outside OPNsense and NetDefense"},
		"third_party_package":              {"package", "outside OPNsense and NetDefense"},
	} {
		for _, want := range wants {
			if !strings.Contains(FirmwareNoteText(token), want) {
				t.Errorf("%s: %q should mention %q", token, FirmwareNoteText(token), want)
			}
		}
	}
	for _, token := range FirmwareNotes {
		if want := strings.HasPrefix(token, "boot_environment_"); FirmwareNoteIsBootEnvironment(token) != want {
			t.Errorf("FirmwareNoteIsBootEnvironment(%q) = %v", token, !want)
		}
	}
}

// Every closed set has a sentence for each of its words, and a word it does not
// know comes back as itself: the sets say what has a sentence, they are never a
// filter.
func TestFirmwareVocabulariesShowAnUnknownStringAsItself(t *testing.T) {
	for _, tc := range []struct {
		name  string
		words []string
		text  func(string) string
	}{
		{"major states", FirmwareMajorStates, FirmwareMajorStateText},
		{"blocked reasons", FirmwareMajorBlockedReasons, FirmwareMajorBlockedReasonText},
		{"device blocked_by", FirmwareDeviceMajorBlocked, FirmwareDeviceMajorBlockedText},
		{"boot-environment last", FirmwareBootEnvironmentLasts, FirmwareBootEnvironmentLastText},
	} {
		for _, word := range tc.words {
			if text := tc.text(word); text == "" || text == word {
				t.Errorf("%s: %q has no sentence of its own (%q)", tc.name, word, text)
			}
		}
		if got := tc.text("from_the_future"); got != "from_the_future" {
			t.Errorf("%s: an unknown string = %q, want it as sent", tc.name, got)
		}
		if got := tc.text(""); got != "" {
			t.Errorf("%s: an empty string = %q, want it left empty", tc.name, got)
		}
	}
	// The holds a device can be blocked by read as the hold reason they are.
	if got, want := FirmwareDeviceMajorBlockedText("held_ha_member"), FirmwareReasonText("held_ha_member"); got != want {
		t.Errorf("held_ha_member = %q, want the hold's own sentence %q", got, want)
	}
}

// What turning major upgrades on means is written out here, not read from the
// constants the CLI prompt and the MCP preview are built from: a test that
// sourced the sentences from the implementation would agree with it by
// construction. The text says the upgrade is offline,
// takes three reboots and has no rollback.
func TestFirmwareMajorSwitchWarningStatesTheConsequences(t *testing.T) {
	warning := FirmwareMajorSwitchWarning()
	for _, want := range []string{
		"With major upgrades on, the track's update windows move its devices to the next OPNsense series.",
		"takes the device offline for the length of the upgrade and reboots it three times;",
		"has no automatic rollback;",
		"leaves a device that loses power during the base or package install in a state that needs the console or a VM snapshot to recover;",
		"needs a link of about 3 Mbit/s or better, so the download finishes inside the step;",
		"needs 3.25 GiB free on the filesystem that holds the update cache;",
		"on ZFS makes a boot environment first when the pool has room and the device's setting allows it; that is a recovery path from the console only, and the agent never removes it;",
		"after a manual rollback, needs the device rebound: stop the track from upgrading it again first (switch major upgrades off, or halt the series), and clear the failures it caused afterwards.",
	} {
		if !strings.Contains(warning, want) {
			t.Errorf("the warning is missing %q:\n%s", want, warning)
		}
	}
	// It names no flag and no tool: every front-end shows it as it is.
	for _, bad := range []string{"--", "ndcli", "confirm"} {
		if strings.Contains(warning, bad) {
			t.Errorf("the warning names %q, which belongs to one front-end:\n%s", bad, warning)
		}
	}
	if len(FirmwareMajorConsequences) != 7 {
		t.Errorf("%d consequences, want 7", len(FirmwareMajorConsequences))
	}
}

// ── the response models ──────────────────────────────────────────────────────
//
// Each fixture sets every property of its NDManager response schema, so a
// field the model dropped or renamed comes back different from what went in.
// `filters` on the device list is the one property the CLI deliberately does
// not carry: it only echoes the query the caller sent.

const (
	firmwareConfigFixture = `{
  "organization_name": "acme-corp",
  "status": "ENABLED",
  "reboot": false,
  "plan_enabled": true,
  "tracks": [
    {"track": "fast", "soak_days": 1, "min_success": 0, "schedule": "saturday-night", "scheduled": true,
     "next_run_at": "2026-10-10T02:00:00Z", "auto_apply_major": true,
     "devices": {"total": 4, "enrolling": 1, "managed": 2, "held": 1, "unenrolling": 0}}
  ],
  "created_at": "2026-10-08T12:00:00Z",
  "updated_at": "2026-10-09T08:30:00Z",
  "message": "Firmware disabled; every device in a track is being released",
  "released_devices": 4
}`
	// FirmwareTrackWriteResponse, the answer of PUT /tracks/{track}: the track
	// with cancelled_tasks, set when the request switched major upgrades off.
	firmwareTrackFixture = `{
  "track": "regular", "soak_days": 9, "min_success": 1, "schedule": "saturday-night", "scheduled": true,
  "next_run_at": "2026-10-10T02:00:00Z", "auto_apply_major": false, "cancelled_tasks": 2,
  "devices": {"total": 2, "enrolling": 0, "managed": 2, "held": 0, "unenrolling": 0}
}`
	firmwareAssignmentFixture = `{
  "organization_name": "acme-corp",
  "track": "fast",
  "results": [
    {"device_name": "fw-a", "result": "added", "track": "fast", "state": "enrolling", "boot_environment": false,
     "notes": ["agent_too_old", "device_disabled"]},
    {"device_name": "fw-b", "result": "updated", "track": "fast", "state": "managed", "boot_environment": true, "notes": []}
  ],
  "cancelled_tasks": 2
}`
	firmwareDeviceListFixture = `{
  "items": [
    {"device_name": "fw-a", "device_uuid": "3f9d2c1e-7a4b-4c55-9e0a-0d1f2b3c4d5e", "device_status": "ENABLED",
     "agent_version": "1.24.0", "track": "fast", "state": "held", "pinned_version": "26.7.3",
     "held_reason": "held_custom_flavour", "last_run_at": "2026-10-08T12:00:00Z", "last_status": "failed",
     "last_error": "the update did not finish", "drift": true, "reinstall_entries": 1, "blocked": "agent_too_old",
     "released": {"at": "2026-10-01T00:00:00Z", "reason": "dead_man"},
     "series": "26.7", "major": {"eligible": false, "blocked_by": "held_insufficient_space"},
     "boot_environment": {"setting": false, "last": "skipped_no_room"},
     "notes": ["boot_environment_skipped_no_room", "third_party_repository"]}
  ],
  "total": 1, "page": 1, "per_page": 50, "pages": 1
}`
	firmwareHistoryFixture = `{
  "device_name": "fw-a",
  "items": [
    {"completed_at": "2026-10-08T12:00:00Z", "track": "fast", "action": "advance", "outcome": "success",
     "reason": "applied", "kind": "minor", "target": "26.7.5", "from_version": "26.7.3", "to_version": "26.7.5", "applied": true,
     "cleared_at": "2026-10-08T13:00:00Z", "cleared_by": "admin@acme.com"},
    {"completed_at": "2026-10-09T12:00:00Z", "track": "fast", "action": "advance", "outcome": "success",
     "reason": "applied", "kind": "major", "target": "26.7", "from_version": "26.1.11_10", "to_version": "26.7.1_1", "applied": true,
     "cleared_at": "2026-10-09T13:00:00Z", "cleared_by": "admin@acme.com"}
  ],
  "total": 1, "page": 1, "per_page": 50, "pages": 1
}`
	firmwareRolloutFixture = `{
  "organization_name": "acme-corp",
  "status": "ENABLED",
  "plan_enabled": true,
  "tracks": [
    {"track": "regular", "soak_days": 9, "min_success": 1, "schedule": "saturday-night", "scheduled": true,
     "devices": {"total": 2, "enrolling": 0, "managed": 2, "held": 0, "unenrolling": 0},
     "series": [
       {"series": "26.7", "target": "26.7.3", "target_approved_at": "2026-10-01T00:00:00Z", "target_source": "manual",
        "next_candidate": {"version": "26.7.4", "state": "waiting_for_evidence",
                           "soak_ends_at": "2026-10-20T00:00:00Z", "soak_remaining_seconds": 86400,
                           "evidence": {"reached": 0, "required": 1}, "stalled": true,
                           "stall_reason": "evidence_unreachable"}}
     ]}
  ],
  "failures": [
    {"version": "26.7.4", "track": "fast", "devices": ["fw-b"], "count": 1, "last_at": "2026-10-08T12:00:00Z"}
  ],
  "halts": [
    {"version": "26.7.5", "kind": "minor", "reason": "regression", "halted_at": "2026-10-08T12:00:00Z",
     "halted_by": "admin@acme.com", "unhalted_at": "2026-10-09T12:00:00Z", "unhalted_by": "ops@acme.com"}
  ],
  "majors": [
    {"track": "regular", "series": "26.7", "source_series": "26.1", "state": "waiting_for_evidence",
     "soak_ends_at": "2026-10-20T00:00:00Z", "soak_remaining_seconds": 3600, "evidence": {"reached": 0, "required": 1},
     "stalled": true, "stall_reason": "evidence_unreachable", "auto_apply_major": true,
     "devices_eligible": 2, "devices_on_series": 1, "devices_hopped": 1, "devices_awaiting_minor_evidence": 1, "offer_not_seen": 1,
     "blocked_reason": "previous_track_applies_no_majors", "upgrade_open_at": "2026-09-20T00:00:00Z",
     "approved_at": "2026-10-01T00:00:00Z", "approval_source": "manual", "source_series_end_of_life": true,
     "notice": "payload_changed"}
  ],
  "major_failures": [
    {"kind": "major", "series": "26.7", "track": "fast", "devices": ["fw-b"], "count": 1, "last_at": "2026-10-08T12:00:00Z"}
  ],
  "agent_repo": {"configured": true, "channel": "https://repo.netdefense.io/dev/opnsense",
                 "trees": [{"abi": "FreeBSD:15:amd64", "ok": false, "checked_at": "2026-10-12T12:00:00Z"}]}
}`
	// FirmwareHaltListResponse with kind=all: a halt of a release has a version,
	// one of a major upgrade (FirmwareSeriesHaltItem) its series and no version.
	firmwareHaltListFixture = `{
  "items": [
    {"version": "26.7.5", "kind": "minor", "reason": "regression", "halted_at": "2026-10-08T12:00:00Z",
     "halted_by": "admin@acme.com", "unhalted_at": "2026-10-09T12:00:00Z", "unhalted_by": "ops@acme.com"},
    {"series": "26.7", "kind": "major", "reason": "payload changed", "halted_at": "2026-10-10T12:00:00Z",
     "halted_by": "admin@acme.com", "unhalted_at": "2026-10-11T12:00:00Z", "unhalted_by": "ops@acme.com"}
  ],
  "total": 2, "page": 1, "per_page": 50, "pages": 1
}`
	firmwareHaltResultFixture = `{
  "halt": {"version": "26.7.5", "kind": "minor", "reason": "regression", "halted_at": "2026-10-08T12:00:00Z",
           "halted_by": "admin@acme.com", "unhalted_at": "2026-10-09T12:00:00Z", "unhalted_by": "ops@acme.com"},
  "cancelled_tasks": 3
}`
	// FirmwareSeriesHaltResponse, FirmwareSeriesPromoteResponse and
	// FirmwareSeriesClearFailureResponse: the answers of the operations on a
	// major upgrade, with series in place of version.
	firmwareSeriesHaltResultFixture = `{
  "halt": {"series": "26.7", "kind": "major", "reason": "payload changed", "halted_at": "2026-10-10T12:00:00Z",
           "halted_by": "admin@acme.com", "unhalted_at": "2026-10-11T12:00:00Z", "unhalted_by": "ops@acme.com"},
  "cancelled_tasks": 3
}`
	firmwareSeriesPromotionFixture = `{
  "series": "26.7",
  "results": [{"track": "fast", "result": "approved", "target": "26.7"}]
}`
	firmwareSeriesClearedFixture = `{"series": "26.7", "cleared": 1}`
	firmwarePromotionFixture     = `{
  "version": "26.7.5",
  "results": [
    {"track": "fast", "result": "approved", "target": "26.7.5"},
    {"track": "regular", "result": "already_approved", "target": "26.7.5"}
  ]
}`
	firmwareClearedFixture = `{"version": "26.7.5", "cleared": 2}`
	firmwareDeletedFixture = `{"message": "Firmware configuration deleted"}`
)

func TestFirmwareModelsCarryEveryPropertyOfTheContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		target  interface{}
		// omitted are properties the CLI deliberately does not carry.
		omitted []string
	}{
		{"FirmwareConfigResponse", firmwareConfigFixture, &FirmwareConfig{}, nil},
		{"FirmwareTrackResponse", firmwareTrackFixture, &FirmwareTrack{}, nil},
		{"FirmwareAssignmentResponse", firmwareAssignmentFixture, &FirmwareAssignment{}, nil},
		{"FirmwareDeviceListResponse", firmwareDeviceListFixture, &FirmwareDeviceList{}, nil},
		{"FirmwareHistoryListResponse", firmwareHistoryFixture, &FirmwareHistory{}, nil},
		{"FirmwareStatusResponse", firmwareRolloutFixture, &FirmwareRollout{}, nil},
		{"FirmwareHaltListResponse", firmwareHaltListFixture, &FirmwareHaltList{}, nil},
		{"FirmwareHaltResponse", firmwareHaltResultFixture, &FirmwareHaltResult{}, nil},
		{"FirmwarePromoteResponse", firmwarePromotionFixture, &FirmwarePromotion{}, nil},
		{"FirmwareClearFailureResponse", firmwareClearedFixture, &FirmwareFailuresCleared{}, nil},
		{"FirmwareConfigDeleteResponse", firmwareDeletedFixture, &FirmwareConfigDeleted{}, nil},
		{"FirmwareTrackWriteResponse", firmwareTrackFixture, &FirmwareTrack{}, nil},
		{"FirmwareSeriesHaltResponse", firmwareSeriesHaltResultFixture, &FirmwareHaltResult{}, nil},
		{"FirmwareSeriesPromoteResponse", firmwareSeriesPromotionFixture, &FirmwarePromotion{}, nil},
		{"FirmwareSeriesClearFailureResponse", firmwareSeriesClearedFixture, &FirmwareFailuresCleared{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want interface{}
			if err := json.Unmarshal([]byte(tc.fixture), &want); err != nil {
				t.Fatalf("fixture is not JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(tc.fixture), tc.target); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out, err := json.Marshal(tc.target)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var got interface{}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("re-encoded output is not JSON: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the model lost or renamed a property:\n got  %s\n want %s", out, tc.fixture)
			}
		})
	}
}

// TestFirmwareModelsAcceptNullsAndAbsences: the server sends null for what is
// not set, and an older one may omit a field. Both decode to the zero value.
func TestFirmwareModelsAcceptNullsAndAbsences(t *testing.T) {
	var device FirmwareDevice
	body := `{"device_name": "fw-a", "device_uuid": "u", "device_status": "ENABLED", "agent_version": null,
	  "track": null, "state": null, "pinned_version": null, "held_reason": null, "last_run_at": null,
	  "last_status": null, "last_error": null, "drift": null, "reinstall_entries": null, "blocked": null,
	  "released": null}`
	if err := json.Unmarshal([]byte(body), &device); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if device.Track != "" || device.Drift != nil || device.ReinstallEntries != nil || device.Released != nil {
		t.Errorf("nulls should decode to the zero value: %+v", device)
	}

	var track FirmwareTrack
	if err := json.Unmarshal([]byte(`{"track": "stable", "soak_days": 10, "min_success": 1, "schedule": null,
	  "scheduled": false, "next_run_at": null, "devices": {"total": 0}}`), &track); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if track.Schedule != "" || track.NextRunAt != "" || track.Scheduled {
		t.Errorf("an unscheduled track should decode with no schedule: %+v", track)
	}

	var cand FirmwareCandidate
	if err := json.Unmarshal([]byte(`{"version": "26.7.5", "state": "ready", "soak_ends_at": null,
	  "soak_remaining_seconds": 0, "evidence": null, "stalled": false, "stall_reason": null}`), &cand); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 0 is a real answer (the soak is over); null is not.
	if cand.SoakRemainingSeconds == nil || *cand.SoakRemainingSeconds != 0 || cand.Evidence != nil {
		t.Errorf("soak_remaining_seconds 0 must survive, evidence null must stay nil: %+v", cand)
	}
}

// A control plane that predates major upgrades sends none of their fields: they
// decode to nothing, which is not the same as a setting that is off (a track's
// switch is a pointer for it). And the nulls of a current one: a track set to
// leave, a device in no track, a halt with no version.
func TestFirmwareMajorFieldsAreAbsentOrNullWithoutBeingZeroValuesThatLie(t *testing.T) {
	var old FirmwareTrack
	if err := json.Unmarshal([]byte(`{"track": "fast", "soak_days": 1, "min_success": 0, "scheduled": false, "devices": {"total": 0}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.AutoApplyMajor != nil || old.CancelledTasks != nil {
		t.Errorf("a track without the switch must not read as off: %+v", old)
	}
	var off FirmwareTrack
	if err := json.Unmarshal([]byte(`{"track": "fast", "soak_days": 1, "min_success": 0, "scheduled": false, "auto_apply_major": false, "cancelled_tasks": 0, "devices": {"total": 0}}`), &off); err != nil {
		t.Fatal(err)
	}
	if off.AutoApplyMajor == nil || *off.AutoApplyMajor || off.CancelledTasks == nil || *off.CancelledTasks != 0 {
		t.Errorf("off and 0 cancelled are answers, and survive: %+v", off)
	}

	var device FirmwareDevice
	if err := json.Unmarshal([]byte(`{"device_name": "fw-a", "device_uuid": "u", "device_status": "ENABLED",
	  "series": null, "major": null, "boot_environment": null, "notes": []}`), &device); err != nil {
		t.Fatal(err)
	}
	if device.Series != "" || device.Major != nil || device.BootEnvironment != nil || len(device.Notes) != 0 {
		t.Errorf("nulls should decode to nothing: %+v", device)
	}
	var eligible FirmwareDevice
	if err := json.Unmarshal([]byte(`{"device_name": "fw-a", "device_uuid": "u", "device_status": "ENABLED",
	  "series": "26.1", "major": {"eligible": true, "blocked_by": null}, "boot_environment": {"setting": true, "last": null}}`), &eligible); err != nil {
		t.Fatal(err)
	}
	if eligible.Major == nil || !eligible.Major.Eligible || eligible.Major.BlockedBy != "" ||
		eligible.BootEnvironment == nil || !eligible.BootEnvironment.Setting || eligible.BootEnvironment.Last != "" {
		t.Errorf("an eligible device with no decision yet: %+v", eligible)
	}

	// A request that carried only the setting is answered with no track, and a
	// device that left has no setting.
	var assign FirmwareAssignment
	if err := json.Unmarshal([]byte(`{"organization_name": "acme", "track": null, "cancelled_tasks": 1,
	  "results": [{"device_name": "fw-a", "result": "updated", "track": "fast", "state": "managed", "boot_environment": false, "notes": []},
	              {"device_name": "fw-b", "result": "released", "track": null, "state": "unenrolling", "boot_environment": null, "notes": []}]}`), &assign); err != nil {
		t.Fatal(err)
	}
	if assign.Track != "" || assign.Results[0].BootEnvironment == nil || *assign.Results[0].BootEnvironment || assign.Results[1].BootEnvironment != nil {
		t.Errorf("assignment = %+v", assign)
	}
	out, _ := json.Marshal(assign)
	if strings.Contains(string(out), `"track":""`) {
		t.Errorf("a null track must not come back as an empty string: %s", out)
	}

	// A halt of a series has no version, and says so when it is printed.
	var halt FirmwareHalt
	if err := json.Unmarshal([]byte(`{"series": "26.7", "kind": "major", "reason": "r", "halted_at": "t", "halted_by": "a@b.c", "unhalted_at": null, "unhalted_by": null}`), &halt); err != nil {
		t.Fatal(err)
	}
	if !halt.IsMajor() || halt.Name() != "26.7" || halt.Version != "" {
		t.Errorf("series halt = %+v", halt)
	}
	out, _ = json.Marshal(halt)
	if strings.Contains(string(out), `"version"`) {
		t.Errorf("a halt of a major upgrade must carry no version: %s", out)
	}
	if rel := (FirmwareHalt{Version: "26.7.5", Kind: "minor"}); rel.IsMajor() || rel.Name() != "26.7.5" {
		t.Errorf("a release halt = %+v", rel)
	}
	// Whether a halt is a major upgrade's and what it is called come from the same
	// field, whatever the row's kind says, so a printed subject never pairs one
	// with the other's name.
	for _, h := range []FirmwareHalt{
		{Version: "26.7.5", Kind: "major"},
		{Series: "26.7", Kind: "minor"},
		{Series: "26.7"},
		{Version: "26.7.5"},
		{Kind: "major"},
	} {
		if h.IsMajor() != (h.Name() == h.Series && h.Series != "") {
			t.Errorf("IsMajor() = %t but Name() = %q for %+v", h.IsMajor(), h.Name(), h)
		}
	}
	if p := (FirmwarePromotion{Series: "26.7"}); !p.IsMajor() || p.Name() != "26.7" {
		t.Errorf("series promotion = %+v", p)
	}
	if c := (FirmwareFailuresCleared{Version: "26.7.5"}); c.IsMajor() || c.Name() != "26.7.5" {
		t.Errorf("release clear = %+v", c)
	}

	// An older board has none of the three keys, and a rollout encodes without them.
	var board FirmwareRollout
	if err := json.Unmarshal([]byte(`{"organization_name": "acme", "status": "ENABLED", "plan_enabled": true, "tracks": [], "failures": [], "halts": []}`), &board); err != nil {
		t.Fatal(err)
	}
	out, _ = json.Marshal(board)
	for _, key := range []string{"majors", "major_failures", "agent_repo"} {
		if strings.Contains(string(out), key) {
			t.Errorf("a board from a control plane that predates major upgrades must stay as it was; it grew %q: %s", key, out)
		}
	}
	// A current control plane's empty lists stay empty lists, not absences.
	var current FirmwareRollout
	if err := json.Unmarshal([]byte(`{"organization_name": "acme", "status": "ENABLED", "plan_enabled": true, "tracks": [], "failures": [], "halts": [],
	  "majors": [], "major_failures": [], "agent_repo": {"configured": false, "channel": null, "trees": []}}`), &current); err != nil {
		t.Fatal(err)
	}
	out, _ = json.Marshal(current)
	for _, want := range []string{`"majors":[]`, `"major_failures":[]`, `"agent_repo":{"configured":false,"trees":[]}`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("a board from a control plane that has no major upgrade to show must say so: missing %s in %s", want, out)
		}
	}
	var current2 FirmwareDevice
	if err := json.Unmarshal([]byte(`{"device_name": "fw-a", "device_uuid": "u", "device_status": "ENABLED", "track": "fast", "notes": []}`), &current2); err != nil {
		t.Fatal(err)
	}
	out, _ = json.Marshal(current2)
	if !strings.Contains(string(out), `"notes":[]`) {
		t.Errorf("a device row's empty notes list must stay an empty list: %s", out)
	}
	// A device row of such a control plane likewise.
	var oldDevice FirmwareDevice
	if err := json.Unmarshal([]byte(`{"device_name": "fw-a", "device_uuid": "u", "device_status": "ENABLED", "track": "fast"}`), &oldDevice); err != nil {
		t.Fatal(err)
	}
	out, _ = json.Marshal(oldDevice)
	for _, key := range []string{"series", "major", "boot_environment", "notes"} {
		if strings.Contains(string(out), `"`+key+`"`) {
			t.Errorf("a device row from a control plane that predates major upgrades grew %q: %s", key, out)
		}
	}
}

// ── the FIRMWARE_TRACK task result ───────────────────────────────────────────

// These messages are valid vectors from NDDataModels v1.42.0's
// tests/fixtures/firmware-track/results.json (the minor ones are 7 of its valid
// ones; the major ones are listed below), copied as literals.
func TestParseFirmwareTrackResultReadsTheContractVectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want FirmwareTrackResult
	}{
		{
			"enroll pinned at the installed release",
			`{"action": "enroll", "track": "fast", "outcome": "success", "reason": "enrolled", "drift": false, "reinstall_entries": 0, "from_version": "26.7.3", "to_version": "26.7.3", "pinned_version": "26.7.3"}`,
			FirmwareTrackResult{Action: "enroll", Track: "fast", Outcome: "success", Reason: "enrolled", FromVersion: "26.7.3", ToVersion: "26.7.3", PinnedVersion: "26.7.3"},
		},
		{
			"enroll held on a custom flavour",
			`{"action": "enroll", "track": "fast", "outcome": "held", "reason": "held_custom_flavour", "drift": false, "reinstall_entries": 0, "from_version": "26.7.3", "to_version": "26.7.3"}`,
			FirmwareTrackResult{Action: "enroll", Track: "fast", Outcome: "held", Reason: "held_custom_flavour", FromVersion: "26.7.3", ToVersion: "26.7.3"},
		},
		{
			"enroll with a same-version reinstall entry",
			`{"action": "enroll", "track": "regular", "outcome": "success", "reason": "enrolled", "drift": false, "reinstall_entries": 1, "from_version": "26.7.3", "to_version": "26.7.3", "pinned_version": "26.7.3"}`,
			FirmwareTrackResult{Action: "enroll", Track: "regular", Outcome: "success", Reason: "enrolled", ReinstallEntries: 1, FromVersion: "26.7.3", ToVersion: "26.7.3", PinnedVersion: "26.7.3"},
		},
		{
			"advance applied",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.7.3", "to_version": "26.7.5", "target_version": "26.7.5", "pinned_version": "26.7.5"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.7.3", ToVersion: "26.7.5", TargetVersion: "26.7.5", PinnedVersion: "26.7.5"},
		},
		{
			"advance noop after a pin drift",
			`{"action": "advance", "track": "regular", "outcome": "noop", "reason": "pin_drift", "drift": true, "reinstall_entries": 0, "from_version": "26.7.5", "to_version": "26.7.5", "target_version": "26.7.3", "pinned_version": "26.7.5"}`,
			FirmwareTrackResult{Action: "advance", Track: "regular", Outcome: "noop", Reason: "pin_drift", Drift: true, FromVersion: "26.7.5", ToVersion: "26.7.5", TargetVersion: "26.7.3", PinnedVersion: "26.7.5"},
		},
		{
			"advance released for a missing release folder",
			`{"action": "advance", "track": "fast", "outcome": "released", "reason": "release_folder_unavailable", "drift": false, "reinstall_entries": 0, "from_version": "26.7.3", "to_version": "26.7.3", "target_version": "26.7.5"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "released", Reason: "release_folder_unavailable", FromVersion: "26.7.3", ToVersion: "26.7.3", TargetVersion: "26.7.5"},
		},
		{
			"advance held in agent test mode",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_test_mode", "drift": false, "reinstall_entries": 0, "from_version": "26.7.3", "to_version": "26.7.3", "target_version": "26.7.5", "pinned_version": "26.7.3"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_test_mode", FromVersion: "26.7.3", ToVersion: "26.7.3", TargetVersion: "26.7.5", PinnedVersion: "26.7.3"},
		},
		{
			"major: the upgrade landed and a boot environment was made",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major", "notes": ["boot_environment_made"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major", Notes: []string{"boot_environment_made"}},
		},
		{
			"major: the upgrade landed, nothing to note",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major"},
		},
		{
			"major: the upgrade landed with a third-party repository and package on the box",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major", "notes": ["boot_environment_made", "third_party_repository", "third_party_package"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major", Notes: []string{"boot_environment_made", "third_party_repository", "third_party_package"}},
		},
		{
			"major: the upgrade landed, the pool had no room for a boot environment",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major", "notes": ["boot_environment_skipped_no_room"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major", Notes: []string{"boot_environment_skipped_no_room"}},
		},
		{
			"major: the upgrade landed, the device's boot environment setting is off",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major", "notes": ["boot_environment_off"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major", Notes: []string{"boot_environment_off"}},
		},
		{
			"major: the upgrade landed on a box without ZFS",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "pinned_version": "26.7.1_1", "kind": "major", "notes": ["boot_environment_unavailable"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", PinnedVersion: "26.7.1_1", Kind: "major", Notes: []string{"boot_environment_unavailable"}},
		},
		{
			"major: the upgrade landed with the agent's record gone: a success with drift and no pin",
			`{"action": "advance", "track": "fast", "outcome": "success", "reason": "applied", "drift": true, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.7.1_1", "target_version": "26.7", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "success", Reason: "applied", Drift: true, FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", TargetVersion: "26.7", Kind: "major"},
		},
		{
			"major: no-op, the device's mirror does not offer the series yet",
			`{"action": "advance", "track": "fast", "outcome": "noop", "reason": "target_not_yet_offered", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "noop", Reason: "target_not_yet_offered", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major"},
		},
		{
			"major: no-op, the series is not offered yet, with a third-party package noted",
			`{"action": "advance", "track": "fast", "outcome": "noop", "reason": "target_not_yet_offered", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["third_party_package"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "noop", Reason: "target_not_yet_offered", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"third_party_package"}},
		},
		{
			"major: no-op, the device is not at the final build and the forced check shows no offer",
			`{"action": "advance", "track": "fast", "outcome": "noop", "reason": "target_not_yet_offered", "drift": false, "reinstall_entries": 0, "from_version": "26.1.9", "to_version": "26.1.9", "target_version": "26.7", "pinned_version": "26.1.9", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "noop", Reason: "target_not_yet_offered", FromVersion: "26.1.9", ToVersion: "26.1.9", TargetVersion: "26.7", PinnedVersion: "26.1.9", Kind: "major"},
		},
		{
			"major: held, not enough free disk, with a third-party repository noted",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_insufficient_space", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["third_party_repository"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_insufficient_space", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"third_party_repository"}},
		},
		{
			"major: held, not enough free disk, nothing noted",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_insufficient_space", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_insufficient_space", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major"},
		},
		{
			"major: held, the device is a member of an HA pair",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_ha_member", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_ha_member", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major"},
		},
		{
			"major: held, the NetDefense repository has no tree for the new ABI",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_agent_abi_unavailable", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["third_party_repository"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_agent_abi_unavailable", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"third_party_repository"}},
		},
		{
			"major: held, two earlier attempts aborted and the back-off answers without touching the box",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_upgrade_aborted", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_upgrade_aborted", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major"},
		},
		{
			"major: held, the first stage aborted after a boot environment was made",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_upgrade_aborted", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["boot_environment_made"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_upgrade_aborted", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"boot_environment_made"}},
		},
		{
			"major: held, the offer is not the one the step expects",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_unexpected_plan", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["boot_environment_off"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_unexpected_plan", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"boot_environment_off"}},
		},
		{
			"major: held, the firmware check failed",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_check_failed", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major"}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_check_failed", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major"},
		},
		{
			"major: held, the waits before the request used the time the upgrade needs",
			`{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_late_dispatch", "drift": false, "reinstall_entries": 0, "from_version": "26.1.11_10", "to_version": "26.1.11_10", "target_version": "26.7", "pinned_version": "26.1.11_10", "kind": "major", "notes": ["boot_environment_off"]}`,
			FirmwareTrackResult{Action: "advance", Track: "fast", Outcome: "held", Reason: "held_late_dispatch", FromVersion: "26.1.11_10", ToVersion: "26.1.11_10", TargetVersion: "26.7", PinnedVersion: "26.1.11_10", Kind: "major", Notes: []string{"boot_environment_off"}},
		},
		{
			"the largest valid result of a major upgrade: every key at its limit",
			`{"action": "advance", "track": "regular", "outcome": "success", "reason": "applied", "drift": true, "reinstall_entries": 100000, "from_version": "99.99.99_999", "to_version": "99.99.99_999", "target_version": "26.7", "pinned_version": "99.99.99_999", "kind": "major", "notes": ["boot_environment_made", "boot_environment_skipped_no_room", "boot_environment_off", "boot_environment_unavailable", "third_party_repository", "third_party_package"]}`,
			FirmwareTrackResult{Action: "advance", Track: "regular", Outcome: "success", Reason: "applied", Drift: true, ReinstallEntries: 100000, FromVersion: "99.99.99_999", ToVersion: "99.99.99_999", TargetVersion: "26.7", PinnedVersion: "99.99.99_999", Kind: "major", Notes: []string{"boot_environment_made", "boot_environment_skipped_no_room", "boot_environment_off", "boot_environment_unavailable", "third_party_repository", "third_party_package"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseFirmwareTrackResult(tc.raw)
			if got == nil {
				t.Fatalf("not read as a result: %s", tc.raw)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", *got, tc.want)
			}
		})
	}
}

// A message that is not a result is nil, so it renders as the message it is.
func TestParseFirmwareTrackResultLeavesOtherMessagesAlone(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":                               "",
		"the sentence a pending task carries": "Firmware track fast: advance",
		"the agent's plain-text failure":      "firmware check failed: connection refused",
		"a FIRMWARE_UPGRADE block":            `{"resolved_mode": "minor", "from_version": "26.7.3", "to_version": "26.7.5", "applied": true}`,
		"a SYNC envelope":                     `{"message": "Aliases +2", "results": [{"type": "alias", "name": "x", "status": "success"}]}`,
		"json that is not an object":          `["advance", "success"]`,
		"an object with no action":            `{"outcome": "success", "reason": "applied"}`,
		"an object with no outcome":           `{"action": "advance", "reason": "applied"}`,
		"truncated json":                      `{"action": "advance", "outcome": "succ`,
		"more than a result can hold":         `{"action": "advance", "outcome": "success", "reason": "` + strings.Repeat("x", 17<<10) + `"}`,
	} {
		if got := ParseFirmwareTrackResult(raw); got != nil {
			t.Errorf("%s: read %+v as a result", name, *got)
		}
	}
}

// Task.Message is JSON inside a string: the Task decode sanitized the outer
// string, where a \u escape was still literal text, and this second decode is
// what turns it into a live control byte.
func TestParseFirmwareTrackResultSanitizesTheInnerDecode(t *testing.T) {
	const esc = `\u001b`
	raw := `{"action": "advance", "track": "fast", "outcome": "held", "reason": "held_` + esc + `[2Jfirmware_busy",` +
		` "drift": false, "reinstall_entries": 0, "from_version": "26.7` + esc + `]0;pwn\u0007.3"}`
	got := ParseFirmwareTrackResult(raw)
	if got == nil {
		t.Fatal("not read as a result")
	}
	for _, field := range []string{got.Reason, got.FromVersion, got.Action, got.Track, got.Outcome} {
		for _, r := range field {
			if r < 0x20 && r != '\n' && r != '\t' && r != '\r' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("a control character survived in %q", field)
			}
		}
	}
	if got.Reason != "held_[2Jfirmware_busy" {
		t.Errorf("reason = %q, want the escape stripped and the rest kept", got.Reason)
	}
}
