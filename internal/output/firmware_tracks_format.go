package output

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Firmware tracks: configuration, tracks, device standing and ledger, for all
// four formatters. The rollout board, halts and promotions are in
// firmware_rollout_format.go and the FIRMWARE_TRACK task result in
// firmware_track_result.go.
//
// Nothing here re-escapes text: every string was sanitized once, at the decode
// boundary. Timestamps are RFC 3339 strings and are shown in the configured
// display zone. A code the server sends that this build has no word for (a
// state, a reason, a candidate state) is shown as the code, never blank.

// firmwareField is one label and value line, shared by the three human
// renderers of a view so they cannot disagree about what a view says.
type firmwareField struct {
	label string
	value string
}

// writeFirmwareFields prints fields as aligned "Label: value" lines.
func writeFirmwareFields(f BaseFormatter, fields []firmwareField, indent string) {
	width := 0
	for _, fld := range fields {
		if len(fld.label) > width {
			width = len(fld.label)
		}
	}
	for _, fld := range fields {
		fmt.Fprintf(f.Writer, "%s%-*s %s\n", indent, width+1, fld.label+":", fld.value)
	}
}

// firmwareFieldBox collects fields into one titled box.
func firmwareFieldBox(title string, fields []firmwareField) *fieldBox {
	box := &fieldBox{title: title}
	for _, fld := range fields {
		box.field(fld.label, fld.value)
	}
	return box
}

// ── labels ──────────────────────────────────────────────────────────────────

// firmwareTimestamp renders an RFC 3339 string in the display zone; "-" when
// empty. A value that does not parse is shown as sent rather than hidden.
func firmwareTimestamp(s string) string {
	if s == "" {
		return "-"
	}
	t := parseRunTime(s)
	if t.IsZero() {
		return s
	}
	return FormatTimestamp(t)
}

// firmwareTimestampShort is firmwareTimestamp without the seconds, for tables.
func firmwareTimestampShort(s string) string {
	if s == "" {
		return "-"
	}
	t := parseRunTime(s)
	if t.IsZero() {
		return s
	}
	return FormatTimestampShort(t)
}

// firmwareDuration renders a span compactly: "3d 4h", "5h 20m", "12m". A span
// that is not positive is "now".
func firmwareDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		days, h := int(d.Hours())/24, int(d.Hours())%24
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, h)
	}
}

// firmwareUntil says how long from now an RFC 3339 instant is: "in 2d 3h", or
// "due" once it has passed. Empty or unparseable input gives "".
func firmwareUntil(s string, now time.Time) string {
	t := parseRunTime(s)
	if t.IsZero() {
		return ""
	}
	if !t.After(now) {
		return "due"
	}
	return "in " + firmwareDuration(t.Sub(now))
}

// firmwareWhen is a timestamp followed by how far away it is.
func firmwareWhen(s string, now time.Time) string {
	if s == "" {
		return "-"
	}
	stamp := firmwareTimestamp(s)
	if until := firmwareUntil(s, now); until != "" {
		return stamp + " (" + until + ")"
	}
	return stamp
}

func firmwarePlural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func firmwareSoak(days int) string { return firmwarePlural(days, "day", "days") }

func firmwareYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func firmwareOnOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// firmwareOnOffRef is a setting that may not have been reported: "-" when it
// was not.
func firmwareOnOffRef(v *bool) string {
	if v == nil {
		return "-"
	}
	return firmwareOnOff(*v)
}

// firmwareTrackDisplay names a device's track; a device in none is unmanaged.
func firmwareTrackDisplay(track string) string {
	if track == "" {
		return ColorDim.Sprint(models.FirmwareUnmanaged)
	}
	return track
}

// firmwareStateDisplay colours a device's state in its track.
func firmwareStateDisplay(state string) string {
	switch state {
	case models.FirmwareStateManaged:
		return ColorEnabled.Sprint(state)
	case models.FirmwareStateEnrolling:
		return ColorInProgress.Sprint(state)
	case models.FirmwareStateHeld, models.FirmwareStateUnenrolling:
		return ColorWarning.Sprint(state)
	case "":
		return ColorDim.Sprint("-")
	default:
		return state
	}
}

// firmwareOutcomeDisplay colours the outcome of a step.
func firmwareOutcomeDisplay(outcome string) string {
	switch outcome {
	case models.FirmwareOutcomeSuccess:
		return ColorSuccess.Sprint(outcome)
	case models.FirmwareOutcomeNoop:
		return ColorDim.Sprint(outcome)
	case models.FirmwareOutcomeHeld, models.FirmwareOutcomeReleased:
		return ColorWarning.Sprint(outcome)
	case models.FirmwareOutcomeFailed, models.FirmwareOutcomeFailedNoResponse:
		return ColorError.Sprint(outcome)
	case "":
		return ColorDim.Sprint("-")
	default:
		return outcome
	}
}

// firmwareReasonCell is a reason as a sentence, or "-" when there is none; the
// code itself when the build has no sentence for it.
func firmwareReasonCell(code string) string {
	if code == "" {
		return "-"
	}
	return models.FirmwareReasonText(code)
}

// firmwareReasonFull is a reason as a sentence followed by its code, so the
// code stays searchable: "a custom firmware flavour is set (held_custom_flavour)".
func firmwareReasonFull(code string) string {
	if code == "" {
		return "-"
	}
	text := models.FirmwareReasonText(code)
	if text == code {
		return code
	}
	return text + " (" + code + ")"
}

// firmwareDeviceCounts summarises a track's devices: "4 (3 managed, 1 held)".
func firmwareDeviceCounts(c models.FirmwareDeviceCounts) string {
	var parts []string
	for _, p := range []struct {
		n    int
		name string
	}{
		{c.Enrolling, models.FirmwareStateEnrolling},
		{c.Managed, models.FirmwareStateManaged},
		{c.Held, models.FirmwareStateHeld},
		{c.Unenrolling, models.FirmwareStateUnenrolling},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.name))
		}
	}
	if len(parts) == 0 {
		return strconv.Itoa(c.Total)
	}
	return fmt.Sprintf("%d (%s)", c.Total, strings.Join(parts, ", "))
}

// firmwareDevicesPhrase is firmwareDeviceCounts in a sentence: "4 devices
// (3 managed, 1 held)".
func firmwareDevicesPhrase(c models.FirmwareDeviceCounts) string {
	phrase := firmwarePlural(c.Total, "device", "devices")
	if counts := firmwareDeviceCounts(c); strings.Contains(counts, "(") {
		phrase += counts[strings.Index(counts, " ("):]
	}
	return phrase
}

// firmwareScheduleCell names the schedule a track's windows run on. A schedule
// that is attached but not firing (the schedule or its spec is disabled) says
// so, because the windows will not come.
func firmwareScheduleCell(t models.FirmwareTrack) string {
	switch {
	case t.Schedule == "":
		return "-"
	case t.Scheduled:
		return t.Schedule
	default:
		return t.Schedule + " (disabled)"
	}
}

// firmwareBlockedText says why no step is sent to a device.
func firmwareBlockedText(blocked, agentVersion string) string {
	switch blocked {
	case models.FirmwareBlockedAgentTooOld:
		if agentVersion != "" {
			return "agent too old for firmware tracks (" + agentVersion + ")"
		}
		return "agent too old for firmware tracks"
	case models.FirmwareBlockedDeviceDisabled:
		return "device disabled: no step is sent while it is"
	case "":
		return ""
	default:
		return blocked
	}
}

// firmwareNoteText explains a note on a device assignment.
func firmwareNoteText(note string) string {
	switch note {
	case models.FirmwareBlockedAgentTooOld:
		return "agent too old for firmware tracks: no step is sent until it is upgraded"
	case models.FirmwareBlockedDeviceDisabled:
		return "device disabled: no step is sent while it is"
	default:
		return note
	}
}

// ── configuration and tracks ────────────────────────────────────────────────

// firmwarePlanOff is what an organization whose plan lacks firmware tracks
// gets while it still has a configuration: the control plane sends no new
// steps and releases the devices still in a track.
const firmwarePlanOff = "no new steps are sent; devices still in a track are being released"

// firmwarePlanCell is the Plan row of the configuration and the rollout board.
func firmwarePlanCell(included bool) string {
	if included {
		return "included"
	}
	return "not included (" + firmwarePlanOff + ")"
}

func firmwareConfigFields(cfg *models.FirmwareConfig) []firmwareField {
	status := ColorDisabled.Sprint("○") + " " + ColorDisabled.Sprint("Disabled")
	if cfg.Status == models.FirmwareConfigEnabled {
		status = ColorEnabled.Sprint("●") + " " + ColorEnabled.Sprint("Enabled")
	} else if cfg.Status != models.FirmwareConfigDisabled {
		status = cfg.Status
	}
	plan := firmwarePlanCell(cfg.PlanEnabled)
	reboot := "yes (the full update, base and kernel included, then a reboot)"
	if !cfg.Reboot {
		reboot = "no (userland packages only; base and kernel stay deferred)"
	}
	fields := []firmwareField{
		{"Organization", cfg.OrganizationName},
		{"Status", status},
		{"Plan", plan},
		{"Reboot", reboot},
	}
	// The server sends the count on enabling too, always 0: it means something
	// only when Firmware was just disabled.
	if cfg.ReleasedDevices != nil && cfg.Status == models.FirmwareConfigDisabled {
		fields = append(fields, firmwareField{"Releasing", firmwarePlural(*cfg.ReleasedDevices, "device", "devices") + " leaving their track"})
	}
	fields = append(fields, firmwareField{"Created", firmwareTimestamp(cfg.CreatedAt)})
	if cfg.UpdatedAt != "" {
		fields = append(fields, firmwareField{"Updated", firmwareTimestamp(cfg.UpdatedAt)})
	}
	return fields
}

// firmwareSoakNote explains what a track's soak is counted from.
func firmwareSoakNote(track string) string {
	if track == models.FirmwareTrackFast {
		return "counted from OPNsense publishing the release"
	}
	return "counted from the previous track's evidence being met"
}

// firmwareMinSuccessNote explains what a track's min_success counts.
func firmwareMinSuccessNote(track string, n int) string {
	if track == models.FirmwareTrackFast {
		return "no previous track to wait for"
	}
	if n == 0 {
		return "no evidence required from the previous track"
	}
	return firmwarePlural(n, "device", "devices") + " of the previous track must reach a release first"
}

// firmwareMajorSwitchNote says what a track's auto_apply_major means.
func firmwareMajorSwitchNote(on bool) string {
	if on {
		return "on: the track's windows move its devices to the next OPNsense series"
	}
	return "off: the track's windows ask none of its devices to upgrade"
}

func firmwareTrackFields(t *models.FirmwareTrack, now time.Time) []firmwareField {
	fields := []firmwareField{
		{"Track", t.Track},
		{"Soak", firmwareSoak(t.SoakDays) + ", " + firmwareSoakNote(t.Track)},
		{"Min success", strconv.Itoa(t.MinSuccess) + ", " + firmwareMinSuccessNote(t.Track, t.MinSuccess)},
		{"Schedule", firmwareScheduleCell(*t)},
		{"Next window", firmwareWhen(t.NextRunAt, now)},
	}
	// A control plane that predates major upgrades does not report the switch;
	// say nothing rather than that it is off.
	if t.AutoApplyMajor != nil {
		fields = append(fields, firmwareField{"Major upgrades", firmwareMajorSwitchNote(*t.AutoApplyMajor)})
	}
	return append(fields, firmwareField{"Devices", firmwareDeviceCounts(t.Devices)})
}

// firmwareTrackNotes lists what a track write reported beyond the track.
func firmwareTrackNotes(t *models.FirmwareTrack) []string {
	if t.CancelledTasks == nil || *t.CancelledTasks == 0 {
		return nil
	}
	return []string{firmwarePlural(*t.CancelledTasks, "queued major-upgrade step", "queued major-upgrade steps") + " cancelled"}
}

// firmwareAnyMajorSwitch reports whether any track reports its switch.
func firmwareAnyMajorSwitch(tracks []models.FirmwareTrack) bool {
	for i := range tracks {
		if tracks[i].AutoApplyMajor != nil {
			return true
		}
	}
	return false
}

// firmwareTrackLine is a track on one line, for the simple formats.
func firmwareTrackLine(t *models.FirmwareTrack, now time.Time) string {
	line := fmt.Sprintf("• %s: soak %s, min success %d, schedule %s, %s",
		t.Track, firmwareSoak(t.SoakDays), t.MinSuccess, firmwareScheduleCell(*t), firmwareDevicesPhrase(t.Devices))
	if t.NextRunAt != "" {
		line += ", next window " + firmwareWhen(t.NextRunAt, now)
	}
	if t.AutoApplyMajor != nil {
		line += ", major upgrades " + firmwareOnOff(*t.AutoApplyMajor)
	}
	return line
}

func firmwareTracksTable(f BaseFormatter, tracks []models.FirmwareTrack, now time.Time) {
	headers := []string{"Track", "Soak", "Min Success", "Schedule", "Next Window", "Devices"}
	withSwitch := firmwareAnyMajorSwitch(tracks)
	if withSwitch {
		headers = append(headers, "Major Upgrades")
	}
	table := NewStyledTable(headers)
	for i := range tracks {
		t := &tracks[i]
		next := "-"
		if t.NextRunAt != "" {
			next = firmwareTimestampShort(t.NextRunAt)
			if until := firmwareUntil(t.NextRunAt, now); until != "" {
				next += " (" + until + ")"
			}
		}
		row := []string{
			t.Track,
			firmwareSoak(t.SoakDays),
			strconv.Itoa(t.MinSuccess),
			firmwareScheduleCell(*t),
			next,
			firmwareDeviceCounts(t.Devices),
		}
		if withSwitch {
			row = append(row, firmwareOnOffRef(t.AutoApplyMajor))
		}
		table.Append(row)
	}
	table.RenderTo(f.Writer)
}

// FormatFirmwareConfig renders the configuration with its tracks, and what a
// configuration write reported.
func (f *TableFormatter) FormatFirmwareConfig(cfg *models.FirmwareConfig) error {
	if cfg.Message != "" {
		f.Success(cfg.Message)
	}
	writeFirmwareFields(f.BaseFormatter, firmwareConfigFields(cfg), "")
	if len(cfg.Tracks) > 0 {
		fmt.Fprintln(f.Writer)
		firmwareTracksTable(f.BaseFormatter, cfg.Tracks, time.Now())
	}
	return nil
}

func (f *SimpleFormatter) FormatFirmwareConfig(cfg *models.FirmwareConfig) error {
	if cfg.Message != "" {
		f.Success(cfg.Message)
	}
	fmt.Fprintf(f.Writer, "Firmware: %s\n", cfg.OrganizationName)
	writeFirmwareFields(f.BaseFormatter, firmwareConfigFields(cfg)[1:], "  ")
	now := time.Now()
	for i := range cfg.Tracks {
		fmt.Fprintln(f.Writer, firmwareTrackLine(&cfg.Tracks[i], now))
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareConfig(cfg *models.FirmwareConfig) error {
	if cfg.Message != "" {
		f.Success(cfg.Message)
	}
	firmwareFieldBox("Firmware configuration", firmwareConfigFields(cfg)).render(f.BaseFormatter)
	now := time.Now()
	for i := range cfg.Tracks {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("Track "+cfg.Tracks[i].Track, firmwareTrackFields(&cfg.Tracks[i], now)[1:]).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareConfig(cfg *models.FirmwareConfig) error {
	return f.output(cfg)
}

// FormatFirmwareConfigDeleted reports the configuration gone.
func (f *TableFormatter) FormatFirmwareConfigDeleted(result *models.FirmwareConfigDeleted) error {
	f.Success(firmwareDeletedMessage(result))
	return nil
}

func (f *SimpleFormatter) FormatFirmwareConfigDeleted(result *models.FirmwareConfigDeleted) error {
	f.Success(firmwareDeletedMessage(result))
	return nil
}

func (f *DetailedFormatter) FormatFirmwareConfigDeleted(result *models.FirmwareConfigDeleted) error {
	f.Success(firmwareDeletedMessage(result))
	return nil
}

func (f *JSONFormatter) FormatFirmwareConfigDeleted(result *models.FirmwareConfigDeleted) error {
	return f.output(result)
}

func firmwareDeletedMessage(result *models.FirmwareConfigDeleted) string {
	msg := result.Message
	if msg == "" {
		msg = "Firmware configuration deleted"
	}
	if result.Organization != "" {
		msg += " (" + result.Organization + ")"
	}
	return msg
}

// FormatFirmwareTracks renders the three tracks side by side.
func (f *TableFormatter) FormatFirmwareTracks(tracks []models.FirmwareTrack) error {
	if len(tracks) == 0 {
		f.Info("No tracks found")
		return nil
	}
	firmwareTracksTable(f.BaseFormatter, tracks, time.Now())
	return nil
}

func (f *SimpleFormatter) FormatFirmwareTracks(tracks []models.FirmwareTrack) error {
	if len(tracks) == 0 {
		f.Info("No tracks found")
		return nil
	}
	now := time.Now()
	for i := range tracks {
		fmt.Fprintln(f.Writer, firmwareTrackLine(&tracks[i], now))
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareTracks(tracks []models.FirmwareTrack) error {
	if len(tracks) == 0 {
		f.Info("No tracks found")
		return nil
	}
	now := time.Now()
	for i := range tracks {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		firmwareFieldBox("Track "+tracks[i].Track, firmwareTrackFields(&tracks[i], now)[1:]).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareTracks(tracks []models.FirmwareTrack) error {
	return f.output(map[string]interface{}{
		"tracks": tracks,
		"total":  len(tracks),
	})
}

// FormatFirmwareTrack renders one track, and the result of changing it.
func (f *TableFormatter) FormatFirmwareTrack(track *models.FirmwareTrack) error {
	writeFirmwareFields(f.BaseFormatter, firmwareTrackFields(track, time.Now()), "")
	for _, note := range firmwareTrackNotes(track) {
		fmt.Fprintf(f.Writer, "%s\n", note)
	}
	return nil
}

func (f *SimpleFormatter) FormatFirmwareTrack(track *models.FirmwareTrack) error {
	fmt.Fprintf(f.Writer, "Track: %s\n", track.Track)
	writeFirmwareFields(f.BaseFormatter, firmwareTrackFields(track, time.Now())[1:], "  ")
	for _, note := range firmwareTrackNotes(track) {
		fmt.Fprintf(f.Writer, "  %s\n", note)
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareTrack(track *models.FirmwareTrack) error {
	firmwareFieldBox("Track "+track.Track, firmwareTrackFields(track, time.Now())[1:]).render(f.BaseFormatter)
	for _, note := range firmwareTrackNotes(track) {
		fmt.Fprintf(f.Writer, "\n%s\n", note)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareTrack(track *models.FirmwareTrack) error {
	return f.output(track)
}

// ── devices ─────────────────────────────────────────────────────────────────

// firmwareAssignmentHeadline sums up what moving devices did.
func firmwareAssignmentHeadline(r *models.FirmwareAssignment) string {
	counts := map[string]int{}
	for _, res := range r.Results {
		counts[res.Result]++
	}
	var parts []string
	for _, kind := range []string{
		models.FirmwareAssignmentAdded, models.FirmwareAssignmentMoved,
		models.FirmwareAssignmentReleased, models.FirmwareAssignmentUpdated, models.FirmwareAssignmentUnchanged,
	} {
		switch {
		case counts[kind] == 0:
		case kind == models.FirmwareAssignmentUpdated:
			parts = append(parts, fmt.Sprintf("boot-environment setting changed on %d", counts[kind]))
		default:
			parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
		}
	}
	head := firmwarePlural(len(r.Results), "device", "devices")
	// A request that carried only the boot-environment setting moved nobody and
	// has no track to name.
	if r.Track != "" {
		target := r.Track
		if target != models.FirmwareUnmanaged {
			target += " track"
		}
		head += " → " + target
	}
	if len(parts) > 0 {
		head += ": " + strings.Join(parts, ", ")
	}
	return head
}

// firmwareAnyBootEnvironment reports whether any result carries the setting.
func firmwareAnyBootEnvironment(r *models.FirmwareAssignment) bool {
	for _, res := range r.Results {
		if res.BootEnvironment != nil {
			return true
		}
	}
	return false
}

func firmwareAssignmentNotes(r *models.FirmwareAssignment) []string {
	var notes []string
	if r.CancelledTasks > 0 {
		notes = append(notes, firmwarePlural(r.CancelledTasks, "pending firmware step", "pending firmware steps")+" cancelled")
	}
	return notes
}

func firmwareResultNotes(res models.FirmwareAssignmentResult) string {
	texts := make([]string, 0, len(res.Notes))
	for _, n := range res.Notes {
		texts = append(texts, firmwareNoteText(n))
	}
	return strings.Join(texts, "; ")
}

// FormatFirmwareAssignment renders what moving devices into a track did.
func (f *TableFormatter) FormatFirmwareAssignment(r *models.FirmwareAssignment) error {
	f.Success(firmwareAssignmentHeadline(r))
	if len(r.Results) > 0 {
		headers := []string{"Device", "Result", "Track", "State"}
		withBE := firmwareAnyBootEnvironment(r)
		if withBE {
			headers = append(headers, "Boot Env")
		}
		table := NewStyledTable(append(headers, "Notes"))
		for _, res := range r.Results {
			row := []string{res.DeviceName, res.Result, firmwareTrackDisplay(res.Track), firmwareStateDisplay(res.State)}
			if withBE {
				row = append(row, firmwareOnOffRef(res.BootEnvironment))
			}
			table.Append(append(row, firmwareResultNotes(res)))
		}
		table.RenderTo(f.Writer)
	}
	for _, note := range firmwareAssignmentNotes(r) {
		fmt.Fprintf(f.Writer, "%s\n", note)
	}
	return nil
}

func (f *SimpleFormatter) FormatFirmwareAssignment(r *models.FirmwareAssignment) error {
	f.Success(firmwareAssignmentHeadline(r))
	for _, res := range r.Results {
		line := fmt.Sprintf("• %s: %s", res.DeviceName, res.Result)
		if res.State != "" {
			line += fmt.Sprintf(" (%s, %s)", firmwareTrackDisplay(res.Track), firmwareStateDisplay(res.State))
		}
		if res.BootEnvironment != nil {
			line += ", boot environment " + firmwareOnOff(*res.BootEnvironment)
		}
		if notes := firmwareResultNotes(res); notes != "" {
			line += " — " + notes
		}
		fmt.Fprintln(f.Writer, line)
	}
	for _, note := range firmwareAssignmentNotes(r) {
		fmt.Fprintf(f.Writer, "%s\n", note)
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareAssignment(r *models.FirmwareAssignment) error {
	f.Success(firmwareAssignmentHeadline(r))
	for _, res := range r.Results {
		fields := []firmwareField{
			{"Result", res.Result},
			{"Track", firmwareTrackDisplay(res.Track)},
			{"State", firmwareStateDisplay(res.State)},
		}
		if res.BootEnvironment != nil {
			fields = append(fields, firmwareField{"Boot environment", firmwareOnOff(*res.BootEnvironment)})
		}
		if notes := firmwareResultNotes(res); notes != "" {
			fields = append(fields, firmwareField{"Notes", notes})
		}
		fmt.Fprintln(f.Writer)
		firmwareFieldBox(res.DeviceName, fields).render(f.BaseFormatter)
	}
	for _, note := range firmwareAssignmentNotes(r) {
		fmt.Fprintf(f.Writer, "\n%s\n", note)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareAssignment(r *models.FirmwareAssignment) error {
	return f.output(r)
}

// firmwareDeviceNotes lists what is worth a line on a device beyond its track,
// state and last result: why no step reaches it, a pin changed outside
// NetDefense, and an automatic release.
func firmwareDeviceNotes(d *models.FirmwareDevice) []string {
	var notes []string
	if text := firmwareBlockedText(d.Blocked, d.AgentVersion); text != "" {
		notes = append(notes, text)
	}
	if d.Drift != nil && *d.Drift {
		notes = append(notes, "the pin had been changed outside NetDefense")
	}
	if d.Released != nil {
		notes = append(notes, fmt.Sprintf("released automatically %s: %s",
			firmwareTimestampShort(d.Released.At), firmwareReasonCell(d.Released.Reason)))
	}
	return append(notes, firmwareDeviceWarnings(d)...)
}

// firmwareDeviceWarnings are what the newest major upgrade found on the device
// that the operator should know: a repository or a package from outside OPNsense
// and NetDefense, in one line. The boot-environment tokens are left out; the boot
// environment field says that, with the setting beside it. A token this build does
// not know is shown as its own sentence, or as itself.
func firmwareDeviceWarnings(d *models.FirmwareDevice) []string {
	var repository, pkg bool
	var others []string
	for _, token := range d.Notes {
		switch {
		case token == models.FirmwareNoteThirdPartyRepository:
			repository = true
		case token == models.FirmwareNoteThirdPartyPackage:
			pkg = true
		case !models.FirmwareNoteIsBootEnvironment(token):
			others = append(others, models.FirmwareNoteText(token))
		}
	}
	var warnings []string
	switch {
	case repository && pkg:
		warnings = append(warnings, "the last major upgrade found a third-party repository and a third-party package on the device")
	case repository:
		warnings = append(warnings, "the last major upgrade found a third-party repository on the device")
	case pkg:
		warnings = append(warnings, "the last major upgrade found a third-party package on the device")
	}
	return append(warnings, others...)
}

// firmwareAnyMajorData reports whether any device row carries a field of the
// major upgrade. A control plane that predates them sends none, and the views
// then show exactly what they always showed.
func firmwareAnyMajorData(items []models.FirmwareDevice) bool {
	for i := range items {
		if items[i].Series != "" || items[i].Major != nil || items[i].BootEnvironment != nil {
			return true
		}
	}
	return false
}

func firmwareMajorCell(d *models.FirmwareDevice) string {
	switch {
	case d.Major == nil:
		return ColorDim.Sprint("-")
	case d.Major.Eligible:
		return ColorEnabled.Sprint("eligible")
	default:
		return ColorWarning.Sprint("blocked")
	}
}

// firmwareMajorStatus says in words whether the next window would send the
// device a major upgrade: "eligible at the next window", or what blocks it.
func firmwareMajorStatus(d *models.FirmwareDevice) string {
	switch {
	case d.Major == nil:
		return ""
	case d.Major.Eligible:
		return "eligible at the next window"
	case d.Major.BlockedBy == "":
		return "blocked"
	case d.Major.BlockedBy == d.HeldReason:
		return "blocked by the last hold"
	default:
		return "blocked: " + models.FirmwareDeviceMajorBlockedText(d.Major.BlockedBy)
	}
}

// firmwareMajorBlockedNote is the line a device that cannot be upgraded earns
// in the compact formats. A block that is the device's own last hold points at
// the hold, which those formats show beside it, and does not say it twice.
func firmwareMajorBlockedNote(d *models.FirmwareDevice) string {
	if d.Major == nil || d.Major.Eligible || d.Major.BlockedBy == "" {
		return ""
	}
	if d.Major.BlockedBy == d.HeldReason {
		return "major upgrade blocked by the last hold"
	}
	return "major upgrade blocked: " + models.FirmwareDeviceMajorBlockedText(d.Major.BlockedBy)
}

// firmwareBootEnvironmentLastShort is what the last major upgrade did about the
// boot environment, in a few words for a cell; a value this build does not know
// is shown as it was sent.
func firmwareBootEnvironmentLastShort(last string) string {
	switch last {
	case "made":
		return "made"
	case "skipped_no_room":
		return "skipped, no room"
	case "off":
		return "setting was off"
	case "unavailable":
		return "unavailable"
	default:
		return last
	}
}

func firmwareBootEnvironmentCell(d *models.FirmwareDevice) string {
	if d.BootEnvironment == nil {
		return "-"
	}
	cell := firmwareOnOff(d.BootEnvironment.Setting)
	if d.BootEnvironment.Last != "" {
		cell += " (last: " + firmwareBootEnvironmentLastShort(d.BootEnvironment.Last) + ")"
	}
	return cell
}

// firmwareBootEnvironmentText is the boot-environment field in words.
func firmwareBootEnvironmentText(d *models.FirmwareDevice) string {
	if d.BootEnvironment == nil {
		return ""
	}
	text := firmwareOnOff(d.BootEnvironment.Setting)
	if d.BootEnvironment.Last != "" {
		text += "; " + models.FirmwareBootEnvironmentLastText(d.BootEnvironment.Last)
	}
	return text
}

// firmwareErrorNote is the agent's message from a failed step on one line, cut
// to width runes (0 keeps it whole), for the formats that have no line of their
// own for it.
func firmwareErrorNote(d *models.FirmwareDevice, width int) string {
	text := strings.Join(strings.Fields(d.LastError), " ")
	if text == "" {
		return ""
	}
	if runes := []rune(text); width > 0 && len(runes) > width {
		text = string(runes[:width-1]) + "…"
	}
	return "last error: " + text
}

// firmwareErrorCellWidth is how much of a failed step's message a table cell shows.
const firmwareErrorCellWidth = 60

// nonEmpty is s as a list: none when it is empty.
func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func firmwareDashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firmwareDeviceFields(d *models.FirmwareDevice) []firmwareField {
	fields := []firmwareField{{"Track", firmwareTrackDisplay(d.Track)}}
	if d.State != "" {
		fields = append(fields, firmwareField{"State", firmwareStateDisplay(d.State)})
	}
	if d.PinnedVersion != "" {
		fields = append(fields, firmwareField{"Pinned", d.PinnedVersion})
	}
	if d.Series != "" {
		fields = append(fields, firmwareField{"Series", d.Series})
	}
	if d.HeldReason != "" {
		fields = append(fields, firmwareField{"Held", firmwareReasonFull(d.HeldReason)})
	}
	if status := firmwareMajorStatus(d); status != "" {
		fields = append(fields, firmwareField{"Major upgrade", status})
	}
	if text := firmwareBootEnvironmentText(d); text != "" {
		fields = append(fields, firmwareField{"Boot environment", text})
	}
	if d.LastStatus != "" {
		last := firmwareOutcomeDisplay(d.LastStatus)
		if d.LastRunAt != "" {
			last += " at " + firmwareTimestamp(d.LastRunAt)
		}
		fields = append(fields, firmwareField{"Last result", last})
	}
	if d.LastError != "" {
		fields = append(fields, firmwareField{"Last error", d.LastError})
	}
	if d.Drift != nil {
		fields = append(fields, firmwareField{"Pin drift", firmwareYesNo(*d.Drift)})
	}
	if d.ReinstallEntries != nil && *d.ReinstallEntries > 0 {
		fields = append(fields, firmwareField{"Reinstalls", strconv.Itoa(*d.ReinstallEntries) + " same-version reinstall entry listed by the last check (harmless)"})
	}
	if d.AgentVersion != "" {
		fields = append(fields, firmwareField{"Agent", d.AgentVersion})
	}
	fields = append(fields, firmwareField{"Device", d.DeviceStatus})
	for _, note := range firmwareDeviceNotes(d) {
		fields = append(fields, firmwareField{"Note", note})
	}
	return fields
}

// FormatFirmwareDevices renders a page of devices and where they stand.
func (f *TableFormatter) FormatFirmwareDevices(list *models.FirmwareDeviceList) error {
	if len(list.Items) == 0 {
		f.Info("No devices found")
		return nil
	}
	// The columns of the major upgrade appear only when a row carries something
	// for them, so a page from a control plane that predates it is unchanged.
	withMajor := firmwareAnyMajorData(list.Items)
	headers := []string{"Device", "Track", "State", "Pinned", "Held Reason", "Last Result", "Last Run", "Notes"}
	if withMajor {
		headers = []string{"Device", "Track", "State", "Series", "Pinned", "Held Reason", "Last Result", "Last Run", "Major", "Boot Env", "Notes"}
	}
	table := NewStyledTable(headers)
	for i := range list.Items {
		d := &list.Items[i]
		last := "-"
		if d.LastStatus != "" {
			last = firmwareOutcomeDisplay(d.LastStatus)
		}
		lastRun := "-"
		if d.LastRunAt != "" {
			lastRun = firmwareTimestampShort(d.LastRunAt)
		}
		notes := firmwareDeviceNotes(d)
		notes = append(notes, nonEmpty(firmwareMajorBlockedNote(d))...)
		notes = append(notes, nonEmpty(firmwareErrorNote(d, firmwareErrorCellWidth))...)
		row := []string{d.DeviceName, firmwareTrackDisplay(d.Track), firmwareStateDisplay(d.State)}
		if withMajor {
			row = append(row, firmwareDashIfEmpty(d.Series))
		}
		row = append(row, firmwareDashIfEmpty(d.PinnedVersion), firmwareReasonCell(d.HeldReason), last, lastRun)
		if withMajor {
			row = append(row, firmwareMajorCell(d), firmwareBootEnvironmentCell(d))
		}
		table.Append(append(row, strings.Join(notes, "; ")))
	}
	table.RenderTo(f.Writer)
	fmt.Fprintf(f.Writer, "\nTotal: %d device(s)\n", list.Total)
	return nil
}

func (f *SimpleFormatter) FormatFirmwareDevices(list *models.FirmwareDeviceList) error {
	if len(list.Items) == 0 {
		f.Info("No devices found")
		return nil
	}
	for i := range list.Items {
		d := &list.Items[i]
		line := fmt.Sprintf("• %s: %s", d.DeviceName, firmwareTrackDisplay(d.Track))
		if d.State != "" {
			line += "/" + firmwareStateDisplay(d.State)
		}
		if d.PinnedVersion != "" {
			line += ", pinned " + d.PinnedVersion
		}
		if d.Series != "" {
			line += " (series " + d.Series + ")"
		}
		if d.LastStatus != "" {
			line += ", last " + firmwareOutcomeDisplay(d.LastStatus)
			if d.LastRunAt != "" {
				line += " " + firmwareTimestampShort(d.LastRunAt)
			}
		}
		if d.Major != nil && d.Major.Eligible {
			line += ", major upgrade eligible"
		}
		if d.BootEnvironment != nil {
			line += ", boot environment " + firmwareBootEnvironmentCell(d)
		}
		fmt.Fprintln(f.Writer, line)
		if d.HeldReason != "" {
			fmt.Fprintf(f.Writer, "    held: %s\n", firmwareReasonFull(d.HeldReason))
		}
		notes := append(firmwareDeviceNotes(d), nonEmpty(firmwareMajorBlockedNote(d))...)
		for _, note := range append(notes, nonEmpty(firmwareErrorNote(d, 0))...) {
			fmt.Fprintf(f.Writer, "    note: %s\n", note)
		}
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareDevices(list *models.FirmwareDeviceList) error {
	if len(list.Items) == 0 {
		f.Info("No devices found")
		return nil
	}
	for i := range list.Items {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		firmwareFieldBox(list.Items[i].DeviceName, firmwareDeviceFields(&list.Items[i])).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareDevices(list *models.FirmwareDeviceList) error {
	return f.output(list)
}

// ── ledger ──────────────────────────────────────────────────────────────────

// firmwareVersionMove is "26.7.3 → 26.7.5", or the one version a step knows,
// or "-" when it knows none.
func firmwareVersionMove(from, to string) string {
	switch {
	case from != "" && to != "" && from == to:
		return to + " (unchanged)"
	case from != "" && to != "":
		return from + " → " + to
	case to != "":
		return "→ " + to
	case from != "":
		return from
	default:
		return "-"
	}
}

func firmwareHistoryNote(item *models.FirmwareHistoryItem) string {
	if item.ClearedAt == "" {
		return ""
	}
	note := "failure cleared " + firmwareTimestampShort(item.ClearedAt)
	if item.ClearedBy != "" {
		note += " by " + item.ClearedBy
	}
	return note
}

// firmwareKindLabel says what a ledger row's kind is; empty when the control
// plane sent none (one that predates major upgrades only had ordinary updates).
func firmwareKindLabel(kind string) string {
	switch kind {
	case models.FirmwareKindMinor:
		return "minor (an ordinary update)"
	case models.FirmwareKindMajor:
		return "major (the upgrade to the next OPNsense series)"
	default:
		return kind
	}
}

// firmwareAnyMajorRow reports whether a page of the ledger holds a major row.
func firmwareAnyMajorRow(items []models.FirmwareHistoryItem) bool {
	for i := range items {
		if items[i].Kind == models.FirmwareKindMajor {
			return true
		}
	}
	return false
}

func firmwareHistoryFields(item *models.FirmwareHistoryItem) []firmwareField {
	fields := []firmwareField{
		{"Track", item.Track},
		{"Action", item.Action},
	}
	if item.Kind != "" {
		fields = append(fields, firmwareField{"Kind", firmwareKindLabel(item.Kind)})
	}
	fields = append(fields,
		firmwareField{"Outcome", firmwareOutcomeDisplay(item.Outcome)},
		firmwareField{"Reason", firmwareReasonFull(item.Reason)},
		firmwareField{"Target", firmwareDashIfEmpty(item.Target)},
		firmwareField{"Versions", firmwareVersionMove(item.FromVersion, item.ToVersion)},
		firmwareField{"Applied", firmwareYesNo(item.Applied)},
	)
	if note := firmwareHistoryNote(item); note != "" {
		fields = append(fields, firmwareField{"Note", note})
	}
	return fields
}

// FormatFirmwareHistory renders a page of a device's ledger.
func (f *TableFormatter) FormatFirmwareHistory(h *models.FirmwareHistory) error {
	if len(h.Items) == 0 {
		f.Info("No firmware history for " + h.DeviceName)
		return nil
	}
	// The Kind column appears on a page that holds a major row; a page of
	// ordinary updates is what it always was.
	withKind := firmwareAnyMajorRow(h.Items)
	headers := []string{"When", "Track", "Action"}
	if withKind {
		headers = append(headers, "Kind")
	}
	table := NewStyledTable(append(headers, "Outcome", "Reason", "Target", "Versions", "Applied", "Notes"))
	for i := range h.Items {
		item := &h.Items[i]
		row := []string{firmwareTimestampShort(item.CompletedAt), item.Track, item.Action}
		if withKind {
			row = append(row, firmwareDashIfEmpty(item.Kind))
		}
		table.Append(append(row,
			firmwareOutcomeDisplay(item.Outcome),
			firmwareReasonCell(item.Reason),
			firmwareDashIfEmpty(item.Target),
			firmwareVersionMove(item.FromVersion, item.ToVersion),
			firmwareYesNo(item.Applied),
			firmwareHistoryNote(item),
		))
	}
	table.RenderTo(f.Writer)
	fmt.Fprintf(f.Writer, "\nTotal: %d step(s) for %s\n", h.Total, h.DeviceName)
	return nil
}

func (f *SimpleFormatter) FormatFirmwareHistory(h *models.FirmwareHistory) error {
	if len(h.Items) == 0 {
		f.Info("No firmware history for " + h.DeviceName)
		return nil
	}
	for i := range h.Items {
		item := &h.Items[i]
		track := item.Track
		if item.Kind == models.FirmwareKindMajor {
			track += ", major upgrade"
			if item.Target != "" {
				track += " to " + item.Target
			}
		}
		fmt.Fprintf(f.Writer, "• %s  %s %s (%s)",
			firmwareTimestampShort(item.CompletedAt), item.Action, firmwareOutcomeDisplay(item.Outcome), track)
		if move := firmwareVersionMove(item.FromVersion, item.ToVersion); move != "-" {
			fmt.Fprintf(f.Writer, ": %s", move)
		}
		if item.Reason != "" {
			fmt.Fprintf(f.Writer, " — %s", firmwareReasonCell(item.Reason))
		}
		if note := firmwareHistoryNote(item); note != "" {
			fmt.Fprintf(f.Writer, " [%s]", note)
		}
		fmt.Fprintln(f.Writer)
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareHistory(h *models.FirmwareHistory) error {
	if len(h.Items) == 0 {
		f.Info("No firmware history for " + h.DeviceName)
		return nil
	}
	for i := range h.Items {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		firmwareFieldBox(h.DeviceName+" · "+firmwareTimestamp(h.Items[i].CompletedAt), firmwareHistoryFields(&h.Items[i])).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareHistory(h *models.FirmwareHistory) error {
	return f.output(h)
}
