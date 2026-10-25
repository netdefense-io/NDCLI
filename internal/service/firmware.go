package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// Firmware tracks: staged OPNsense updates. Every method here is one of
// NDManager's /organizations/{org}/firmware routes; cli/firmware.go and
// internal/mcp/tools_firmware.go are thin shells over them.
//
// Inputs that name a track are normalized here, once, so the CLI and the MCP
// tools cannot diverge: the track is trimmed and lower-cased (the server
// accepts any letter case but answers in lowercase), and a value that is not
// one of the fixed names is refused before anything is sent. The release names
// are NOT validated: NDManager owns the rule for what a release directory is
// called and answers a malformed one with its own message.

// NDManager's error codes that a firmware front-end advises on.
const (
	apiCodeFirmwareNotConfigured = "FIRMWARE_NOT_CONFIGURED"
	apiCodeFirmwareDisabled      = "FIRMWARE_DISABLED"
	apiCodeFirmwareDeviceManaged = "FIRMWARE_DEVICE_MANAGED"
	apiCodeFirmwareSpecManaged   = "FIRMWARE_SPEC_MANAGED"
	apiCodeFirmwareDeviceLeaving = "FIRMWARE_DEVICE_LEAVING"
	// A promote of a major upgrade whose OPNsense upgrade path is closed.
	apiCodeFirmwareUpgradeNotOpen = "FIRMWARE_UPGRADE_NOT_OPEN"
	// A boot-environment setting sent for devices that stand in no track.
	apiCodeFirmwareDeviceUnmanaged = "FIRMWARE_DEVICE_UNMANAGED"
	// A track update that names no field it knows.
	apiCodeMissingField = "MISSING_FIELD"
)

// The page size NDManager's firmware lists default to.
const firmwareDefaultPerPage = 50

// firmwarePath builds /api/v1/organizations/{org}/firmware/{segments...}, each
// piece path-escaped: a device name or a release is user input.
func firmwarePath(org string, segments ...string) string {
	var b strings.Builder
	b.WriteString("/api/v1/organizations/")
	b.WriteString(url.PathEscape(org))
	b.WriteString("/firmware")
	for _, seg := range segments {
		b.WriteString("/")
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

// decodeFirmware turns the outcome of a request into the model it answers
// with. The decode goes through api.ParseResponse, which caps the body and
// scrubs terminal control sequences from every string in it.
func decodeFirmware[T any](resp *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var out T
	if err := api.ParseResponse(resp, &out); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &out, nil
}

// NormalizeFirmwareChoice trims and lower-cases value and checks it against
// valid, returning the word the server takes. field names the input in the
// message, which is surface-neutral: it says "track", never "--track". The
// methods below call it on their own; a front-end calls it to refuse a typo
// before it asks for a confirmation.
func NormalizeFirmwareChoice(field, value string, valid []string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return "", &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("%s is required — one of: %s", field, strings.Join(valid, ", ")),
		}
	}
	if !slices.Contains(valid, v) {
		return "", &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("unknown %s %q — valid values: %s", field, strings.TrimSpace(value), strings.Join(valid, ", ")),
		}
	}
	return v, nil
}

// requireFirmwareText trims value and refuses it when nothing is left.
func requireFirmwareText(field, value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", &Error{Code: CodeInvalidInput, Message: field + " is required"}
	}
	return v, nil
}

func firmwarePaging(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = firmwareDefaultPerPage
	}
	return page, perPage
}

func pagingParams(page, perPage int) map[string]string {
	return map[string]string{"page": strconv.Itoa(page), "per_page": strconv.Itoa(perPage)}
}

// ── configuration ───────────────────────────────────────────────────────────

// FirmwareConfigGet returns the organization's firmware configuration with its
// three tracks. A 404 FIRMWARE_NOT_CONFIGURED is preserved for errors.As.
func (s *Service) FirmwareConfigGet(ctx context.Context, org string) (*models.FirmwareConfig, error) {
	return decodeFirmware[models.FirmwareConfig](s.api.Get(ctx, firmwarePath(org, "config"), nil))
}

// FirmwareConfigCreateOpts are the inputs of creating the configuration.
type FirmwareConfigCreateOpts struct {
	// NoReboot makes the advance steps apply the userland packages only and
	// not reboot. The zero value is the default: the full update, base and
	// kernel included, then a reboot.
	NoReboot bool
}

// FirmwareConfigCreate enables Firmware for the organization. It creates the
// three tracks at their default settings; no device is in a track and no track
// has a schedule yet.
func (s *Service) FirmwareConfigCreate(ctx context.Context, org string, opts FirmwareConfigCreateOpts) (*models.FirmwareConfig, error) {
	return decodeFirmware[models.FirmwareConfig](
		s.api.Post(ctx, firmwarePath(org, "config"), map[string]bool{"reboot": !opts.NoReboot}))
}

// FirmwareConfigUpdate changes the default reboot flag of the advance steps.
func (s *Service) FirmwareConfigUpdate(ctx context.Context, org string, reboot bool) (*models.FirmwareConfig, error) {
	return decodeFirmware[models.FirmwareConfig](
		s.api.Put(ctx, firmwarePath(org, "config"), map[string]bool{"reboot": reboot}))
}

// FirmwareConfigSetStatus enables or disables Firmware. Disabling releases
// every device in a track: each starts leaving it, and its agent restores the
// setting it had before joining. Enabling brings no device back.
func (s *Service) FirmwareConfigSetStatus(ctx context.Context, org string, enabled bool) (*models.FirmwareConfig, error) {
	status := models.FirmwareConfigDisabled
	if enabled {
		status = models.FirmwareConfigEnabled
	}
	return decodeFirmware[models.FirmwareConfig](
		s.api.Put(ctx, firmwarePath(org, "config", "status"), map[string]string{"status": status}))
}

// FirmwareConfigDelete deletes the configuration, the tracks, their schedules
// and the approvals. NDManager refuses (409 HAS_DEPENDENCIES) while any device
// is in a track or still leaving one; the refusal lists them in
// APIError.BlockingResources.
func (s *Service) FirmwareConfigDelete(ctx context.Context, org string) (*models.FirmwareConfigDeleted, error) {
	out, err := decodeFirmware[models.FirmwareConfigDeleted](s.api.Delete(ctx, firmwarePath(org, "config")))
	if err != nil {
		return nil, err
	}
	out.Organization = org
	return out, nil
}

// ── tracks ──────────────────────────────────────────────────────────────────

// FirmwareTrackList returns the three tracks, in conveyor order, as the
// configuration reports them.
func (s *Service) FirmwareTrackList(ctx context.Context, org string) ([]models.FirmwareTrack, error) {
	cfg, err := s.FirmwareConfigGet(ctx, org)
	if err != nil {
		return nil, err
	}
	return cfg.Tracks, nil
}

// FirmwareTrackGet returns one track's settings, schedule and device counts.
func (s *Service) FirmwareTrackGet(ctx context.Context, org, track string) (*models.FirmwareTrack, error) {
	track, err := NormalizeFirmwareChoice("track", track, models.FirmwareTracks)
	if err != nil {
		return nil, err
	}
	return decodeFirmware[models.FirmwareTrack](s.api.Get(ctx, firmwarePath(org, "tracks", track), nil))
}

// FirmwareTrackUpdateOpts are the settings of a track to change; nil leaves a
// setting as it is.
type FirmwareTrackUpdateOpts struct {
	// SoakDays is the days a release soaks before the track takes it: counted
	// from OPNsense publishing it (fast) or from the previous track's evidence
	// being met (regular, stable).
	SoakDays *int
	// MinSuccess is how many distinct devices of the previous track must have
	// reached a release before this track takes it. The fast track has no
	// previous track and takes 0 only; NDManager enforces that and the upper
	// bounds of both.
	MinSuccess *int
	// AutoApplyMajor turns the track's major upgrades on or off: whether its
	// windows move its devices to the next OPNsense series. Off by default; the
	// series is approved through the conveyor either way, and off means no step
	// asks a device to upgrade. Switching it off also cancels the major-upgrade
	// steps not yet dispatched; one already running goes on. It is sent on its
	// own (see FirmwareTrackUpdate).
	AutoApplyMajor *bool
}

// FirmwareTrackUpdate changes a track's soak_days and/or min_success, or its
// auto_apply_major switch. Releases the track already approved stay approved;
// the new values apply to the decisions still to come.
//
// The switch is changed on its own: an NDManager that predates major upgrades
// ignores a field it does not know, so beside soak_days it would apply those and
// drop the switch without a word. Alone, it sees an empty update and answers 400
// MISSING_FIELD, which is reported as an older NDManager; an answer that does
// not echo the switch is the second guard.
func (s *Service) FirmwareTrackUpdate(ctx context.Context, org, track string, opts FirmwareTrackUpdateOpts) (*models.FirmwareTrack, error) {
	track, body, err := CheckFirmwareTrackUpdate(track, opts)
	if err != nil {
		return nil, err
	}
	out, err := decodeFirmware[models.FirmwareTrack](s.api.Put(ctx, firmwarePath(org, "tracks", track), body))
	if err != nil {
		if opts.AutoApplyMajor != nil && predatesAutoApplyMajor(err) {
			return nil, &Error{Code: CodeServerTooOld, Err: err,
				Message: "this NDManager predates major upgrades: it does not know auto_apply_major and refused the request as empty, so the switch was not changed"}
		}
		return nil, err
	}
	if opts.AutoApplyMajor != nil {
		if err := checkAutoApplyMajorEcho(out, *opts.AutoApplyMajor); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CheckFirmwareTrackUpdate validates a track update and returns the track as
// the server takes it and the body that would be sent. FirmwareTrackUpdate calls
// it on its own; a front-end calls it to refuse a bad request before it asks for
// the confirmation that turning major upgrades on needs.
func CheckFirmwareTrackUpdate(track string, opts FirmwareTrackUpdateOpts) (string, map[string]interface{}, error) {
	track, err := NormalizeFirmwareChoice("track", track, models.FirmwareTracks)
	if err != nil {
		return "", nil, err
	}
	body := map[string]interface{}{}
	if opts.SoakDays != nil {
		if *opts.SoakDays < 0 {
			return "", nil, &Error{Code: CodeInvalidInput, Message: "soak_days cannot be negative"}
		}
		body["soak_days"] = *opts.SoakDays
	}
	if opts.MinSuccess != nil {
		if *opts.MinSuccess < 0 {
			return "", nil, &Error{Code: CodeInvalidInput, Message: "min_success cannot be negative"}
		}
		body["min_success"] = *opts.MinSuccess
	}
	if opts.AutoApplyMajor != nil {
		if len(body) > 0 {
			return "", nil, &Error{Code: CodeInvalidInput,
				Message: "auto_apply_major is changed on its own: change soak_days and min_success in a separate request"}
		}
		body["auto_apply_major"] = *opts.AutoApplyMajor
	}
	if len(body) == 0 {
		return "", nil, &Error{Code: CodeInvalidInput, Message: "provide soak_days and/or min_success, or auto_apply_major"}
	}
	return track, body, nil
}

// predatesAutoApplyMajor recognises the answer an NDManager that predates major
// upgrades gives to the switch sent alone: it ignores the key, sees an update
// that names no field, and answers 400 MISSING_FIELD.
func predatesAutoApplyMajor(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == apiCodeMissingField
}

// checkAutoApplyMajorEcho refuses an answer to a switch write that does not
// show the value that was asked for. A missing field is an NDManager that
// predates major upgrades, which ignored the key; a different value is a write
// that did not happen.
func checkAutoApplyMajorEcho(track *models.FirmwareTrack, want bool) error {
	switch {
	case track.AutoApplyMajor == nil:
		return &Error{Code: CodeServerTooOld, Message: "this NDManager predates major upgrades: its answer does not carry auto_apply_major, so the switch was not changed"}
	case *track.AutoApplyMajor != want:
		return &Error{Code: CodeAPIError, Message: fmt.Sprintf(
			"NDManager answered auto_apply_major=%t to a request for %t: the switch was not changed", *track.AutoApplyMajor, want)}
	}
	return nil
}

// FirmwareTrackSetSchedule attaches the track's windows to the named schedule,
// moves them to another, or detaches them. schedule nil detaches; a non-nil
// value must name a schedule, so an empty string is refused rather than read as
// a detach. Tracks may share one schedule.
func (s *Service) FirmwareTrackSetSchedule(ctx context.Context, org, track string, schedule *string) (*models.FirmwareTrack, error) {
	track, err := NormalizeFirmwareChoice("track", track, models.FirmwareTracks)
	if err != nil {
		return nil, err
	}
	var name interface{}
	if schedule != nil {
		trimmed, err := requireFirmwareText("schedule", *schedule)
		if err != nil {
			return nil, err
		}
		name = trimmed
	}
	return decodeFirmware[models.FirmwareTrack](
		s.api.Put(ctx, firmwarePath(org, "tracks", track, "schedule"), map[string]interface{}{"schedule": name}))
}

// ── devices ─────────────────────────────────────────────────────────────────

// FirmwareDeviceAssignOpts say what to do with the named devices: move them to
// a track, set their boot-environment setting, or both.
type FirmwareDeviceAssignOpts struct {
	// Track is where to move the devices: fast, regular, stable, or unmanaged to
	// release them. Empty leaves their membership alone, which needs
	// BootEnvironment.
	Track string
	// BootEnvironment sets whether the agent makes a ZFS boot environment before
	// a major upgrade of the devices (on by default; ZFS only, best effort).
	// The setting belongs to a device's place in a track: it is refused for a
	// device in no track (FIRMWARE_DEVICE_UNMANAGED) unless Track puts it in one
	// in the same request, and it is not accepted with Track unmanaged, because a
	// device that leaves its track keeps none. Changing it cancels the series
	// upgrade steps of those devices that were not yet dispatched.
	BootEnvironment *bool
}

// FirmwareDeviceAssign moves explicitly named devices into a track, or back to
// unmanaged, and/or sets their boot-environment setting. There is no OU and no
// pattern here: a track is chosen device by device. It is all or nothing for the
// names (an unknown device refuses the whole request). A device not in a track
// joins as enrolling and its agent pins it at the installed release; moving it
// between tracks needs no task; moving it to unmanaged makes it leave the track.
//
// A request that carries the setting has the answer checked: every result must
// echo it. An NDManager that predates the setting refuses the setting sent
// alone (it requires a track) or ignores it beside a track and omits the echo;
// both are reported as that, and a different value as a write that did not
// happen. Send the setting alone when only the setting is meant to change.
func (s *Service) FirmwareDeviceAssign(ctx context.Context, org string, devices []string, opts FirmwareDeviceAssignOpts) (*models.FirmwareAssignment, error) {
	names, track, err := CheckFirmwareDeviceAssign(devices, opts)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{"devices": names}
	if track != "" {
		body["track"] = track
	}
	if opts.BootEnvironment != nil {
		body["boot_environment"] = *opts.BootEnvironment
	}
	out, err := decodeFirmware[models.FirmwareAssignment](s.api.Put(ctx, firmwarePath(org, "devices"), body))
	if opts.BootEnvironment == nil {
		return out, err
	}
	if err != nil {
		if track == "" && predatesBootEnvironment(err) {
			return nil, &Error{Code: CodeServerTooOld, Err: err,
				Message: "this NDManager predates the boot-environment setting: it requires a track and refused the request"}
		}
		return nil, err
	}
	if err := checkBootEnvironmentEcho(out, *opts.BootEnvironment, track); err != nil {
		return nil, err
	}
	return out, nil
}

// CheckFirmwareDeviceAssign validates a device assignment and returns the
// device names (trimmed, without blanks and repeats) and the track as the server
// takes it (empty when the request carries none). FirmwareDeviceAssign calls it
// on its own; a front-end calls it to refuse a request that cannot run before it
// previews one.
func CheckFirmwareDeviceAssign(devices []string, opts FirmwareDeviceAssignOpts) ([]string, string, error) {
	track := strings.TrimSpace(opts.Track)
	if track == "" && opts.BootEnvironment == nil {
		return nil, "", &Error{Code: CodeInvalidInput, Message: "give a track, a boot_environment setting, or both"}
	}
	if track != "" {
		var err error
		if track, err = NormalizeFirmwareChoice("track", track, models.FirmwareAssignTracks); err != nil {
			return nil, "", err
		}
	}
	if track == models.FirmwareUnmanaged && opts.BootEnvironment != nil {
		return nil, "", &Error{Code: CodeInvalidInput,
			Message: "boot_environment cannot be set while moving devices to unmanaged: a device that leaves its track keeps no setting"}
	}
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		d = strings.TrimSpace(d)
		if d != "" && !slices.Contains(names, d) {
			names = append(names, d)
		}
	}
	if len(names) == 0 {
		return nil, "", &Error{Code: CodeInvalidInput, Message: "at least one device name is required"}
	}
	return names, track, nil
}

// predatesBootEnvironment recognises the answer an NDManager that predates the
// boot-environment setting gives to it sent alone: 422, the validator's own
// body (no code, so there is no code to match), saying the track is required.
func predatesBootEnvironment(err error) bool {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity || apiErr.Code != "" {
		return false
	}
	message := strings.ToLower(apiErr.Message)
	return strings.Contains(message, "track") && strings.Contains(message, "required")
}

// checkBootEnvironmentEcho holds the answer to a request that carried the
// setting to it: each result must report the setting after the call, and it
// must be the one asked for. track is the track the request carried, if any:
// beside a track, an older NDManager moves the devices and drops the setting,
// and the message says so. A current NDManager gives every device the setting:
// a device in no track refuses the whole request, and the setting is never sent
// with unmanaged, so a missing echo is an older NDManager, not a device without
// a track.
func checkBootEnvironmentEcho(a *models.FirmwareAssignment, want bool, track string) error {
	var missing, wrong []string
	for _, r := range a.Results {
		switch {
		case r.BootEnvironment == nil:
			missing = append(missing, r.DeviceName)
		case *r.BootEnvironment != want:
			wrong = append(wrong, r.DeviceName)
		}
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("this NDManager predates the boot-environment setting: its answer does not carry boot_environment for %s, so the setting was not changed",
			strings.Join(missing, ", "))
		if track != "" {
			msg += fmt.Sprintf(" (the move to %s itself was made)", track)
		}
		return &Error{Code: CodeServerTooOld, Message: msg}
	}
	if len(wrong) > 0 {
		return &Error{Code: CodeAPIError, Message: fmt.Sprintf(
			"NDManager reports a different boot_environment than the requested %t for %s: the setting was not changed",
			want, strings.Join(wrong, ", "))}
	}
	return nil
}

// FirmwareDeviceListOpts are the filters of the per-device firmware status.
type FirmwareDeviceListOpts struct {
	// Track is fast, regular, stable or unmanaged; empty for every device.
	Track string
	// State is enrolling, managed, held or unenrolling; empty for any.
	State string
	// Name matches device names containing this text.
	Name    string
	Page    int
	PerPage int
}

// FirmwareDeviceList returns every device of the organization and where it
// stands: its track, state, pinned release, the reason of the agent's last
// hold, the last step's result and why no step is being sent.
func (s *Service) FirmwareDeviceList(ctx context.Context, org string, opts FirmwareDeviceListOpts) (*models.FirmwareDeviceList, error) {
	params := map[string]string{}
	if strings.TrimSpace(opts.Track) != "" {
		track, err := NormalizeFirmwareChoice("track", opts.Track, models.FirmwareAssignTracks)
		if err != nil {
			return nil, err
		}
		params["track"] = track
	}
	if strings.TrimSpace(opts.State) != "" {
		state, err := NormalizeFirmwareChoice("state", opts.State, models.FirmwareStates)
		if err != nil {
			return nil, err
		}
		params["state"] = state
	}
	if name := strings.TrimSpace(opts.Name); name != "" {
		params["name"] = name
	}
	page, perPage := firmwarePaging(opts.Page, opts.PerPage)
	for k, v := range pagingParams(page, perPage) {
		params[k] = v
	}
	return decodeFirmware[models.FirmwareDeviceList](s.api.Get(ctx, firmwarePath(org, "devices"), params))
}

// FirmwareHistory returns a device's firmware ledger, newest first: every
// finished step with its outcome, reason and the releases it moved between.
func (s *Service) FirmwareHistory(ctx context.Context, org, device string, page, perPage int) (*models.FirmwareHistory, error) {
	device, err := requireFirmwareText("device name", device)
	if err != nil {
		return nil, err
	}
	page, perPage = firmwarePaging(page, perPage)
	return decodeFirmware[models.FirmwareHistory](
		s.api.Get(ctx, firmwarePath(org, "devices", device, "history"), pagingParams(page, perPage)))
}

// ── rollout ─────────────────────────────────────────────────────────────────

// FirmwareRollout returns where every release stands on the conveyor, per track
// and series: the target, the next candidate with the soak left (computed by
// the server), the evidence reached against the one required and what has
// stalled, plus the failures that block a release and the active halts.
func (s *Service) FirmwareRollout(ctx context.Context, org string) (*models.FirmwareRollout, error) {
	return decodeFirmware[models.FirmwareRollout](s.api.Get(ctx, firmwarePath(org, "status"), nil))
}

// FirmwareHaltListOpts selects which halts to list.
type FirmwareHaltListOpts struct {
	// IncludeLifted adds the halts that have been lifted; by default only the
	// ones in force are listed.
	IncludeLifted bool
	// Kind is minor (the halts of releases), major (those of major upgrades) or
	// all; empty leaves it to the server, which lists releases only. A row of a
	// major upgrade carries its series and no version.
	Kind    string
	Page    int
	PerPage int
}

// FirmwareHaltList returns the halts of the organization, newest first.
func (s *Service) FirmwareHaltList(ctx context.Context, org string, opts FirmwareHaltListOpts) (*models.FirmwareHaltList, error) {
	page, perPage := firmwarePaging(opts.Page, opts.PerPage)
	params := pagingParams(page, perPage)
	params["active"] = strconv.FormatBool(!opts.IncludeLifted)
	kind := ""
	if strings.TrimSpace(opts.Kind) != "" {
		var err error
		if kind, err = NormalizeFirmwareChoice("kind", opts.Kind, models.FirmwareHaltKinds); err != nil {
			return nil, err
		}
		params["kind"] = kind
	}
	list, err := decodeFirmware[models.FirmwareHaltList](s.api.Get(ctx, firmwarePath(org, "halts"), params))
	if err != nil {
		return nil, err
	}
	keepHaltsOfKind(list, kind)
	return list, nil
}

// keepHaltsOfKind drops the rows that are not of the kind asked for (no kind, or
// all, keeps every row). A current NDManager sends none to drop. One that
// predates major upgrades ignores the kind parameter and sends its release halts,
// which a request for major upgrades, or the completion of an unhalt of one, must
// not show; a row with no kind is a release's. Dropped rows mean the server
// ignored the parameter, so its paging described another list: what is left is
// one page, the first.
func keepHaltsOfKind(list *models.FirmwareHaltList, kind string) {
	if kind == "" || kind == models.FirmwareKindAll {
		return
	}
	kept := make([]models.FirmwareHalt, 0, len(list.Items))
	for _, h := range list.Items {
		rowKind := h.Kind
		if rowKind == "" {
			rowKind = models.FirmwareKindMinor
		}
		if rowKind == kind {
			kept = append(kept, h)
		}
	}
	if len(kept) != len(list.Items) {
		list.Items, list.Total, list.Page, list.Pages = kept, len(kept), 1, 1
	}
}

// firmwareTarget is what a halt, an unhalt, a promote or a failure clear
// addresses, and the wire shape that follows from it. A release is addressed by
// version. A major upgrade is addressed by series, never by a flag beside a
// version: 26.7 is the name of a series and of its GA release, and an NDManager
// that predates major upgrades would act on the release if it were given a
// version and ignore a flag. Sent as series, such an NDManager refuses the
// request (422) or matches no route (404).
type firmwareTarget struct {
	major bool
	name  string
}

// resolveFirmwareTarget normalizes the kind (empty is minor) and the name.
func resolveFirmwareTarget(kind, name string) (firmwareTarget, error) {
	if strings.TrimSpace(kind) == "" {
		kind = models.FirmwareKindMinor
	}
	kind, err := NormalizeFirmwareChoice("kind", kind, models.FirmwareKinds)
	if err != nil {
		return firmwareTarget{}, err
	}
	field := "version"
	if kind == models.FirmwareKindMajor {
		field = "series"
	}
	name, err = requireFirmwareText(field, name)
	if err != nil {
		return firmwareTarget{}, err
	}
	return firmwareTarget{major: kind == models.FirmwareKindMajor, name: name}, nil
}

// CheckFirmwareTarget validates the kind (empty is minor) and the name of what a
// halt, an unhalt, a promote or a failure clear addresses, and returns the kind
// as the server takes it and the name trimmed. The methods call it on their own;
// a front-end calls it to refuse a request that cannot run before it previews
// one.
func CheckFirmwareTarget(kind, name string) (string, string, error) {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return "", "", err
	}
	if target.major {
		return models.FirmwareKindMajor, target.name, nil
	}
	return models.FirmwareKindMinor, target.name, nil
}

// checkFirmwareRevision validates a revision named beside a release: 0 is not
// named, else it is a positive number, and a major upgrade has none.
func checkFirmwareRevision(t firmwareTarget, revision int) error {
	if revision == 0 {
		return nil
	}
	if revision < 0 {
		return &Error{Code: CodeInvalidInput, Message: "revision must be a positive number"}
	}
	if t.major {
		return &Error{Code: CodeInvalidInput, Message: "revision applies to a release, not to a major upgrade"}
	}
	return nil
}

// CheckFirmwareRevision is checkFirmwareRevision for a front-end that refuses a
// request before it previews or asks.
func CheckFirmwareRevision(kind, name string, revision int) error {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return err
	}
	return checkFirmwareRevision(target, revision)
}

// field is the body key that names the target.
func (t firmwareTarget) field() string {
	if t.major {
		return "series"
	}
	return "version"
}

// FirmwareHalt halts a release (kind minor, the default) or a major upgrade
// (kind major, named by its target series): no track approves it, and the steps
// queued for it and not yet dispatched are cancelled. Devices already on it, or
// already upgrading, stay; there is no rollback. A release must be in the
// catalog, a series the target of a supported major upgrade.
func (s *Service) FirmwareHalt(ctx context.Context, org, kind, name, reason string, revision int) (*models.FirmwareHaltResult, error) {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return nil, err
	}
	reason, err = requireFirmwareText("reason", reason)
	if err != nil {
		return nil, err
	}
	if err := checkFirmwareRevision(target, revision); err != nil {
		return nil, err
	}
	body := map[string]interface{}{target.field(): target.name, "reason": reason}
	if revision > 0 {
		body["revision"] = revision
	}
	return decodeFirmware[models.FirmwareHaltResult](s.api.Post(ctx, firmwarePath(org, "halts"), body))
}

// FirmwareUnhalt lifts the halt on a release or a major upgrade; it competes
// for the tracks again. A series has a path of its own, never a kind flag on the
// release's.
func (s *Service) FirmwareUnhalt(ctx context.Context, org, kind, name string, revision int) (*models.FirmwareHaltResult, error) {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return nil, err
	}
	if err := checkFirmwareRevision(target, revision); err != nil {
		return nil, err
	}
	if target.major {
		return decodeFirmware[models.FirmwareHaltResult](s.api.Delete(ctx, firmwarePath(org, "halts", "series", target.name)))
	}
	path := firmwarePath(org, "halts", target.name)
	if revision > 0 {
		path += "?" + url.Values{"revision": {strconv.Itoa(revision)}}.Encode()
	}
	return decodeFirmware[models.FirmwareHaltResult](s.api.Delete(ctx, path))
}

// FirmwarePromote approves a release or a major upgrade in one track, or in all
// three at once, without waiting for its soak: the way an urgent security
// release goes out. It is idempotent. A release must be in the catalog and not
// halted; a series must be the target of a supported major upgrade, not
// halted, and its upgrade path open (409 FIRMWARE_UPGRADE_NOT_OPEN otherwise).
// Approving a series starts no upgrade by itself: a track upgrades its devices
// only with auto_apply_major on.
func (s *Service) FirmwarePromote(ctx context.Context, org, kind, name, track string, revision int) (*models.FirmwarePromotion, error) {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return nil, err
	}
	track, err = NormalizeFirmwareChoice("track", track, models.FirmwarePromoteTracks)
	if err != nil {
		return nil, err
	}
	if err := checkFirmwareRevision(target, revision); err != nil {
		return nil, err
	}
	body := map[string]interface{}{target.field(): target.name, "track": track}
	if revision > 0 {
		body["revision"] = revision
	}
	return decodeFirmware[models.FirmwarePromotion](s.api.Post(ctx, firmwarePath(org, "promote"), body))
}

// FirmwareFailuresClear clears the recorded failures that block a release or a
// major upgrade, in one track or (track empty) in every track, so it can move
// on. The records stay in the history.
func (s *Service) FirmwareFailuresClear(ctx context.Context, org, kind, name, track string, revision int) (*models.FirmwareFailuresCleared, error) {
	target, err := resolveFirmwareTarget(kind, name)
	if err != nil {
		return nil, err
	}
	if err := checkFirmwareRevision(target, revision); err != nil {
		return nil, err
	}
	body := map[string]interface{}{target.field(): target.name}
	if revision > 0 {
		body["revision"] = revision
	}
	if strings.TrimSpace(track) != "" {
		track, err = NormalizeFirmwareChoice("track", track, models.FirmwareTracks)
		if err != nil {
			return nil, err
		}
		body["track"] = track
	} else {
		track = ""
	}
	out, err := decodeFirmware[models.FirmwareFailuresCleared](s.api.Post(ctx, firmwarePath(org, "failures", "clear"), body))
	if err != nil {
		return nil, err
	}
	out.Track = track
	if revision > 0 {
		out.Revision = &revision
	}
	return out, nil
}

// ── the way forward from a refusal ──────────────────────────────────────────

// FirmwareVocabulary names, for one front-end, the commands that answer a
// firmware refusal. NDManager's messages say what is wrong; what to run next
// depends on where the user is, an ndcli command line or an MCP tool call, so
// each front-end supplies its own words and the service owns which refusal
// gets which advice.
type FirmwareVocabulary struct {
	// ConfigCreate creates the firmware configuration.
	ConfigCreate string
	// ConfigEnable enables a disabled configuration.
	ConfigEnable string
	// DeviceStatus shows where each device stands in the tracks.
	DeviceStatus string
	// DeviceRelease takes a device out of its track.
	DeviceRelease string
	// TrackSchedule moves a track's windows to another schedule. It carries
	// FirmwareTrackPlaceholder where the track's name goes.
	TrackSchedule string
	// TrackDetach detaches a track from its schedule, which stops its windows
	// and frees the schedule to be deleted. It carries FirmwareTrackPlaceholder
	// where the track's name goes.
	TrackDetach string
	// Rollout shows where every release and major upgrade stands on the board.
	Rollout string
	// DeviceAdd adds devices to a track. It carries FirmwareTrackPlaceholder
	// where the track's name goes.
	DeviceAdd string
	// DeviceAddWithSetting adds devices to a track and sets their
	// boot-environment setting in one go. It carries FirmwareTrackPlaceholder
	// where the track's name goes.
	DeviceAddWithSetting string
}

// FirmwareTrackPlaceholder stands for the track's name in the vocabulary's
// TrackSchedule and TrackDetach. A refusal that names its tracks gets the
// commands with each name in place; one that names none keeps it, so the
// command reads as a template ("firmware track schedule <track> --no-schedule").
const FirmwareTrackPlaceholder = "<track>"

// DetachCommand is TrackDetach for track. Anything that is not one of the three
// fixed track names leaves the placeholder as it is, so a command is never
// built from text that only looks like a track.
func (v FirmwareVocabulary) DetachCommand(track string) string {
	return forFirmwareTrack(v.TrackDetach, track)
}

// ScheduleCommand is TrackSchedule for track, with the same rule as DetachCommand.
func (v FirmwareVocabulary) ScheduleCommand(track string) string {
	return forFirmwareTrack(v.TrackSchedule, track)
}

func forFirmwareTrack(command, track string) string {
	if !slices.Contains(models.FirmwareTracks, track) {
		return command
	}
	return strings.ReplaceAll(command, FirmwareTrackPlaceholder, track)
}

// firmwareTracksNamedBy returns the tracks a refusal lists in blocking_resources
// (a schedule delete does: conveyor order, lowercase), in the order sent. It is
// nil when the refusal lists none, and also when the list holds anything that
// is not one of the three tracks, since a command is built from each entry.
func firmwareTracksNamedBy(err error) []string {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || len(apiErr.BlockingResources) == 0 {
		return nil
	}
	for _, name := range apiErr.BlockingResources {
		if !slices.Contains(models.FirmwareTracks, name) {
			return nil
		}
	}
	return slices.Clone(apiErr.BlockingResources)
}

// firmwareSpecManagedHint is the way forward from FIRMWARE_SPEC_MANAGED. With
// the tracks known it gives one command per track, so nothing is left to fill
// in but a schedule name; without them it gives the template once.
func firmwareSpecManagedHint(v FirmwareVocabulary, tracks []string) string {
	if len(tracks) == 0 {
		return fmt.Sprintf("A firmware track's update windows belong to the track: they cannot be changed here, and the "+
			"schedule they run on cannot be deleted while a track uses it. To detach the track from its schedule: %s. "+
			"To move it to another schedule: %s.", v.TrackDetach, v.TrackSchedule)
	}
	detach := make([]string, len(tracks))
	move := make([]string, len(tracks))
	for i, track := range tracks {
		detach[i] = v.DetachCommand(track)
		move[i] = v.ScheduleCommand(track)
	}
	if len(tracks) == 1 {
		return fmt.Sprintf("To detach the track from its schedule: %s. To move it to another schedule: %s.", detach[0], move[0])
	}
	return fmt.Sprintf("To detach the tracks from their schedule: %s. To move them to another schedule: %s.",
		strings.Join(detach, "; "), strings.Join(move, "; "))
}

// firmwareDeviceUnmanagedHint is the way forward from FIRMWARE_DEVICE_UNMANAGED.
// The refusal's own message says why and names the devices, up to a cap; the
// hint gives the commands, and the number of devices when the message names
// fewer than there are (blocking_resources is capped, count is not).
func firmwareDeviceUnmanagedHint(v FirmwareVocabulary, err error) string {
	hint := fmt.Sprintf("To add the devices to a track: %s. To add them with the setting in one go: %s.", v.DeviceAdd, v.DeviceAddWithSetting)
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.BlockingCount > len(apiErr.BlockingResources) {
		hint = fmt.Sprintf("%d devices are in no track; the refusal names %d. ", apiErr.BlockingCount, len(apiErr.BlockingResources)) + hint
	}
	return hint
}

// FirmwareAdvice returns err with the way forward appended when it is one of
// the firmware refusals that has one, in the caller's own vocabulary; any
// other error comes back untouched. The result still wraps err, so the code
// NDManager sent (api.GetErrorCode) and the service code survive.
func FirmwareAdvice(err error, v FirmwareVocabulary) error {
	if err == nil {
		return nil
	}
	var hint string
	switch api.GetErrorCode(err) {
	case apiCodeFirmwareNotConfigured:
		hint = fmt.Sprintf("Create the firmware configuration first: %s.", v.ConfigCreate)
	case apiCodeFirmwareDisabled:
		hint = fmt.Sprintf("Enable it first: %s.", v.ConfigEnable)
	case apiCodeFirmwareDeviceManaged:
		hint = fmt.Sprintf("See where the devices stand with %s, and take a device out of its track with %s before upgrading it by hand.",
			v.DeviceStatus, v.DeviceRelease)
	case apiCodeFirmwareSpecManaged:
		// One refusal, two ways to meet it: changing a track's schedule entry
		// through the generic spec routes (the body names no track), and
		// deleting a schedule a track's windows run on (the body lists the
		// tracks). Either way the track is the one to change.
		hint = firmwareSpecManagedHint(v, firmwareTracksNamedBy(err))
	case apiCodeFirmwareDeviceLeaving:
		hint = fmt.Sprintf("Check progress with %s, and add the device again once it has left its track.", v.DeviceStatus)
	case apiCodeFirmwareUpgradeNotOpen:
		hint = fmt.Sprintf("A major upgrade cannot be approved while OPNsense's upgrade path is closed: the control plane has not seen "+
			"all of its files on the mirror lately. See where it stands with %s.", v.Rollout)
	case apiCodeFirmwareDeviceUnmanaged:
		hint = firmwareDeviceUnmanagedHint(v, err)
	default:
		return err
	}
	// Advice given once is not given again by an outer layer.
	if strings.Contains(err.Error(), hint) {
		return err
	}
	code := CodeAPIError
	var svcErr *Error
	if errors.As(err, &svcErr) && svcErr.Code != "" {
		code = svcErr.Code
	}
	return &Error{Code: code, Message: err.Error() + "\n  Hint: " + hint, Err: err}
}
