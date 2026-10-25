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
