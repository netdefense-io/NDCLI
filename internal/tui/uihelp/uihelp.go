// Package uihelp holds small, pure presentation helpers shared by the TUI
// screens and resource descriptors. They are ports of the package-private
// helpers in internal/output (which writes to io.Writer and cannot be
// imported for string building) — kept behaviourally identical so the TUI
// and the CLI render the same shapes (durations, attention ranking, …).
package uihelp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// HumanDuration renders a seconds count into a short 12s / 4m / 2h / 3d
// string. Negative input renders as an em dash. Mirrors output.humanDuration.
func HumanDuration(sec int64) string {
	if sec < 0 {
		return "—"
	}
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh", sec/3600)
	default:
		return fmt.Sprintf("%dd", sec/86400)
	}
}

// HumanDurationPtr is the *int64 variant; nil renders as an em dash.
func HumanDurationPtr(sec *int64) string {
	if sec == nil {
		return "—"
	}
	return HumanDuration(*sec)
}

// OnlineLabel maps the tri-state online pointer to a human label. nil means
// the broker registry is unavailable; never reported as offline.
func OnlineLabel(b *bool) string {
	if b == nil {
		return "unknown"
	}
	if *b {
		return "online"
	}
	return "offline"
}

// StatusColorLabel surfaces the dashboard's status_color field for cells.
func StatusColorLabel(c string) string {
	if c == "" {
		return "—"
	}
	return c
}

// SortedAgentVersions returns the histogram sorted by count desc, version asc.
func SortedAgentVersions(items []models.DashboardAgentVersion) []models.DashboardAgentVersion {
	out := make([]models.DashboardAgentVersion, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// AttentionRank scores a compact row for sorting — higher = needs more
// attention. Mirrors output.attentionRank / NDWeb's FleetOverview.attentionRank.
func AttentionRank(row *models.DashboardCompactRow) int {
	r := 0
	switch row.StatusColor {
	case "offline":
		r += 1000
	case "stale":
		r += 500
	case "unknown":
		r += 200
	}
	switch row.Sync.State {
	case "error":
		r += 800
	case "drift":
		r += 100
	}
	if row.FirmwareMixedState {
		r += 50
	}
	if row.Telemetry != nil && row.Telemetry.HeavySummary != nil {
		h := row.Telemetry.HeavySummary
		if h.ServicesDown != nil && *h.ServicesDown > 0 {
			r += 300 + 10*(*h.ServicesDown)
		}
		if h.CertsExpired != nil && *h.CertsExpired > 0 {
			r += 600 + 20*(*h.CertsExpired)
		}
		if h.CertsExpiring30d != nil && *h.CertsExpiring30d > 0 {
			r += 200 + 5*(*h.CertsExpiring30d)
		}
		if h.PendingUpdates != nil && *h.PendingUpdates > 0 {
			r++
		}
	}
	return r
}

// SortedCompact returns the rows ordered by attention rank desc, then name asc.
func SortedCompact(rows []models.DashboardCompactRow) []models.DashboardCompactRow {
	out := make([]models.DashboardCompactRow, len(rows))
	copy(out, rows)
	sort.Slice(out, func(i, j int) bool {
		ri, rj := AttentionRank(&out[i]), AttentionRank(&out[j])
		if ri != rj {
			return ri > rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// AttentionTags renders the compact row's attention badges as a short
// comma-joined string. Empty when nothing is wrong.
func AttentionTags(row *models.DashboardCompactRow) string {
	tags := make([]string, 0, 5)
	if row.Sync.State == "error" {
		tags = append(tags, "sync-error")
	}
	if row.FirmwareMixedState {
		tags = append(tags, "fw-mixed-state")
	}
	if row.Telemetry == nil || row.Telemetry.HeavySummary == nil {
		return strings.Join(tags, ", ")
	}
	h := row.Telemetry.HeavySummary
	if h.ServicesDown != nil && *h.ServicesDown > 0 {
		tags = append(tags, fmt.Sprintf("%d svc-down", *h.ServicesDown))
	}
	if h.PendingUpdates != nil && *h.PendingUpdates > 0 {
		tags = append(tags, fmt.Sprintf("%d pkg", *h.PendingUpdates))
	}
	if h.CertsExpired != nil && *h.CertsExpired > 0 {
		tags = append(tags, fmt.Sprintf("%d cert-expired", *h.CertsExpired))
	}
	if h.CertsExpiring30d != nil && *h.CertsExpiring30d > 0 {
		tags = append(tags, fmt.Sprintf("%d cert-≤30d", *h.CertsExpiring30d))
	}
	return strings.Join(tags, ", ")
}

// DisksOneLine renders the disks slice as a compact "/ 4% · /var 12%" string.
func DisksOneLine(disks []models.TelemetryDisk) string {
	if len(disks) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(disks))
	for _, d := range disks {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", d.Mountpoint, d.UsedPct))
	}
	return strings.Join(parts, " · ")
}

// HumanBytesKB renders a KB count as KB/MB/GB/TB. Mirrors output.humanBytesKB.
func HumanBytesKB(kb uint64) string {
	const (
		mb = 1024
		gb = 1024 * 1024
		tb = 1024 * 1024 * 1024
	)
	switch {
	case kb >= tb:
		return fmt.Sprintf("%.1f TB", float64(kb)/float64(tb))
	case kb >= gb:
		return fmt.Sprintf("%.1f GB", float64(kb)/float64(gb))
	case kb >= mb:
		return fmt.Sprintf("%.1f MB", float64(kb)/float64(mb))
	default:
		return fmt.Sprintf("%d KB", kb)
	}
}

// SplitServices partitions telemetry services into running and not-running.
func SplitServices(items []models.TelemetryService) (running, down []models.TelemetryService) {
	for _, s := range items {
		if s.Running {
			running = append(running, s)
		} else {
			down = append(down, s)
		}
	}
	return
}

// Default returns fallback when s is empty.
func Default(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Truncate shortens s to at most n runes, appending an ellipsis when cut.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// Plural returns "s" unless n == 1.
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
