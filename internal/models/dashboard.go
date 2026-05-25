package models

// Dashboard / device-health types — mirror the NDManager JSON shape
// emitted by `services/dashboard_service.py`. Wire owner is NDManager;
// when adding a field there, add it here too or the table formatter
// will silently drop it.

// DashboardResponse is the org-level roll-up returned by
// GET /api/v1/organizations/{org}/dashboard.
type DashboardResponse struct {
	AsOf          int64                   `json:"as_of"`
	Devices       DashboardDeviceCounters `json:"devices"`
	Sync          DashboardSyncCounters   `json:"sync"`
	Tasks24h      DashboardTaskCounters   `json:"tasks_24h"`
	AgentVersions []DashboardAgentVersion `json:"agent_versions"`
	Compact       []DashboardCompactRow   `json:"compact"`
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
	Name            string                       `json:"name"`
	UUID            string                       `json:"uuid"`
	Status          string                       `json:"status"`
	StatusColor     string                       `json:"status_color"`
	Online          *bool                        `json:"online,omitempty"`
	OUs             []string                     `json:"ous,omitempty"`
	HeartbeatAgeSec *int64                       `json:"heartbeat_age_sec,omitempty"`
	Sync            DashboardCompactSync         `json:"sync"`
	AgentVersion    string                       `json:"agent_version,omitempty"`
	OPNsenseVersion string                       `json:"opnsense_version,omitempty"`
	Telemetry       *DashboardCompactTelemetry   `json:"telemetry,omitempty"`
}

type DashboardCompactSync struct {
	State    string `json:"state"`
	SyncedAt string `json:"synced_at,omitempty"`
	AgeSec   *int64 `json:"age_sec,omitempty"`
}

type DashboardCompactTelemetry struct {
	UptimeSec        *int64                 `json:"uptime_sec,omitempty"`
	Load1            *float64               `json:"load1,omitempty"`
	MemUsedPct       *float64               `json:"mem_used_pct,omitempty"`
	SwapUsedPct      *float64               `json:"swap_used_pct,omitempty"`
	DiskRootUsedPct  *float64               `json:"disk_root_used_pct,omitempty"`
	SnapshotAgeSec   *int64                 `json:"snapshot_age_sec,omitempty"`
	HeavySummary     *DashboardHeavySummary `json:"heavy_summary,omitempty"`
}

// DashboardHeavySummary is three attention counters; each is nil when
// the underlying probe failed (distinct from 0 = "no concerns").
type DashboardHeavySummary struct {
	ServicesDown    *int `json:"services_down,omitempty"`
	PendingUpdates  *int `json:"pending_updates,omitempty"`
	// CertsExpired counts certs whose days_left ≤ 0 (already past their
	// notAfter). Distinct from CertsExpiring30d so the dashboard can
	// render an already-expired cert as a P1 rather than a "≤30 d" warn.
	CertsExpired     *int `json:"certs_expired,omitempty"`
	CertsExpiring30d *int `json:"certs_expiring_30d,omitempty"`
}

// DeviceTelemetryResponse is the per-device drill-down returned by
// GET /api/v1/organizations/{org}/devices/{name}/telemetry. Returns the
// full agent snapshot (no compact trim) so the dashboard can render
// service tables, cert lists, etc.
type DeviceTelemetryResponse struct {
	Name            string                 `json:"name"`
	UUID            string                 `json:"uuid"`
	Status          string                 `json:"status"`
	Online          *bool                  `json:"online,omitempty"`
	OUs             []string               `json:"ous,omitempty"`
	AgentVersion    string                 `json:"agent_version,omitempty"`
	Heartbeat       string                 `json:"heartbeat,omitempty"`
	HeartbeatAgeSec *int64                 `json:"heartbeat_age_sec,omitempty"`
	Sync            DeviceTelemetrySync    `json:"sync"`
	Snapshot        *TelemetrySnapshot     `json:"snapshot,omitempty"`
	SnapshotAgeSec  *int64                 `json:"snapshot_age_sec,omitempty"`
	AsOf            int64                  `json:"as_of"`
}

type DeviceTelemetrySync struct {
	State      string `json:"state"`
	SyncedAt   string `json:"synced_at,omitempty"`
	SyncedHash string `json:"synced_hash,omitempty"`
	AgeSec     *int64 `json:"age_sec,omitempty"`
}

// TelemetrySnapshot mirrors the agent's wire format. Owners:
// - Base fields (uptime, load, mem, disk, cpu_count): NDAgent
//   internal/telemetry/snapshot.go
// - Heavy block: NDAgent internal/telemetry/heavy.go
type TelemetrySnapshot struct {
	UptimeSec    uint64           `json:"uptime_sec"`
	Load1        float64          `json:"load1"`
	Load5        float64          `json:"load5"`
	Load15       float64          `json:"load15"`
	CPUCount     int              `json:"cpu_count"`
	MemUsedPct   float64          `json:"mem_used_pct"`
	MemTotalKB   uint64           `json:"mem_total_kb"`
	SwapUsedPct  float64          `json:"swap_used_pct"`
	SwapTotalKB  uint64           `json:"swap_total_kb"`
	Disks        []TelemetryDisk  `json:"disks,omitempty"`
	Hostname     string           `json:"hostname,omitempty"`
	OSPlatform   string           `json:"os_platform,omitempty"`
	OSVersion    string           `json:"os_version,omitempty"`
	CollectedAt  float64          `json:"collected_at"`
	CollectionMs int64            `json:"collection_ms"`
	Heavy        *TelemetryHeavy  `json:"heavy,omitempty"`
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
	Status         string  `json:"status"`
	StatusMsg      string  `json:"status_msg,omitempty"`
	LastCheck      string  `json:"last_check,omitempty"`
	UpgradeCount   int     `json:"upgrade_count"`
	NewCount       int     `json:"new_count"`
	ReinstallCount int     `json:"reinstall_count"`
	RemoveCount    int     `json:"remove_count"`
	NeedsReboot    bool    `json:"needs_reboot"`
	Connection     string  `json:"connection,omitempty"`
	Repository     string  `json:"repository,omitempty"`
	AsOf           float64 `json:"as_of"`
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
