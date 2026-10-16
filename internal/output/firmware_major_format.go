package output

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Firmware tracks: the major upgrades (a move to the next OPNsense series) on the
// rollout board, for the three human formatters. The board's strings are the
// closed, lowercase sets of models/firmware.go; one this build does not know is
// shown as itself, never as an error. JSON is the model.

// firmwareMajorStateLabel is the short label of one major upgrade in one
// track. An approved series whose track has the switch off is "approved, not
// applied": the conveyor moved it, and no device is asked to upgrade.
func firmwareMajorStateLabel(e models.FirmwareMajorEntry) string {
	switch e.State {
	case models.FirmwareMajorPathClosed:
		return "path closed"
	case models.FirmwareMajorHalted:
		return "halted"
	case models.FirmwareMajorApproved:
		if !e.AutoApplyMajor {
			return "approved, not applied"
		}
		return "approved"
	default:
		return firmwareCandidateLabel(e.State)
	}
}

func firmwareMajorStateDisplay(e models.FirmwareMajorEntry) string {
	label := firmwareMajorStateLabel(e)
	switch e.State {
	case models.FirmwareMajorApproved:
		if !e.AutoApplyMajor {
			return ColorDim.Sprint(label)
		}
		return ColorSuccess.Sprint(label)
	case models.FirmwareCandidateReady:
		return ColorSuccess.Sprint(label)
	case models.FirmwareCandidateSoaking:
		return ColorInProgress.Sprint(label)
	case models.FirmwareCandidateWaitingForPreviousTrack, models.FirmwareCandidateWaitingForEvidence:
		return ColorDim.Sprint(label)
	case models.FirmwareMajorPathClosed, models.FirmwareMajorHalted:
		return ColorWarning.Sprint(label)
	case models.FirmwareCandidateBlockedByFailure:
		return ColorError.Sprint(label)
	default:
		return label
	}
}

// firmwareSeriesMove is "26.1 → 26.7", or the target alone when the source is
// not known.
func firmwareSeriesMove(source, target string) string {
	if source == "" {
		return target
	}
	return source + " → " + target
}

func firmwareSecondsLeft(seconds *int) string {
	if seconds == nil {
		return "-"
	}
	if *seconds <= 0 {
		return "over"
	}
	return firmwareDuration(time.Duration(*seconds) * time.Second)
}

func firmwareEvidenceOf(e *models.FirmwareEvidence) string {
	if e == nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d", e.Reached, e.Required)
}

// firmwareMajorNoticeText says what a notice on a major upgrade means.
func firmwareMajorNoticeText(notice string) string {
	if notice == "payload_changed" {
		return "the upgrade payload was replaced upstream after this track approved the series"
	}
	return notice
}

// firmwareMajorStall says why a series cannot reach a track without an
// operator. A stall for want of evidence names its cause when the board does
// (the previous track has no device, applies no upgrades, or has none that can
// upgrade); a failure points at the command that clears it.
func firmwareMajorStall(e models.FirmwareMajorEntry) string {
	if !e.Stalled {
		return ""
	}
	switch e.StallReason {
	case models.FirmwareStallEvidenceUnreachable:
		if e.BlockedReason != "" {
			return "stalled: " + models.FirmwareMajorBlockedReasonText(e.BlockedReason)
		}
		return firmwareStallText(e.StallReason)
	case models.FirmwareStallBlockedByFailure:
		return "stalled: a failed major upgrade blocks it; see the failures, then 'ndcli firmware failure clear " + e.Series + " --major'"
	default:
		return firmwareStallText(e.StallReason)
	}
}

// firmwareMajorNotes are the lines a major upgrade earns beside its numbers:
// the stall, what blocks it, the devices that wait, and a notice.
func firmwareMajorNotes(e models.FirmwareMajorEntry) []string {
	var notes []string
	stall := firmwareMajorStall(e)
	if stall != "" {
		notes = append(notes, stall)
	}
	// A stall for want of evidence already says its cause in the words of the
	// blocked reason; saying it twice helps nobody.
	stallNamesTheCause := e.Stalled && e.StallReason == models.FirmwareStallEvidenceUnreachable
	if e.BlockedReason != "" && !stallNamesTheCause {
		notes = append(notes, "blocked: "+models.FirmwareMajorBlockedReasonText(e.BlockedReason))
	}
	if e.DevicesAwaitingMinorEvidence > 0 {
		notes = append(notes, firmwarePlural(e.DevicesAwaitingMinorEvidence, "device", "devices")+
			" waiting for the ordinary window that records the final release")
	}
	if e.OfferNotSeen > 0 {
		notes = append(notes, firmwarePlural(e.OfferNotSeen, "device", "devices")+" whose mirror does not offer the upgrade yet")
	}
	if e.Notice != "" {
		notes = append(notes, firmwareMajorNoticeText(e.Notice))
	}
	return notes
}

// firmwareMajorApproval is when and how the track approved the series.
func firmwareMajorApproval(e models.FirmwareMajorEntry) string {
	if e.ApprovedAt == "" {
		return ""
	}
	text := firmwareTimestampShort(e.ApprovedAt)
	if e.ApprovalSource != "" {
		text += " (" + e.ApprovalSource + ")"
	}
	return text
}

// firmwareMajorCounts is a track's devices in the terms of the major upgrade.
func firmwareMajorCounts(e models.FirmwareMajorEntry) string {
	return fmt.Sprintf("%d eligible, %d on the series, %d upgraded", e.DevicesEligible, e.DevicesOnSeries, e.DevicesHopped)
}

// firmwareEndOfLifeSeries lists the source series OPNsense no longer updates
// while the upgrade path from them is open, once each, in board order.
func firmwareEndOfLifeSeries(majors []models.FirmwareMajorEntry) []string {
	var out []string
	for _, e := range majors {
		if !e.SourceSeriesEndOfLife {
			continue
		}
		source := e.SourceSeries
		if source == "" {
			source = "the source series"
		}
		if !slices.Contains(out, source) {
			out = append(out, source)
		}
	}
	return out
}

func firmwareEndOfLifeText(series string) string {
	return "OPNsense gives " + series + " no more updates while the upgrade path from it is open"
}

// firmwareAgentRepoText is the control plane's check of NetDefense's own package
// repository: after an upgrade across a FreeBSD version a device's NetDefense
// repository names the new version's tree, and a missing one fails every
// OPNsense check of the device until someone fixes it. withTime adds when each
// tree was last checked.
func firmwareAgentRepoText(r *models.FirmwareAgentRepo, withTime bool) string {
	if !r.Configured {
		return "not checked: the control plane has no channel configured for it"
	}
	channel := r.Channel
	if channel == "" {
		channel = "the configured channel"
	}
	if len(r.Trees) == 0 {
		return channel + ": no result in the last 30 minutes"
	}
	trees := make([]string, 0, len(r.Trees))
	for _, t := range r.Trees {
		state := ColorSuccess.Sprint("ok")
		if !t.OK {
			state = ColorError.Sprint("missing")
		}
		text := t.ABI + " " + state
		if withTime && t.CheckedAt != "" {
			text += " (checked " + firmwareTimestampShort(t.CheckedAt) + ")"
		}
		trees = append(trees, text)
	}
	return channel + ": " + strings.Join(trees, ", ")
}

// firmwareMajorTable draws the major upgrades of the board.
func firmwareMajorTable(f BaseFormatter, majors []models.FirmwareMajorEntry) {
	table := NewStyledTable([]string{"Track", "Series", "State", "Soak Left", "Evidence", "Apply", "Eligible", "On Series", "Upgraded", "Notes"})
	for _, e := range majors {
		table.Append([]string{
			e.Track,
			firmwareSeriesMove(e.SourceSeries, e.Series),
			firmwareMajorStateDisplay(e),
			firmwareSecondsLeft(e.SoakRemainingSeconds),
			firmwareEvidenceOf(e.Evidence),
			firmwareOnOff(e.AutoApplyMajor),
			strconv.Itoa(e.DevicesEligible),
			strconv.Itoa(e.DevicesOnSeries),
			strconv.Itoa(e.DevicesHopped),
			strings.Join(firmwareMajorNotes(e), "; "),
		})
	}
	table.RenderTo(f.Writer)
}

func firmwareMajorFailureLine(fl models.FirmwareMajorFailure) string {
	return fmt.Sprintf("%s in %s: %s — %s, last %s", fl.Series, fl.Track,
		firmwarePlural(fl.Count, "failure", "failures"), strings.Join(fl.Devices, ", "), firmwareTimestampShort(fl.LastAt))
}

// firmwareMajorClearHint says how a failed major upgrade is cleared.
const firmwareMajorClearHint = "A failed major upgrade blocks the series in the later tracks only; clear it with 'ndcli firmware failure clear <series> --major'."

// writeFirmwareMajorsTable prints the major upgrades, their failures and the
// check of NetDefense's repository as the table format shows them. It prints
// nothing for a board that has none of the three (a control plane that predates
// major upgrades sends none).
func writeFirmwareMajorsTable(f BaseFormatter, r *models.FirmwareRollout) {
	if len(r.Majors) > 0 {
		fmt.Fprintln(f.Writer)
		fmt.Fprintln(f.Writer, "Major upgrades:")
		firmwareMajorTable(f, r.Majors)
		for _, series := range firmwareEndOfLifeSeries(r.Majors) {
			fmt.Fprintln(f.Writer, firmwareEndOfLifeText(series))
		}
	}
	if len(r.MajorFailures) > 0 {
		fmt.Fprintln(f.Writer)
		fmt.Fprintln(f.Writer, "Failures blocking a major upgrade:")
		table := NewStyledTable([]string{"Series", "Track", "Devices", "Count", "Last"})
		for _, fl := range r.MajorFailures {
			table.Append([]string{fl.Series, fl.Track, strings.Join(fl.Devices, ", "), strconv.Itoa(fl.Count), firmwareTimestampShort(fl.LastAt)})
		}
		table.RenderTo(f.Writer)
		fmt.Fprintln(f.Writer, firmwareMajorClearHint)
	} else if len(r.Majors) > 0 {
		fmt.Fprintln(f.Writer)
		fmt.Fprintln(f.Writer, "Major upgrade failures: none")
	}
	if r.AgentRepo != nil {
		fmt.Fprintln(f.Writer)
		fmt.Fprintf(f.Writer, "NetDefense repository: %s\n", firmwareAgentRepoText(r.AgentRepo, false))
	}
}

// writeFirmwareMajorsSimple is writeFirmwareMajorsTable's twin for the simple
// format.
func writeFirmwareMajorsSimple(f BaseFormatter, r *models.FirmwareRollout) {
	if len(r.Majors) > 0 {
		fmt.Fprintln(f.Writer, "  Major upgrades:")
		for _, e := range r.Majors {
			line := fmt.Sprintf("    %s: %s %s", e.Track, firmwareSeriesMove(e.SourceSeries, e.Series), firmwareMajorStateLabel(e))
			if approval := firmwareMajorApproval(e); approval != "" {
				line += " " + approval
			}
			if e.State != models.FirmwareMajorApproved {
				line += fmt.Sprintf(", soak left %s, evidence %s", firmwareSecondsLeft(e.SoakRemainingSeconds), firmwareEvidenceOf(e.Evidence))
			}
			line += "; apply " + firmwareOnOff(e.AutoApplyMajor) + "; " + firmwareMajorCounts(e)
			fmt.Fprintln(f.Writer, line)
			for _, note := range firmwareMajorNotes(e) {
				fmt.Fprintf(f.Writer, "      note: %s\n", note)
			}
		}
		for _, series := range firmwareEndOfLifeSeries(r.Majors) {
			fmt.Fprintf(f.Writer, "    note: %s\n", firmwareEndOfLifeText(series))
		}
	}
	if len(r.MajorFailures) > 0 {
		fmt.Fprintln(f.Writer, "  Failures blocking a major upgrade:")
		for _, fl := range r.MajorFailures {
			fmt.Fprintf(f.Writer, "    • %s\n", firmwareMajorFailureLine(fl))
		}
	}
	if r.AgentRepo != nil {
		fmt.Fprintf(f.Writer, "  NetDefense repository: %s\n", firmwareAgentRepoText(r.AgentRepo, false))
	}
}

// writeFirmwareMajorsDetailed is writeFirmwareMajorsTable's twin for the
// detailed format: a box for each major upgrade and each failure.
func writeFirmwareMajorsDetailed(f BaseFormatter, r *models.FirmwareRollout) {
	for _, e := range r.Majors {
		fields := []firmwareField{
			{"State", firmwareMajorStateDisplay(e)},
			{"Apply", firmwareMajorSwitchNote(e.AutoApplyMajor)},
		}
		if e.SoakEndsAt != "" {
			fields = append(fields, firmwareField{"Soak ends", firmwareTimestamp(e.SoakEndsAt) + " (left: " + firmwareSecondsLeft(e.SoakRemainingSeconds) + ")"})
		}
		if e.Evidence != nil {
			fields = append(fields, firmwareField{"Evidence", fmt.Sprintf("%d of %d devices of the previous track", e.Evidence.Reached, e.Evidence.Required)})
		}
		if e.UpgradeOpenAt != "" {
			fields = append(fields, firmwareField{"Path opened", firmwareTimestamp(e.UpgradeOpenAt)})
		}
		if approval := firmwareMajorApproval(e); approval != "" {
			fields = append(fields, firmwareField{"Approved", approval})
		}
		fields = append(fields, firmwareField{"Devices", firmwareMajorCounts(e)})
		for _, note := range firmwareMajorNotes(e) {
			fields = append(fields, firmwareField{"Note", note})
		}
		if e.SourceSeriesEndOfLife {
			source := e.SourceSeries
			if source == "" {
				source = "the source series"
			}
			fields = append(fields, firmwareField{"Note", firmwareEndOfLifeText(source)})
		}
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("Major upgrade "+e.Track+": "+firmwareSeriesMove(e.SourceSeries, e.Series), fields).render(f)
	}
	for _, fl := range r.MajorFailures {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("Failure blocking major upgrade "+fl.Series, []firmwareField{
			{"Track", fl.Track},
			{"Devices", strings.Join(fl.Devices, ", ")},
			{"Failures", strconv.Itoa(fl.Count)},
			{"Last", firmwareTimestamp(fl.LastAt)},
			{"Clear", "'ndcli firmware failure clear " + fl.Series + " --major'"},
		}).render(f)
	}
	if r.AgentRepo != nil {
		fmt.Fprintln(f.Writer)
		firmwareFieldBox("NetDefense repository", []firmwareField{
			{"Check", firmwareAgentRepoText(r.AgentRepo, true)},
		}).render(f)
	}
}
