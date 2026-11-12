package output

import (
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// firmwareWithRevision is "26.1.11 rev 2" when a revision is named, else the
// version as it is.
func firmwareWithRevision(version string, revision *int) string {
	if version == "" || revision == nil {
		return version
	}
	return fmt.Sprintf("%s rev %d", version, *revision)
}

// firmwareHistoryTarget is a ledger step's target with the revision it was
// sealed with, and the one the device reported when that differs: "26.1.11 rev
// 2, device reported rev 3" is a held_revision_mismatch hold.
func firmwareHistoryTarget(item *models.FirmwareHistoryItem) string {
	target := firmwareWithRevision(item.Target, item.Revision)
	if mismatch := firmwareReportedRevision(item); mismatch != "" {
		return target + ", " + mismatch
	}
	return target
}

// firmwareReportedRevision is "device reported rev 3" when the step was sealed
// with a revision and the device's answer named another, else "".
func firmwareReportedRevision(item *models.FirmwareHistoryItem) string {
	sealed, reported := item.Revision, item.ReportedRevision
	if item.Target == "" || sealed == nil || reported == nil || *sealed == *reported {
		return ""
	}
	return fmt.Sprintf("device reported rev %d", *reported)
}

// firmwarePendingText says how many package updates a device's last check found;
// the hotfix label means installing them would not change the OPNsense version.
func firmwarePendingText(p *models.FirmwarePending) string {
	if p == nil || p.Count <= 0 {
		return ""
	}
	count := firmwarePlural(p.Count, "package update", "package updates")
	if p.Kind == models.FirmwarePendingHotfix {
		return "Hotfix: " + count + " pending"
	}
	return count + " pending"
}

// firmwareHotfixWarningText says a device is pinned below a release that was
// revised after it appeared.
func firmwareHotfixWarningText(w *models.FirmwareHotfixWarning) string {
	if w == nil {
		return ""
	}
	text := fmt.Sprintf("pinned below release %s, whose revision %d is a hotfix", w.Release, w.Revision)
	if w.ApprovedInTrack {
		return text + "; the track approved it"
	}
	return text + "; the track has not approved it (promote it to move the device up)"
}

// firmwareLatestNote names the newest release of a series when it is not what
// the track targets.
func firmwareLatestNote(s models.FirmwareSeriesStatus) string {
	l := s.LatestRelease
	if l == nil || (l.Version == s.Target && (s.TargetRevision == nil || *s.TargetRevision == l.LatestRevision)) {
		return ""
	}
	return "latest " + firmwareWithRevision(l.Version, &l.LatestRevision)
}

// firmwareHaltLabel is what a halt addresses, with the revision when only one
// is halted.
func firmwareHaltLabel(h models.FirmwareHalt) string {
	if h.IsMajor() {
		return h.Name()
	}
	return firmwareWithRevision(h.Name(), h.Revision)
}
