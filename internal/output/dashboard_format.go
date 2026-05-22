package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Table/simple/detailed FormatDashboard + FormatDeviceHealth live here
// to keep the rest of the formatter files focused on their existing
// shape. The JSON variants stay in json.go since they're trivial.

// --------------------------------------------------------------------
// Table format — aligned columns, no Unicode box drawing
// --------------------------------------------------------------------

// FormatDashboard renders the org dashboard as a table.
func (f *TableFormatter) FormatDashboard(d *models.DashboardResponse) error {
	w := f.Writer
	if header := formatAsOf(d.AsOf); header != "" {
		fmt.Fprintf(w, "Dashboard · as of %s\n", header)
		fmt.Fprintln(w)
	}
	// Counter strip
	fmt.Fprintf(w, "%-10s %-8s %-8s %-8s %-9s %-8s\n",
		"DEVICES", "online", "stale", "offline", "disabled", "pending")
	fmt.Fprintf(w, "%-10d %-8d %-8d %-8d %-9d %-8d\n",
		d.Devices.Total, d.Devices.Online, d.Devices.Stale,
		d.Devices.Offline, d.Devices.Disabled, d.Devices.Pending)
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%-10s %-8s %-8s %-8s %-8s\n",
		"SYNC", "in-sync", "drift", "error", "never")
	fmt.Fprintf(w, "%-10s %-8d %-8d %-8d %-8d\n",
		"", d.Sync.InSync, d.Sync.Drift, d.Sync.Error, d.Sync.Never)
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%-10s %-9s %-12s %-9s %-9s %-8s %-8s\n",
		"TASKS 24h", "completed", "in-progress", "pending", "scheduled", "failed", "expired")
	fmt.Fprintf(w, "%-10s %-9d %-12d %-9d %-9d %-8d %-8d\n",
		"", d.Tasks24h.Completed, d.Tasks24h.InProgress, d.Tasks24h.Pending,
		d.Tasks24h.Scheduled, d.Tasks24h.Failed, d.Tasks24h.Expired)
	fmt.Fprintln(w)

	// Agent-version histogram
	if len(d.AgentVersions) > 0 {
		fmt.Fprintln(w, "AGENT VERSIONS")
		for _, v := range sortedAgentVersions(d.AgentVersions) {
			fmt.Fprintf(w, "  %-20s %d\n", v.Version, v.Count)
		}
		fmt.Fprintln(w)
	}

	// Compact fleet table — sorted by attention rank
	rows := sortedCompact(d.Compact)
	fmt.Fprintf(w, "FLEET (%d device%s) — attention-first\n",
		len(rows), plural(len(rows)))
	fmt.Fprintf(w, "%-20s %-9s %-9s %-13s %-22s %s\n",
		"DEVICE", "STATUS", "HEARTBEAT", "SYNC", "ATTENTION", "AGENT")
	for _, r := range rows {
		row := r
		ou := strings.Join(row.OUs, "/")
		name := row.Name
		if ou != "" {
			name = fmt.Sprintf("%s (%s)", row.Name, ou)
		}
		if len(name) > 20 {
			name = name[:19] + "…"
		}
		syncCell := row.Sync.State
		if row.Sync.AgeSec != nil {
			syncCell = fmt.Sprintf("%s · %s", row.Sync.State, humanDuration(*row.Sync.AgeSec))
		}
		attn := attentionTags(&row)
		if attn == "" {
			attn = "—"
		}
		if len(attn) > 22 {
			attn = attn[:21] + "…"
		}
		fmt.Fprintf(w, "%-20s %-9s %-9s %-13s %-22s %s\n",
			name,
			statusColorLabel(row.StatusColor),
			humanDurationPtr(row.HeartbeatAgeSec),
			truncate(syncCell, 13),
			attn,
			defaultStr(row.AgentVersion, "—"),
		)
	}
	return nil
}

// FormatDeviceHealth renders the per-device drill-down as a table.
func (f *TableFormatter) FormatDeviceHealth(d *models.DeviceTelemetryResponse) error {
	w := f.Writer
	fmt.Fprintf(w, "Device: %s\n", d.Name)
	fmt.Fprintf(w, "  status:   %s (%s)\n", d.Status, onlineLabel(d.Online))
	fmt.Fprintf(w, "  agent:    %s\n", defaultStr(d.AgentVersion, "—"))
	fmt.Fprintf(w, "  hb age:   %s\n", humanDurationPtr(d.HeartbeatAgeSec))
	fmt.Fprintf(w, "  snap age: %s\n", humanDurationPtr(d.SnapshotAgeSec))
	if len(d.OUs) > 0 {
		fmt.Fprintf(w, "  ous:      %s\n", strings.Join(d.OUs, ", "))
	}
	fmt.Fprintf(w, "  sync:     %s", d.Sync.State)
	if d.Sync.AgeSec != nil {
		fmt.Fprintf(w, " · %s ago", humanDuration(*d.Sync.AgeSec))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)

	if d.Snapshot == nil {
		fmt.Fprintln(w, "No recent telemetry.")
		return nil
	}
	s := d.Snapshot
	fmt.Fprintf(w, "RESOURCES  uptime %s · %d CPU · load %.2f/%.2f/%.2f\n",
		humanDuration(int64(s.UptimeSec)), s.CPUCount, s.Load1, s.Load5, s.Load15)
	fmt.Fprintf(w, "  memory:   %.1f%% of %s\n", s.MemUsedPct, humanBytesKB(s.MemTotalKB))
	if s.SwapTotalKB > 0 {
		fmt.Fprintf(w, "  swap:     %.1f%% of %s\n", s.SwapUsedPct, humanBytesKB(s.SwapTotalKB))
	}
	fmt.Fprintln(w)

	if len(s.Disks) > 0 {
		fmt.Fprintln(w, "DISKS")
		for _, dk := range s.Disks {
			fmt.Fprintf(w, "  %-12s %5.1f%%  (%s used / %s total)\n",
				dk.Mountpoint, dk.UsedPct, humanBytesKB(dk.UsedKB), humanBytesKB(dk.TotalKB))
		}
		fmt.Fprintln(w)
	}

	if s.Heavy != nil {
		writeHeavyTable(w, s.Heavy)
	}
	return nil
}

func writeHeavyTable(w writer, h *models.TelemetryHeavy) {
	if h.Services != nil {
		items := h.Services.Items
		running, down := splitServices(items)
		fmt.Fprintf(w, "SERVICES (%d running / %d down of %d)\n",
			len(running), len(down), len(items))
		if len(down) > 0 {
			fmt.Fprintln(w, "  not running:")
			for _, s := range down {
				fmt.Fprintf(w, "    - %s  %s\n", s.Name, s.Description)
			}
		}
		fmt.Fprintln(w)
	}
	if h.Updates != nil {
		u := h.Updates
		fmt.Fprintln(w, "UPDATES")
		fmt.Fprintf(w, "  status:       %s\n", u.Status)
		if u.LastCheck != "" {
			fmt.Fprintf(w, "  last check:   %s\n", u.LastCheck)
		}
		fmt.Fprintf(w, "  pending:      %d (upgrade %d · new %d", u.UpgradeCount+u.NewCount, u.UpgradeCount, u.NewCount)
		if u.ReinstallCount > 0 {
			fmt.Fprintf(w, " · reinstall %d", u.ReinstallCount)
		}
		if u.RemoveCount > 0 {
			fmt.Fprintf(w, " · remove %d", u.RemoveCount)
		}
		fmt.Fprintln(w, ")")
		if u.NeedsReboot {
			fmt.Fprintln(w, "  reboot:       required")
		}
		fmt.Fprintln(w)
	}
	if h.Certs != nil {
		fmt.Fprintln(w, "CERTIFICATES")
		for _, c := range h.Certs.Items {
			tag := fmt.Sprintf("%d d", c.DaysLeft)
			if c.DaysLeft <= 0 {
				tag = "expired"
			} else if c.DaysLeft <= 30 {
				tag = fmt.Sprintf("%d d ⚠", c.DaysLeft)
			}
			extra := ""
			if c.InUse {
				extra = " · in use"
			}
			fmt.Fprintf(w, "  %-32s %s%s\n", truncate(c.Description, 32), tag, extra)
		}
		fmt.Fprintln(w)
	}
}

// --------------------------------------------------------------------
// Simple format — bullet points, no alignment, no decorations
// --------------------------------------------------------------------

// FormatDashboard renders the dashboard as a bullet-point list.
func (f *SimpleFormatter) FormatDashboard(d *models.DashboardResponse) error {
	w := f.Writer
	fmt.Fprintf(w, "Dashboard\n")
	fmt.Fprintf(w, "  devices: %d total (online %d, stale %d, offline %d, disabled %d, pending %d)\n",
		d.Devices.Total, d.Devices.Online, d.Devices.Stale,
		d.Devices.Offline, d.Devices.Disabled, d.Devices.Pending)
	fmt.Fprintf(w, "  sync: in-sync %d, drift %d, error %d, never %d\n",
		d.Sync.InSync, d.Sync.Drift, d.Sync.Error, d.Sync.Never)
	fmt.Fprintf(w, "  tasks 24h: completed %d, in-progress %d, pending %d, scheduled %d, failed %d, expired %d\n",
		d.Tasks24h.Completed, d.Tasks24h.InProgress, d.Tasks24h.Pending,
		d.Tasks24h.Scheduled, d.Tasks24h.Failed, d.Tasks24h.Expired)
	if len(d.AgentVersions) > 0 {
		parts := make([]string, 0, len(d.AgentVersions))
		for _, v := range sortedAgentVersions(d.AgentVersions) {
			parts = append(parts, fmt.Sprintf("%s×%d", v.Version, v.Count))
		}
		fmt.Fprintf(w, "  agent versions: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Fleet")
	for _, r := range sortedCompact(d.Compact) {
		row := r
		hb := humanDurationPtr(row.HeartbeatAgeSec)
		attn := attentionTags(&row)
		line := fmt.Sprintf("  %s [%s] hb=%s sync=%s agent=%s",
			row.Name, row.StatusColor, hb, row.Sync.State, defaultStr(row.AgentVersion, "—"))
		if attn != "" {
			line += " · " + attn
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// FormatDeviceHealth renders the per-device drill-down as bullet points.
func (f *SimpleFormatter) FormatDeviceHealth(d *models.DeviceTelemetryResponse) error {
	w := f.Writer
	fmt.Fprintf(w, "Device %s · %s · %s\n", d.Name, d.Status, onlineLabel(d.Online))
	fmt.Fprintf(w, "  agent: %s\n", defaultStr(d.AgentVersion, "—"))
	fmt.Fprintf(w, "  heartbeat: %s ago\n", humanDurationPtr(d.HeartbeatAgeSec))
	fmt.Fprintf(w, "  snapshot:  %s ago\n", humanDurationPtr(d.SnapshotAgeSec))
	fmt.Fprintf(w, "  sync: %s", d.Sync.State)
	if d.Sync.AgeSec != nil {
		fmt.Fprintf(w, " (%s ago)", humanDuration(*d.Sync.AgeSec))
	}
	fmt.Fprintln(w)
	if d.Snapshot == nil {
		fmt.Fprintln(w, "  telemetry: none")
		return nil
	}
	s := d.Snapshot
	fmt.Fprintf(w, "  uptime: %s · %d CPU · load %.2f/%.2f/%.2f\n",
		humanDuration(int64(s.UptimeSec)), s.CPUCount, s.Load1, s.Load5, s.Load15)
	fmt.Fprintf(w, "  memory: %.1f%% · swap: %.1f%%\n", s.MemUsedPct, s.SwapUsedPct)
	if len(s.Disks) > 0 {
		fmt.Fprintf(w, "  disks: %s\n", disksOneLine(s.Disks))
	}
	if s.Heavy != nil {
		if s.Heavy.Services != nil {
			run, down := splitServices(s.Heavy.Services.Items)
			fmt.Fprintf(w, "  services: %d running, %d down\n", len(run), len(down))
			for _, ds := range down {
				fmt.Fprintf(w, "    - %s\n", ds.Name)
			}
		}
		if s.Heavy.Updates != nil {
			u := s.Heavy.Updates
			fmt.Fprintf(w, "  updates: %s (%d pending; reboot=%t)\n", u.Status, u.UpgradeCount+u.NewCount, u.NeedsReboot)
		}
		if s.Heavy.Certs != nil {
			for _, c := range s.Heavy.Certs.Items {
				fmt.Fprintf(w, "  cert: %s — %d days left\n", c.Description, c.DaysLeft)
			}
		}
	}
	return nil
}

// --------------------------------------------------------------------
// Detailed format — Unicode box drawing
// --------------------------------------------------------------------

// FormatDashboard renders the dashboard in the detailed boxed style.
func (f *DetailedFormatter) FormatDashboard(d *models.DashboardResponse) error {
	w := f.Writer
	fmt.Fprintf(w, "╭─ Dashboard")
	if h := formatAsOf(d.AsOf); h != "" {
		fmt.Fprintf(w, " · as of %s", h)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "│\n")
	fmt.Fprintf(w, "│ Devices  total %d  ·  online %d  stale %d  offline %d  disabled %d  pending %d\n",
		d.Devices.Total, d.Devices.Online, d.Devices.Stale,
		d.Devices.Offline, d.Devices.Disabled, d.Devices.Pending)
	fmt.Fprintf(w, "│ Sync     in-sync %d  drift %d  error %d  never %d\n",
		d.Sync.InSync, d.Sync.Drift, d.Sync.Error, d.Sync.Never)
	fmt.Fprintf(w, "│ Tasks 24h  completed %d  in-progress %d  pending %d  scheduled %d  failed %d  expired %d\n",
		d.Tasks24h.Completed, d.Tasks24h.InProgress, d.Tasks24h.Pending,
		d.Tasks24h.Scheduled, d.Tasks24h.Failed, d.Tasks24h.Expired)

	if len(d.AgentVersions) > 0 {
		fmt.Fprintf(w, "│\n├─ Agent versions\n")
		for _, v := range sortedAgentVersions(d.AgentVersions) {
			fmt.Fprintf(w, "│   %-20s %d\n", v.Version, v.Count)
		}
	}

	rows := sortedCompact(d.Compact)
	fmt.Fprintf(w, "│\n├─ Fleet (%d device%s) — attention-first\n", len(rows), plural(len(rows)))
	for _, r := range rows {
		row := r
		fmt.Fprintf(w, "│ %s · %s\n", row.Name, row.StatusColor)
		ou := strings.Join(row.OUs, " / ")
		if ou != "" {
			fmt.Fprintf(w, "│   ou:    %s\n", ou)
		}
		fmt.Fprintf(w, "│   hb:    %s  ·  sync: %s", humanDurationPtr(row.HeartbeatAgeSec), row.Sync.State)
		if row.Sync.AgeSec != nil {
			fmt.Fprintf(w, " (%s)", humanDuration(*row.Sync.AgeSec))
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "│   agent: %s\n", defaultStr(row.AgentVersion, "—"))
		if attn := attentionTags(&row); attn != "" {
			fmt.Fprintf(w, "│   ⚠     %s\n", attn)
		}
		if row.Telemetry != nil {
			t := row.Telemetry
			fmt.Fprintf(w, "│   snap:  %s ago\n", humanDurationPtr(t.SnapshotAgeSec))
		}
		fmt.Fprintln(w, "│")
	}
	fmt.Fprintln(w, "╰─")
	return nil
}

// FormatDeviceHealth renders the per-device drill-down in detailed style.
func (f *DetailedFormatter) FormatDeviceHealth(d *models.DeviceTelemetryResponse) error {
	w := f.Writer
	fmt.Fprintf(w, "╭─ Device %s\n", d.Name)
	fmt.Fprintf(w, "│ status:    %s  ·  %s\n", d.Status, onlineLabel(d.Online))
	fmt.Fprintf(w, "│ agent:     %s\n", defaultStr(d.AgentVersion, "—"))
	fmt.Fprintf(w, "│ heartbeat: %s ago\n", humanDurationPtr(d.HeartbeatAgeSec))
	fmt.Fprintf(w, "│ snapshot:  %s ago\n", humanDurationPtr(d.SnapshotAgeSec))
	if len(d.OUs) > 0 {
		fmt.Fprintf(w, "│ ous:       %s\n", strings.Join(d.OUs, ", "))
	}
	fmt.Fprintf(w, "│ sync:      %s", d.Sync.State)
	if d.Sync.AgeSec != nil {
		fmt.Fprintf(w, "  (%s ago)", humanDuration(*d.Sync.AgeSec))
	}
	fmt.Fprintln(w)

	if d.Snapshot == nil {
		fmt.Fprintln(w, "│")
		fmt.Fprintln(w, "│ No recent telemetry.")
		fmt.Fprintln(w, "╰─")
		return nil
	}

	s := d.Snapshot
	fmt.Fprintln(w, "│")
	fmt.Fprintf(w, "├─ Resources  uptime %s · %d CPU\n",
		humanDuration(int64(s.UptimeSec)), s.CPUCount)
	fmt.Fprintf(w, "│   load:    %.2f / %.2f / %.2f\n", s.Load1, s.Load5, s.Load15)
	fmt.Fprintf(w, "│   memory:  %.1f%% of %s\n", s.MemUsedPct, humanBytesKB(s.MemTotalKB))
	if s.SwapTotalKB > 0 {
		fmt.Fprintf(w, "│   swap:    %.1f%% of %s\n", s.SwapUsedPct, humanBytesKB(s.SwapTotalKB))
	}

	if len(s.Disks) > 0 {
		fmt.Fprintln(w, "│")
		fmt.Fprintln(w, "├─ Disks")
		for _, dk := range s.Disks {
			fmt.Fprintf(w, "│   %-12s %5.1f%%  (%s / %s)\n",
				dk.Mountpoint, dk.UsedPct,
				humanBytesKB(dk.UsedKB), humanBytesKB(dk.TotalKB))
		}
	}

	if s.Heavy != nil {
		h := s.Heavy
		if h.Services != nil {
			run, down := splitServices(h.Services.Items)
			fmt.Fprintln(w, "│")
			fmt.Fprintf(w, "├─ Services  %d running / %d down\n", len(run), len(down))
			if len(down) > 0 {
				for _, ds := range down {
					fmt.Fprintf(w, "│   ✗ %-20s %s\n", ds.Name, ds.Description)
				}
			} else {
				fmt.Fprintln(w, "│   all services running")
			}
		}
		if h.Updates != nil {
			u := h.Updates
			fmt.Fprintln(w, "│")
			fmt.Fprintln(w, "├─ Updates")
			fmt.Fprintf(w, "│   status:   %s\n", u.Status)
			if u.LastCheck != "" {
				fmt.Fprintf(w, "│   checked:  %s\n", u.LastCheck)
			}
			fmt.Fprintf(w, "│   pending:  %d (upgrade %d · new %d", u.UpgradeCount+u.NewCount, u.UpgradeCount, u.NewCount)
			if u.ReinstallCount > 0 {
				fmt.Fprintf(w, " · reinstall %d", u.ReinstallCount)
			}
			if u.RemoveCount > 0 {
				fmt.Fprintf(w, " · remove %d", u.RemoveCount)
			}
			fmt.Fprintln(w, ")")
			if u.NeedsReboot {
				fmt.Fprintln(w, "│   reboot:   required ⚠")
			}
		}
		if h.Certs != nil {
			fmt.Fprintln(w, "│")
			fmt.Fprintln(w, "├─ Certificates")
			for _, c := range h.Certs.Items {
				tag := fmt.Sprintf("%d d", c.DaysLeft)
				if c.DaysLeft <= 0 {
					tag = "expired ⚠"
				} else if c.DaysLeft <= 30 {
					tag = fmt.Sprintf("%d d ⚠", c.DaysLeft)
				}
				extra := ""
				if c.InUse {
					extra = " · in use"
				}
				fmt.Fprintf(w, "│   %-32s %s%s\n", truncate(c.Description, 32), tag, extra)
			}
		}
	}
	fmt.Fprintln(w, "╰─")
	return nil
}

// --------------------------------------------------------------------
// Shared helpers — kept package-private so the formatters share them
// --------------------------------------------------------------------

type writer interface {
	Write(p []byte) (int, error)
}

func splitServices(items []models.TelemetryService) (running, down []models.TelemetryService) {
	for _, s := range items {
		if s.Running {
			running = append(running, s)
		} else {
			down = append(down, s)
		}
	}
	return
}

func humanBytesKB(kb uint64) string {
	const (
		MB = 1024
		GB = 1024 * 1024
		TB = 1024 * 1024 * 1024
	)
	if kb >= TB {
		return fmt.Sprintf("%.1f TB", float64(kb)/float64(TB))
	}
	if kb >= GB {
		return fmt.Sprintf("%.1f GB", float64(kb)/float64(GB))
	}
	if kb >= MB {
		return fmt.Sprintf("%.1f MB", float64(kb)/float64(MB))
	}
	return fmt.Sprintf("%d KB", kb)
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
