package models

// Dashboard / device-health types — mirror the NDManager JSON shape
// emitted by `services/dashboard_service.py`. Wire owner is NDManager;
// when adding a field there, add it here too or the table formatter
// will silently drop it.

// DashboardResponse is the org-level roll-up returned by
// GET /api/v1/organizations/{org}/dashboard.
type DashboardResponse struct {
	AsOf               int64                   `json:"as_of"`
	Devices            DashboardDeviceCounters `json:"devices"`
	Sync               DashboardSyncCounters   `json:"sync"`
	Tasks24h           DashboardTaskCounters   `json:"tasks_24h"`
	AgentVersions      []DashboardAgentVersion `json:"agent_versions"`
	LatestAgentVersion string                  `json:"latest_agent_version,omitempty"`
	// OPNsenseVersions is the {version, count} histogram of the installed
	// OPNsense releases, newest first; a device silent past the snapshot TTL
	// stays in it through its facts or its last-known telemetry.
	OPNsenseVersions []DashboardAgentVersion `json:"opnsense_versions,omitempty"`
	// LatestOPNsenseVersion is the newest OPNsense Community release, or the
	// newest the fleet runs when the upstream reference is unknown.
	LatestOPNsenseVersion string                   `json:"latest_opnsense_version,omitempty"`
	LatestOPNsense        *DashboardLatestOPNsense `json:"latest_opnsense,omitempty"`
	Compact               []DashboardCompactRow    `json:"compact"`
}

// DashboardLatestOPNsense is the control plane's OPNsense reference: the
// newest release of each series it knows, from upstream or, without it, the
// fleet.
type DashboardLatestOPNsense struct {
	Source       string                    `json:"source,omitempty"` // upstream | fleet
	FetchedAt    *int64                    `json:"fetched_at,omitempty"`
	Newest       string                    `json:"newest,omitempty"`
	NewestSeries string                    `json:"newest_series,omitempty"`
	BySeries     []DashboardOPNsenseSeries `json:"by_series,omitempty"`
}

type DashboardOPNsenseSeries struct {
	Series string `json:"series"`
	Latest string `json:"latest"`
	Date   string `json:"date,omitempty"`
}

// The values of OPNsenseStatus.OPNsenseUpdate.
const (
	OPNsenseUpdateMinor   = "minor"   // a newer release of the installed series
	OPNsenseUpdateMajor   = "major"   // no newer release of it, but a newer series
	OPNsenseUpdateNone    = "none"    // up to date
	OPNsenseUpdateUnknown = "unknown" // cannot tell
)

// OPNsenseStatus is the control plane's reading of a device's OPNsense, on
// each dashboard row and on the device telemetry: the installed release,
// where it was read, and what is available. Every field is absent, or null,
// from a control plane that predates it.
type OPNsenseStatus struct {
	OPNsenseVersion string `json:"opnsense_version,omitempty"`
	// OPNsenseVersionSource is where the version was read: live telemetry,
	// the device's facts, or its last-known telemetry.
	OPNsenseVersionSource string `json:"opnsense_version_source,omitempty"` // live | facts | last_known
	OPNsenseSeries        string `json:"opnsense_series,omitempty"`
	OPNsensePackage       string `json:"opnsense_package,omitempty"`
	OPNsenseLatest        string `json:"opnsense_latest,omitempty"`
	OPNsenseLatestSource  string `json:"opnsense_latest_source,omitempty"` // device | upstream
	// UpgradeMajorVersion is the next series the device's mirror offers.
	UpgradeMajorVersion string `json:"upgrade_major_version,omitempty"`
	OPNsenseUpdate      string `json:"opnsense_update,omitempty"`
	// OPNsenseNewerSeries is a newer series, set beside a minor update too.
	OPNsenseNewerSeries string `json:"opnsense_newer_series,omitempty"`
	// CheckedAt is when the device last completed a firmware check, in
	// Unix seconds of the device's clock.
	CheckedAt *int64 `json:"checked_at,omitempty"`
}

// OPNsenseText is the installed release and what is available, as every front-end
// shows it: "26.7.8_2 · update to 26.7.9 available", "26.1.11 · up to date",
// "26.1.11 · 26.7 available (major upgrade)". A version read from the facts
// or the last-known telemetry says so. "" when the version is not known.
func (o OPNsenseStatus) OPNsenseText() string {
	if o.OPNsenseVersion == "" {
		return ""
	}
	text := o.OPNsenseVersion
	switch o.OPNsenseVersionSource {
	case "facts":
		text += " (from the device's facts)"
	case "last_known":
		text += " (last known)"
	}
	if update := o.updateText(); update != "" {
		text += " · " + update
	}
	return text
}

func (o OPNsenseStatus) updateText() string {
	switch o.OPNsenseUpdate {
	case OPNsenseUpdateMinor:
		text := "a newer release is available"
		if o.OPNsenseLatest != "" {
			text = "update to " + o.OPNsenseLatest + " available"
		}
		if o.OPNsenseNewerSeries != "" {
			text += "; series " + o.OPNsenseNewerSeries + " is out too"
		}
		return text
	case OPNsenseUpdateMajor:
		if o.OPNsenseNewerSeries == "" {
			return "a newer series is available (major upgrade)"
		}
		return o.OPNsenseNewerSeries + " available (major upgrade)"
	case OPNsenseUpdateNone:
		return "up to date"
	case OPNsenseUpdateUnknown:
		return "update status unknown"
	case "":
		return ""
	}
	return "update: " + o.OPNsenseUpdate
}

type DashboardDeviceCounters struct {
	Total    int `json:"total"`
	Online   int `json:"online"`
	Stale    int `json:"stale"`
	Offline  int `json:"offline"`
	Disabled int `json:"disabled"`
	Pending  int `json:"pending"`
}

type DashboardSyncCounters struct {
	InSync int `json:"in_sync"`
	Drift  int `json:"drift"`
	Error  int `json:"error"`
	Never  int `json:"never"`
}

type DashboardTaskCounters struct {
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Scheduled  int `json:"scheduled"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Expired    int `json:"expired"`
}

type DashboardAgentVersion struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

type DashboardCompactRow struct {
	Name            string               `json:"name"`
	UUID            string               `json:"uuid"`
	Status          string               `json:"status"`
	StatusColor     string               `json:"status_color"`
	Online          *bool                `json:"online,omitempty"`
	OUs             []string             `json:"ous,omitempty"`
	HeartbeatAgeSec *int64               `json:"heartbeat_age_sec,omitempty"`
	Sync            DashboardCompactSync `json:"sync"`
	AgentVersion    string               `json:"agent_version,omitempty"`
	OPNsenseStatus
	// FirmwareMixedState is true when the device last ran a FIRMWARE_UPGRADE
	// with reboot=false and base/kernel updates are still pending. NDManager
	// sets this when it receives a task result with mixed_state=true.
	FirmwareMixedState bool                       `json:"firmware_mixed_state,omitempty"`
	Telemetry          *DashboardCompactTelemetry `json:"telemetry,omitempty"`
}

type DashboardCompactSync struct {
	State    string `json:"state"`
	SyncedAt string `json:"synced_at,omitempty"`
	AgeSec   *int64 `json:"age_sec,omitempty"`
}

type DashboardCompactTelemetry struct {
	UptimeSec       *int64   `json:"uptime_sec,omitempty"`
	Load1           *float64 `json:"load1,omitempty"`
	MemUsedPct      *float64 `json:"mem_used_pct,omitempty"`
	SwapUsedPct     *float64 `json:"swap_used_pct,omitempty"`
	DiskRootUsedPct *float64 `json:"disk_root_used_pct,omitempty"`
	SnapshotAgeSec  *int64   `json:"snapshot_age_sec,omitempty"`
	// Stale and the HeavySummary ages come with retained telemetry, which no
	// front-end asks the dashboard for yet: they are decoded for -f json only.
	Stale        *bool                  `json:"stale,omitempty"`
	HeavySummary *DashboardHeavySummary `json:"heavy_summary,omitempty"`
}

// DashboardHeavySummary is three attention counters; each is nil when
// the underlying probe failed (distinct from 0 = "no concerns").
type DashboardHeavySummary struct {
	ServicesDown   *int `json:"services_down,omitempty"`
	PendingUpdates *int `json:"pending_updates,omitempty"`
	// CertsExpired counts certs whose days_left ≤ 0 (already past their
	// notAfter). Distinct from CertsExpiring30d so the dashboard can
	// render an already-expired cert as a P1 rather than a "≤30 d" warn.
	CertsExpired     *int `json:"certs_expired,omitempty"`
	CertsExpiring30d *int `json:"certs_expiring_30d,omitempty"`
	// With retained telemetry: the oldest as_of of the blocks used, and each
	// block's age on the server's clock, corrected for the agent's skew.
	AsOf           *float64 `json:"as_of,omitempty"`
	ServicesAgeSec *int64   `json:"services_age_sec,omitempty"`
	UpdatesAgeSec  *int64   `json:"updates_age_sec,omitempty"`
	CertsAgeSec    *int64   `json:"certs_age_sec,omitempty"`
}

// DeviceTelemetryResponse is the per-device drill-down returned by
// GET /api/v1/organizations/{org}/devices/{name}/telemetry. Returns the
// full agent snapshot (no compact trim) so the dashboard can render
// service tables, cert lists, etc.
type DeviceTelemetryResponse struct {
	Name         string   `json:"name"`
	UUID         string   `json:"uuid"`
	Status       string   `json:"status"`
	Online       *bool    `json:"online"`
	OUs          []string `json:"ous,omitempty"`
	AgentVersion string   `json:"agent_version,omitempty"`
	OPNsenseStatus
	Heartbeat       string              `json:"heartbeat,omitempty"`
	HeartbeatAgeSec *int64              `json:"heartbeat_age_sec,omitempty"`
	Sync            DeviceTelemetrySync `json:"sync"`
	Snapshot        *TelemetrySnapshot  `json:"snapshot,omitempty"`
	SnapshotAgeSec  *int64              `json:"snapshot_age_sec,omitempty"`
	// Stale and HeavyAgeSec come with retained telemetry only: Stale is set on
	// a last-known snapshot served in place of an expired one, and
	// HeavyAgeSec is each heavy block's age on the server's clock.
	Stale       *bool               `json:"stale,omitempty"`
	HeavyAgeSec *TelemetryHeavyAges `json:"heavy_age_sec,omitempty"`
	AsOf        int64               `json:"as_of"`
}

// TelemetryHeavyAges is each heavy block's age in seconds, corrected for the
// agent's clock skew; nil when the server could not tell.
type TelemetryHeavyAges struct {
	Services *int64 `json:"services,omitempty"`
	Updates  *int64 `json:"updates,omitempty"`
	Certs    *int64 `json:"certs,omitempty"`
}

// IsStale reports whether the snapshot is a last-known one, not live.
func (d *DeviceTelemetryResponse) IsStale() bool {
	return d.Stale != nil && *d.Stale
}

type DeviceTelemetrySync struct {
	State      string `json:"state"`
	SyncedAt   string `json:"synced_at,omitempty"`
	SyncedHash string `json:"synced_hash,omitempty"`
	AgeSec     *int64 `json:"age_sec,omitempty"`
}

// TelemetrySnapshot mirrors the agent's wire format. Owners:
//   - Base fields (uptime, load, mem, disk, cpu_count): NDAgent
//     internal/telemetry/snapshot.go
//   - Heavy block: NDAgent internal/telemetry/heavy.go
type TelemetrySnapshot struct {
	UptimeSec    uint64          `json:"uptime_sec"`
	Load1        float64         `json:"load1"`
	Load5        float64         `json:"load5"`
	Load15       float64         `json:"load15"`
	CPUCount     int             `json:"cpu_count"`
	MemUsedPct   float64         `json:"mem_used_pct"`
	MemTotalKB   uint64          `json:"mem_total_kb"`
	SwapUsedPct  float64         `json:"swap_used_pct"`
	SwapTotalKB  uint64          `json:"swap_total_kb"`
	Disks        []TelemetryDisk `json:"disks,omitempty"`
	Hostname     string          `json:"hostname,omitempty"`
	OSPlatform   string          `json:"os_platform,omitempty"`
	OSVersion    string          `json:"os_version,omitempty"`
	CollectedAt  float64         `json:"collected_at"`
	CollectionMs int64           `json:"collection_ms"`
	Heavy        *TelemetryHeavy `json:"heavy,omitempty"`
}

type TelemetryDisk struct {
	Mountpoint string  `json:"mountpoint"`
	UsedPct    float64 `json:"used_pct"`
	TotalKB    uint64  `json:"total_kb"`
	UsedKB     uint64  `json:"used_kb"`
}

type TelemetryHeavy struct {
	Services    *TelemetryServicesBlock `json:"services,omitempty"`
	Updates     *TelemetryUpdates       `json:"updates,omitempty"`
	Certs       *TelemetryCertsBlock    `json:"certs,omitempty"`
	CollectedAt float64                 `json:"collected_at"`
}

type TelemetryServicesBlock struct {
	Items []TelemetryService `json:"items"`
	AsOf  float64            `json:"as_of"`
}

type TelemetryService struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Running     bool   `json:"running"`
}

type TelemetryUpdates struct {
	Status         string `json:"status"`
	StatusMsg      string `json:"status_msg,omitempty"`
	LastCheck      string `json:"last_check,omitempty"`
	UpgradeCount   int    `json:"upgrade_count"`
	NewCount       int    `json:"new_count"`
	ReinstallCount int    `json:"reinstall_count"`
	RemoveCount    int    `json:"remove_count"`
	NeedsReboot    bool   `json:"needs_reboot"`
	Connection     string `json:"connection,omitempty"`
	Repository     string `json:"repository,omitempty"`
	// OPNsenseLatest is the installed core package's update candidate, and
	// counts only beside OPNsensePackage: an agent that does not send the
	// package put OPNsense's product_latest there, which is not a target.
	OPNsenseVersion     string  `json:"opnsense_version,omitempty"`
	OPNsenseLatest      string  `json:"opnsense_latest,omitempty"`
	OPNsensePackage     string  `json:"opnsense_package,omitempty"`
	UpgradeMajorVersion string  `json:"upgrade_major_version,omitempty"`
	LastCheckUnix       *int64  `json:"last_check_unix,omitempty"`
	AsOf                float64 `json:"as_of"`
}

type TelemetryCertsBlock struct {
	Items []TelemetryCert `json:"items"`
	AsOf  float64         `json:"as_of"`
}

type TelemetryCert struct {
	Description string `json:"description"`
	DaysLeft    int    `json:"days_left"`
	ValidTo     string `json:"valid_to,omitempty"`
	InUse       bool   `json:"in_use,omitempty"`
}
