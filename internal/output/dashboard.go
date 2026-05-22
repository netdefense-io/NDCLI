package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Shared helpers used by every output format. Each format's main file
// imports these to keep the formatter switch concise — the dashboard
// payload has enough fields that copy-pasting per-format would drift
// quickly.

// dashboardJSON is the JSON-format implementation, shared between all
// formats' fallback when --format json is set. The other formats
// (table/simple/detailed) delegate to format-specific helpers below.
func dashboardJSON(w stringWriter, d *models.DashboardResponse) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

func deviceHealthJSON(w stringWriter, d *models.DeviceTelemetryResponse) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

// stringWriter is what every formatter's underlying io.Writer satisfies.
// Aliased so the helpers above stay generic over the four format types.
type stringWriter interface {
	Write(p []byte) (int, error)
}

// humanDuration renders an int seconds count into a short
// 12s / 4m / 2h / 3d string suitable for table cells. Matches the
// NDWeb dashboard's humanDuration so users see the same shape across
// surfaces.
func humanDuration(sec int64) string {
	if sec < 0 {
		return "—"
	}
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm", sec/60)
	}
	if sec < 86400 {
		return fmt.Sprintf("%dh", sec/3600)
	}
	return fmt.Sprintf("%dd", sec/86400)
}

// humanDurationPtr is the *int64 variant used by the dashboard's nilable
// age fields.
func humanDurationPtr(sec *int64) string {
	if sec == nil {
		return "—"
	}
	return humanDuration(*sec)
}

// onlineLabel maps the tristate online pointer to a human label. nil =
// the broker registry is unavailable; we never report it as offline.
func onlineLabel(b *bool) string {
	if b == nil {
		return "unknown"
	}
	if *b {
		return "online"
	}
	return "offline"
}

// statusColorLabel surfaces the dashboard's status_color field as the
// human-readable string for table cells.
func statusColorLabel(c string) string {
	if c == "" {
		return "—"
	}
	return c
}

// sortedAgentVersions returns the histogram sorted by count desc,
// breaking ties on version asc. The wire is already pre-sorted by the
// NDManager service, but defending here means the formatters don't
// silently drift if that contract changes.
func sortedAgentVersions(items []models.DashboardAgentVersion) []models.DashboardAgentVersion {
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

// attentionRank scores a compact row for sorting — higher = needs more
// attention. Matches NDWeb's FleetOverview.attentionRank so CLI table
// order mirrors what users see on the web dashboard.
func attentionRank(row *models.DashboardCompactRow) int {
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
	if row.Telemetry != nil && row.Telemetry.HeavySummary != nil {
		h := row.Telemetry.HeavySummary
		if h.ServicesDown != nil && *h.ServicesDown > 0 {
			r += 300 + 10*(*h.ServicesDown)
		}
		// Already-expired certs are P1 — outrank "expiring soon".
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

// sortedCompact returns the rows ordered by attention rank desc, then
// name asc.
func sortedCompact(rows []models.DashboardCompactRow) []models.DashboardCompactRow {
	out := make([]models.DashboardCompactRow, len(rows))
	copy(out, rows)
	sort.Slice(out, func(i, j int) bool {
		ri, rj := attentionRank(&out[i]), attentionRank(&out[j])
		if ri != rj {
			return ri > rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// attentionTags renders the compact row's attention badges as a short
// comma-joined string for table cells. Empty when nothing's wrong.
func attentionTags(row *models.DashboardCompactRow) string {
	tags := make([]string, 0, 4)
	if row.Sync.State == "error" {
		tags = append(tags, "sync-error")
	}
	if row.Telemetry == nil || row.Telemetry.HeavySummary == nil {
		if len(tags) == 0 {
			return ""
		}
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

// formatAsOf converts the response's `as_of` unix-seconds field into a
// human-readable timestamp for headers.
func formatAsOf(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05 MST")
}

// disksLineOneShot renders the disks slice as a compact "/, /var: 4%/12%"
// string for the detailed view. The table view shows full per-mount
// breakdown.
func disksOneLine(disks []models.TelemetryDisk) string {
	if len(disks) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(disks))
	for _, d := range disks {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", d.Mountpoint, d.UsedPct))
	}
	return strings.Join(parts, " · ")
}
