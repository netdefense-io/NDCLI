package output

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Firmware tracks: the rollout board (where every release stands on its way
// down the tracks), halts, promotions and failure clears, for all four
// formatters. The labels and helpers are in firmware_tracks_format.go.

// firmwareCandidateLabel names the state of the release a track will take next.
func firmwareCandidateLabel(state string) string {
	switch state {
	case models.FirmwareCandidateReady:
		return "ready for the next window"
	case models.FirmwareCandidateSoaking:
		return "soaking"
	case models.FirmwareCandidateWaitingForPreviousTrack:
		return "waiting for the previous track"
	case models.FirmwareCandidateWaitingForEvidence:
		return "waiting for evidence"
	case models.FirmwareCandidateBlockedByFailure:
		return "blocked by a failure"
	default:
		return state
	}
}

func firmwareCandidateDisplay(state string) string {
	label := firmwareCandidateLabel(state)
	switch state {
	case models.FirmwareCandidateReady:
		return ColorSuccess.Sprint(label)
	case models.FirmwareCandidateSoaking:
		return ColorInProgress.Sprint(label)
	case models.FirmwareCandidateWaitingForPreviousTrack, models.FirmwareCandidateWaitingForEvidence:
		return ColorDim.Sprint(label)
	case models.FirmwareCandidateBlockedByFailure:
		return ColorError.Sprint(label)
	default:
		return label
	}
}

// firmwareStallText says why a release cannot reach a track without an
// operator, and what the operator can do about it.
func firmwareStallText(reason string) string {
	switch reason {
	case models.FirmwareStallEvidenceUnreachable:
		return "stalled: too few devices in the previous track can still reach it; add devices or lower min success"
	case models.FirmwareStallBlockedByFailure:
		return "stalled: an unresolved failure blocks it; see the failures, then 'ndcli firmware failure clear'"
	case "":
		return "stalled"
	default:
		return "stalled: " + reason
	}
}

// firmwareSoakLeft renders the server-computed soak left.
func firmwareSoakLeft(c *models.FirmwareCandidate) string {
	if c == nil || c.SoakRemainingSeconds == nil {
		return "-"
	}
	if *c.SoakRemainingSeconds <= 0 {
		return "over"
	}
	return firmwareDuration(time.Duration(*c.SoakRemainingSeconds) * time.Second)
}

func firmwareEvidenceCell(c *models.FirmwareCandidate) string {
	if c == nil || c.Evidence == nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d", c.Evidence.Reached, c.Evidence.Required)
}

func firmwareTargetCell(s models.FirmwareSeriesStatus) string {
	if s.Target == "" {
		return "-"
	}
	target := firmwareWithRevision(s.Target, s.TargetRevision)
	if s.TargetSource == "manual" {
		return target + " (manual)"
	}
	return target
}

// firmwareRolloutRow is one track in one series.
type firmwareRolloutRow struct {
	track     string
	series    string
	target    string
	next      string
	state     string
	candidate *models.FirmwareCandidate
	stall     string
}

// firmwareRolloutRows flattens the board into a row per track and series. A
// track with no series yet still gets its row: the board must show all three.
func firmwareRolloutRows(r *models.FirmwareRollout) []firmwareRolloutRow {
	var rows []firmwareRolloutRow
	for _, t := range r.Tracks {
		if len(t.Series) == 0 {
			rows = append(rows, firmwareRolloutRow{track: t.Track, series: "-", target: "-", next: "-", state: "-"})
			continue
		}
		for _, s := range t.Series {
			row := firmwareRolloutRow{track: t.Track, series: s.Series, target: firmwareTargetCell(s), next: "-", state: "-"}
			if c := s.NextCandidate; c != nil {
				row.next = firmwareWithRevision(c.Version, c.Revision)
				row.state = c.State
				row.candidate = c
				if c.Stalled {
					row.stall = firmwareStallText(c.StallReason)
				}
			}
			if note := firmwareLatestNote(s); note != "" {
				if row.stall != "" {
					row.stall += "; "
				}
				row.stall += note
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func firmwareRolloutHeader(r *models.FirmwareRollout) []firmwareField {
	return []firmwareField{
		{"Organization", r.OrganizationName},
		{"Firmware", r.Status},
		{"Plan", firmwarePlanCell(r.PlanEnabled)},
	}
}

func firmwareFailureLine(fl models.FirmwareFailure) string {
	return fmt.Sprintf("%s in %s: %s — %s, last %s", firmwareWithRevision(fl.Version, fl.Revision), fl.Track,
		firmwarePlural(fl.Count, "failure", "failures"), strings.Join(fl.Devices, ", "), firmwareTimestampShort(fl.LastAt))
}

func firmwareHaltStatus(h models.FirmwareHalt) string {
	if h.UnhaltedAt == "" {
		return "in force"
	}
	status := "lifted " + firmwareTimestampShort(h.UnhaltedAt)
	if h.UnhaltedBy != "" {
		status += " by " + h.UnhaltedBy
	}
	return status
}

// firmwareHaltSubject names what a halt is on, in a sentence: "release 26.7.5",
// "major upgrade to 26.7".
func firmwareHaltSubject(h models.FirmwareHalt) string {
	if h.IsMajor() {
		return "major upgrade to " + h.Name()
	}
	return "release " + firmwareHaltLabel(h)
}

// firmwareHaltTitle titles the box of a halt. A release keeps the title it
// always had; a major upgrade is named as one, because its series is also the
// name of the GA release of that series.
func firmwareHaltTitle(h models.FirmwareHalt) string {
	if h.IsMajor() {
		return "Halt on major upgrade to " + h.Name()
	}
	return "Halt on " + firmwareHaltLabel(h)
}

func firmwareHaltFields(h models.FirmwareHalt) []firmwareField {
	label := "Release"
	if h.IsMajor() {
		label = "Major upgrade"
	}
	return []firmwareField{
		{label, firmwareHaltLabel(h)},
		{"Status", firmwareHaltStatus(h)},
		{"Halted", firmwareTimestamp(h.HaltedAt) + " by " + h.HaltedBy},
		{"Reason", h.Reason},
	}
}

func firmwareHaltsTable(f BaseFormatter, halts []models.FirmwareHalt) {
	// A list that holds a halt of a major upgrade says which is which; one of
	// releases only is what it always was.
	withKind := false
	for _, h := range halts {
		withKind = withKind || h.IsMajor()
	}
	headers := []string{"Release", "Status", "Halted", "By", "Reason"}
	if withKind {
		headers = []string{"Kind", "Target", "Status", "Halted", "By", "Reason"}
	}
	table := NewStyledTable(headers)
	for _, h := range halts {
		row := []string{firmwareHaltLabel(h), firmwareHaltStatus(h), firmwareTimestampShort(h.HaltedAt), h.HaltedBy, h.Reason}
		if withKind {
			kind := models.FirmwareKindMinor
			if h.IsMajor() {
				kind = models.FirmwareKindMajor
			}
			row = append([]string{kind}, row...)
		}
		table.Append(row)
	}
	table.RenderTo(f.Writer)
}

// FormatFirmwareRollout renders where every release stands on the conveyor.
func (f *TableFormatter) FormatFirmwareRollout(r *models.FirmwareRollout) error {
	writeFirmwareFields(f.BaseFormatter, firmwareRolloutHeader(r), "")
	fmt.Fprintln(f.Writer)
	rows := firmwareRolloutRows(r)
	if len(rows) > 0 {
		table := NewStyledTable([]string{"Track", "Series", "Target", "Next", "State", "Soak Left", "Evidence", "Notes"})
		for _, row := range rows {
			table.Append([]string{
				row.track, row.series, row.target, row.next, firmwareCandidateDisplay(row.state),
				firmwareSoakLeft(row.candidate), firmwareEvidenceCell(row.candidate), row.stall,
			})
		}
		table.RenderTo(f.Writer)
	}
	fmt.Fprintln(f.Writer)
	if len(r.Failures) == 0 {
		fmt.Fprintln(f.Writer, "Failures: none")
	} else {
		table := NewStyledTable([]string{"Release", "Track", "Devices", "Count", "Last"})
		for _, fl := range r.Failures {
			table.Append([]string{firmwareWithRevision(fl.Version, fl.Revision), fl.Track, strings.Join(fl.Devices, ", "), strconv.Itoa(fl.Count), firmwareTimestampShort(fl.LastAt)})
		}
		fmt.Fprintln(f.Writer, "Failures blocking a release:")
		table.RenderTo(f.Writer)
	}
	fmt.Fprintln(f.Writer)
	if len(r.Halts) == 0 {
		fmt.Fprintln(f.Writer, "Halts: none")
	} else {
		fmt.Fprintln(f.Writer, "Halts in force:")
		firmwareHaltsTable(f.BaseFormatter, r.Halts)
	}
	writeFirmwareMajorsTable(f.BaseFormatter, r)
	return nil
}

func (f *SimpleFormatter) FormatFirmwareRollout(r *models.FirmwareRollout) error {
	fmt.Fprintf(f.Writer, "Firmware rollout: %s [%s]\n", r.OrganizationName, r.Status)
	if !r.PlanEnabled {
		f.Warning("The plan does not include firmware tracks: " + firmwarePlanOff)
	}
	for _, t := range r.Tracks {
		fmt.Fprintf(f.Writer, "  %s: soak %s, min success %d, schedule %s, %s\n", t.Track, firmwareSoak(t.SoakDays),
			t.MinSuccess, firmwareScheduleCell(models.FirmwareTrack{Schedule: t.Schedule, Scheduled: t.Scheduled}),
			firmwareDevicesPhrase(t.Devices))
		if len(t.Series) == 0 {
			fmt.Fprintln(f.Writer, "    no release in play yet")
		}
		for _, s := range t.Series {
			line := fmt.Sprintf("    %s: target %s", s.Series, firmwareTargetCell(s))
			if c := s.NextCandidate; c != nil {
				line += fmt.Sprintf("; next %s %s", firmwareWithRevision(c.Version, c.Revision), firmwareCandidateLabel(c.State))
				if c.SoakRemainingSeconds != nil {
					line += ", soak left " + firmwareSoakLeft(c)
				}
				if c.Evidence != nil {
					line += fmt.Sprintf(", evidence %d/%d", c.Evidence.Reached, c.Evidence.Required)
				}
				if c.Stalled {
					line += " — " + firmwareStallText(c.StallReason)
				}
			}
			if note := firmwareLatestNote(s); note != "" {
				line += "; " + note
			}
			fmt.Fprintln(f.Writer, line)
		}
	}
	if len(r.Failures) > 0 {
		fmt.Fprintln(f.Writer, "  Failures blocking a release:")
		for _, fl := range r.Failures {
			fmt.Fprintf(f.Writer, "    • %s\n", firmwareFailureLine(fl))
		}
	}
	if len(r.Halts) > 0 {
		fmt.Fprintln(f.Writer, "  Halts in force:")
		for _, h := range r.Halts {
			fmt.Fprintf(f.Writer, "    • %s since %s by %s: %s\n", firmwareHaltLabel(h), firmwareTimestampShort(h.HaltedAt), h.HaltedBy, h.Reason)
		}
	}
	writeFirmwareMajorsSimple(f.BaseFormatter, r)
	return nil
}

func (f *DetailedFormatter) FormatFirmwareRollout(r *models.FirmwareRollout) error {
	firmwareFieldBox("Firmware rollout", firmwareRolloutHeader(r)).render(f.BaseFormatter)
	for _, t := range r.Tracks {
		fmt.Fprintln(f.Writer)
		trackFields := []firmwareField{
			{"Soak", firmwareSoak(t.SoakDays) + ", " + firmwareSoakNote(t.Track)},
			{"Min success", strconv.Itoa(t.MinSuccess) + ", " + firmwareMinSuccessNote(t.Track, t.MinSuccess)},
			{"Schedule", firmwareScheduleCell(models.FirmwareTrack{Schedule: t.Schedule, Scheduled: t.Scheduled})},
			{"Devices", firmwareDeviceCounts(t.Devices)},
		}
		if len(t.Series) == 0 {
			trackFields = append(trackFields, firmwareField{"Releases", "none in play yet"})
		}
		for _, s := range t.Series {
			trackFields = append(trackFields, firmwareField{"Series " + s.Series, "target " + firmwareTargetCell(s)})
			if s.TargetApprovedAt != "" {
				trackFields = append(trackFields, firmwareField{"  approved", firmwareTimestamp(s.TargetApprovedAt)})
			}
			if note := firmwareLatestNote(s); note != "" {
				trackFields = append(trackFields, firmwareField{"  latest", strings.TrimPrefix(note, "latest ")})
			}
			if c := s.NextCandidate; c != nil {
				trackFields = append(trackFields, firmwareField{"  next", firmwareWithRevision(c.Version, c.Revision) + ": " + firmwareCandidateDisplay(c.State)})
				if c.SoakEndsAt != "" {
					trackFields = append(trackFields, firmwareField{"  soak ends", firmwareTimestamp(c.SoakEndsAt) + " (left: " + firmwareSoakLeft(c) + ")"})
				}
				if c.Evidence != nil {
					trackFields = append(trackFields, firmwareField{"  evidence", fmt.Sprintf("%d of %d devices of the previous track", c.Evidence.Reached, c.Evidence.Required)})
				}
				if c.Stalled {
					trackFields = append(trackFields, firmwareField{"  stalled", strings.TrimPrefix(firmwareStallText(c.StallReason), "stalled: ")})
				}
			}
		}
		firmwareFieldBox("Track "+t.Track, trackFields).render(f.BaseFormatter)
	}
	for _, fl := range r.Failures {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("Failure blocking "+firmwareWithRevision(fl.Version, fl.Revision), []firmwareField{
			{"Track", fl.Track},
			{"Devices", strings.Join(fl.Devices, ", ")},
			{"Failures", strconv.Itoa(fl.Count)},
			{"Last", firmwareTimestamp(fl.LastAt)},
		}).render(f.BaseFormatter)
	}
	for _, h := range r.Halts {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox(firmwareHaltTitle(h), firmwareHaltFields(h)[1:]).render(f.BaseFormatter)
	}
	writeFirmwareMajorsDetailed(f.BaseFormatter, r)
	return nil
}

func (f *JSONFormatter) FormatFirmwareRollout(r *models.FirmwareRollout) error {
	return f.output(r)
}

// FormatFirmwareHalts renders a page of halts.
func (f *TableFormatter) FormatFirmwareHalts(list *models.FirmwareHaltList) error {
	if len(list.Items) == 0 {
		f.Info("No halts found")
		return nil
	}
	firmwareHaltsTable(f.BaseFormatter, list.Items)
	fmt.Fprintf(f.Writer, "\nTotal: %d halt(s)\n", list.Total)
	return nil
}

func (f *SimpleFormatter) FormatFirmwareHalts(list *models.FirmwareHaltList) error {
	if len(list.Items) == 0 {
		f.Info("No halts found")
		return nil
	}
	for _, h := range list.Items {
		fmt.Fprintf(f.Writer, "• %s [%s] halted %s by %s: %s\n", firmwareHaltSubject(h), firmwareHaltStatus(h), firmwareTimestampShort(h.HaltedAt), h.HaltedBy, h.Reason)
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwareHalts(list *models.FirmwareHaltList) error {
	if len(list.Items) == 0 {
		f.Info("No halts found")
		return nil
	}
	for i, h := range list.Items {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		firmwareFieldBox(firmwareHaltTitle(h), firmwareHaltFields(h)[1:]).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwareHalts(list *models.FirmwareHaltList) error {
	return f.output(list)
}

// firmwareHaltHeadline says what halting, or lifting a halt on, a release did.
func firmwareHaltHeadline(r *models.FirmwareHaltResult) string {
	if r.Halt.UnhaltedAt != "" {
		return fmt.Sprintf("Halt on %s lifted: it competes for the tracks again", firmwareHaltSubject(r.Halt))
	}
	head := fmt.Sprintf("Release %s halted: no track will approve it; devices already on it stay on it", firmwareHaltLabel(r.Halt))
	if r.Halt.IsMajor() {
		head = fmt.Sprintf("Major upgrade to %s halted: no track will approve it; devices already upgrading are left alone and devices already on the series stay on it", r.Halt.Series)
	}
	if r.CancelledTasks > 0 {
		head += fmt.Sprintf(" (%s cancelled)", firmwarePlural(r.CancelledTasks, "pending step", "pending steps"))
	}
	return head
}

// FormatFirmwareHalt renders the result of halting a release or lifting a halt.
func (f *TableFormatter) FormatFirmwareHalt(r *models.FirmwareHaltResult) error {
	f.Success(firmwareHaltHeadline(r))
	writeFirmwareFields(f.BaseFormatter, firmwareHaltFields(r.Halt)[1:], "")
	return nil
}

func (f *SimpleFormatter) FormatFirmwareHalt(r *models.FirmwareHaltResult) error {
	f.Success(firmwareHaltHeadline(r))
	writeFirmwareFields(f.BaseFormatter, firmwareHaltFields(r.Halt)[1:], "  ")
	return nil
}

func (f *DetailedFormatter) FormatFirmwareHalt(r *models.FirmwareHaltResult) error {
	f.Success(firmwareHaltHeadline(r))
	fmt.Fprintln(f.Writer)
	firmwareFieldBox(firmwareHaltTitle(r.Halt), firmwareHaltFields(r.Halt)[1:]).render(f.BaseFormatter)
	return nil
}

func (f *JSONFormatter) FormatFirmwareHalt(r *models.FirmwareHaltResult) error {
	return f.output(r)
}

func firmwarePromotionResultText(result string) string {
	switch result {
	case models.FirmwarePromotionApproved:
		return "approved now"
	case models.FirmwarePromotionAlreadyApproved:
		return "already approved"
	default:
		return result
	}
}

func firmwarePromotionHeadline(p *models.FirmwarePromotion) string {
	var approved []string
	for _, r := range p.Results {
		if r.Result == models.FirmwarePromotionApproved {
			approved = append(approved, r.Track)
		}
	}
	if p.IsMajor() {
		if len(approved) == 0 {
			return fmt.Sprintf("Major upgrade to %s was already approved in every track asked", p.Series)
		}
		return fmt.Sprintf("Major upgrade to %s approved in %s without waiting for its soak; a track with major upgrades on "+
			"upgrades its eligible devices at its next window", p.Series, strings.Join(approved, ", "))
	}
	if len(approved) == 0 {
		return fmt.Sprintf("Release %s was already approved in every track asked", firmwareWithRevision(p.Version, p.Revision))
	}
	return fmt.Sprintf("Release %s approved in %s without waiting for its soak", firmwareWithRevision(p.Version, p.Revision), strings.Join(approved, ", "))
}

// firmwarePromotionTargetLabel names what a promotion result's target is, as a
// table header and as a field. For a release it is the track's target for the
// release's series afterwards; for a major upgrade it is the series itself, which
// is not a target the track moves to but what was approved.
func firmwarePromotionTargetLabel(p *models.FirmwarePromotion) (header, field string) {
	if p.IsMajor() {
		return "Series", "Series"
	}
	return "Target Now", "Target now"
}

// FormatFirmwarePromotion renders what promoting a release did in each track.
func (f *TableFormatter) FormatFirmwarePromotion(p *models.FirmwarePromotion) error {
	f.Success(firmwarePromotionHeadline(p))
	if len(p.Results) > 0 {
		header, _ := firmwarePromotionTargetLabel(p)
		table := NewStyledTable([]string{"Track", "Result", header})
		for _, r := range p.Results {
			table.Append([]string{r.Track, firmwarePromotionResultText(r.Result), firmwareDashIfEmpty(firmwareWithRevision(r.Target, r.TargetRevision))})
		}
		table.RenderTo(f.Writer)
	}
	return nil
}

func (f *SimpleFormatter) FormatFirmwarePromotion(p *models.FirmwarePromotion) error {
	f.Success(firmwarePromotionHeadline(p))
	_, field := firmwarePromotionTargetLabel(p)
	for _, r := range p.Results {
		fmt.Fprintf(f.Writer, "• %s: %s (%s %s)\n", r.Track, firmwarePromotionResultText(r.Result),
			strings.ToLower(field), firmwareDashIfEmpty(firmwareWithRevision(r.Target, r.TargetRevision)))
	}
	return nil
}

func (f *DetailedFormatter) FormatFirmwarePromotion(p *models.FirmwarePromotion) error {
	f.Success(firmwarePromotionHeadline(p))
	_, field := firmwarePromotionTargetLabel(p)
	for _, r := range p.Results {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("Track "+r.Track, []firmwareField{
			{"Result", firmwarePromotionResultText(r.Result)},
			{field, firmwareDashIfEmpty(firmwareWithRevision(r.Target, r.TargetRevision))},
		}).render(f.BaseFormatter)
	}
	return nil
}

func (f *JSONFormatter) FormatFirmwarePromotion(p *models.FirmwarePromotion) error {
	return f.output(p)
}

func firmwareClearedHeadline(c *models.FirmwareFailuresCleared) string {
	scope := "every track"
	if c.Track != "" {
		scope = "the " + c.Track + " track"
	}
	subject := "release " + firmwareWithRevision(c.Version, c.Revision)
	if c.IsMajor() {
		subject = "the major upgrade to " + c.Series
	}
	if c.Cleared == 0 {
		return fmt.Sprintf("No recorded failure was blocking %s in %s", subject, scope)
	}
	return fmt.Sprintf("Cleared %s blocking %s in %s; the records stay in the history",
		firmwarePlural(c.Cleared, "failure", "failures"), subject, scope)
}

// FormatFirmwareFailuresCleared reports the failures cleared for a release.
func (f *TableFormatter) FormatFirmwareFailuresCleared(c *models.FirmwareFailuresCleared) error {
	f.Success(firmwareClearedHeadline(c))
	return nil
}

func (f *SimpleFormatter) FormatFirmwareFailuresCleared(c *models.FirmwareFailuresCleared) error {
	f.Success(firmwareClearedHeadline(c))
	return nil
}

func (f *DetailedFormatter) FormatFirmwareFailuresCleared(c *models.FirmwareFailuresCleared) error {
	f.Success(firmwareClearedHeadline(c))
	return nil
}

func (f *JSONFormatter) FormatFirmwareFailuresCleared(c *models.FirmwareFailuresCleared) error {
	return f.output(c)
}
