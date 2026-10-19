package output

import (
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The result of a FIRMWARE_TRACK task, rendered wherever a task is described or
// listed. The agent answers a COMPLETED task with a JSON block in the task's
// message; a task still pending carries the sentence the control plane wrote
// and a failed one the agent's plain-text error, and those render as the
// message they are. models.ParseFirmwareTrackResult decodes (and re-sanitizes)
// the block; this file only lays it out.

// firmwareTrackResultFor returns the result block of a FIRMWARE_TRACK task, or
// nil for any other task or a message that is not a result.
func firmwareTrackResultFor(task *models.Task) *models.FirmwareTrackResult {
	if task == nil || task.Type != models.TaskTypeFirmwareTrack {
		return nil
	}
	return models.ParseFirmwareTrackResult(task.Message)
}

// firmwareTrackResultLines lays a result out as "Label: value" lines: what was
// done, how it ended and why, the releases it moved between, whether it was a
// major upgrade and what the agent noted on the way, and whether the pin had
// been changed outside NetDefense.
func firmwareTrackResultLines(r *models.FirmwareTrackResult) []string {
	if r == nil {
		return nil
	}
	line := func(label, value string) string { return fmt.Sprintf("%-10s %s", label+":", value) }
	lines := []string{
		line("Action", r.Action+" on the "+r.Track+" track"),
		line("Outcome", r.Outcome),
	}
	if r.Reason != "" {
		lines = append(lines, line("Reason", firmwareReasonFull(r.Reason)))
	}
	if r.FromVersion != "" || r.ToVersion != "" {
		lines = append(lines, line("Version", firmwareVersionMove(r.FromVersion, r.ToVersion)))
	}
	if r.TargetVersion != "" {
		lines = append(lines, line("Target", r.TargetVersion))
	}
	if r.IsMajor() {
		kind := "major upgrade"
		if r.TargetVersion != "" {
			kind += " to the " + r.TargetVersion + " series"
		}
		lines = append(lines, line("Kind", kind))
	}
	if r.PinnedVersion != "" {
		lines = append(lines, line("Pinned", r.PinnedVersion))
	}
	for _, token := range r.Notes {
		lines = append(lines, line("Note", models.FirmwareNoteText(token)))
	}
	drift := "no"
	if r.Drift {
		drift = "yes: the pin had been changed outside NetDefense"
	}
	lines = append(lines, line("Drift", drift))
	if r.ReinstallEntries > 0 {
		lines = append(lines, line("Reinstall", strconv.Itoa(r.ReinstallEntries)+" same-version reinstall entry listed by the check (harmless)"))
	}
	return lines
}

// firmwareTrackResultSummary is the one-line form a task list shows:
// "advance success 26.7.3 → 26.7.5", "advance held — a reboot is pending on
// the device". Empty when the task has no result block.
func firmwareTrackResultSummary(task *models.Task) string {
	r := firmwareTrackResultFor(task)
	if r == nil {
		return ""
	}
	summary := r.Action + " " + r.Outcome
	switch {
	case r.Action == models.FirmwareActionEnroll && r.Outcome == models.FirmwareOutcomeSuccess && r.PinnedVersion != "":
		summary += ", pinned " + r.PinnedVersion
	case r.FromVersion != "" || r.ToVersion != "":
		summary += " " + firmwareVersionMove(r.FromVersion, r.ToVersion)
	}
	if r.Reason != "" && r.Outcome != models.FirmwareOutcomeSuccess {
		summary += " — " + firmwareReasonCell(r.Reason)
	}
	if r.Drift {
		summary += " (pin drift)"
	}
	if r.IsMajor() {
		summary += " (major upgrade)"
	}
	return summary
}

// anyFirmwareTrackResult reports whether a page of tasks has a result worth a
// column of its own.
func anyFirmwareTrackResult(tasks []models.Task) bool {
	for i := range tasks {
		if firmwareTrackResultSummary(&tasks[i]) != "" {
			return true
		}
	}
	return false
}
