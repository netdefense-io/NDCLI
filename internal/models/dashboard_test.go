package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// deviceTelemetryRetained is a device's telemetry with retained=true, with
// every field the control plane sends for OPNsense and staleness.
const deviceTelemetryRetained = `{
  "name": "fw-01", "uuid": "u1", "status": "ENABLED", "online": false, "agent_version": "1.29.3",
  "opnsense_version": "26.1.11_10", "opnsense_version_source": "last_known", "opnsense_series": "26.1",
  "opnsense_package": "opnsense", "opnsense_latest": "26.1.12", "opnsense_latest_source": "upstream",
  "upgrade_major_version": "26.7", "opnsense_update": "minor", "opnsense_newer_series": "26.7",
  "checked_at": 1794509791, "heartbeat_age_sec": 7300, "sync": {"state": "in_sync"},
  "snapshot": {"uptime_sec": 10, "collected_at": 1794500000, "collection_ms": 3,
    "heavy": {"collected_at": 1794500000, "updates": {"status": "update", "upgrade_count": 2, "new_count": 0,
      "reinstall_count": 0, "remove_count": 0, "needs_reboot": false, "opnsense_version": "26.1.11_10",
      "opnsense_latest": "26.1.12", "opnsense_package": "opnsense", "upgrade_major_version": "26.7",
      "last_check_unix": 1794509791, "as_of": 1794500000}}},
  "snapshot_age_sec": 7200, "stale": true,
  "heavy_age_sec": {"services": null, "updates": 7400, "certs": 7400}, "as_of": 1794516339
}`

// The OPNsense fields, the staleness and the block ages decode, and -f json
// and the MCP tools, which encode the model again, keep them (#256).
func TestDeviceTelemetryKeepsTheOPNsenseAndRetainedFields(t *testing.T) {
	var d DeviceTelemetryResponse
	if err := json.Unmarshal([]byte(deviceTelemetryRetained), &d); err != nil {
		t.Fatal(err)
	}
	if d.OPNsenseVersion != "26.1.11_10" || d.OPNsenseUpdate != OPNsenseUpdateMinor || d.OPNsenseNewerSeries != "26.7" ||
		d.CheckedAt == nil || *d.CheckedAt != 1794509791 || !d.IsStale() ||
		d.HeavyAgeSec == nil || d.HeavyAgeSec.Services != nil || *d.HeavyAgeSec.Updates != 7400 {
		t.Errorf("decoded %+v", d)
	}
	u := d.Snapshot.Heavy.Updates
	if u.OPNsenseLatest != "26.1.12" || u.OPNsensePackage != "opnsense" || u.UpgradeMajorVersion != "26.7" || *u.LastCheckUnix != 1794509791 {
		t.Errorf("updates block %+v", u)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"opnsense_version":"26.1.11_10"`, `"opnsense_version_source":"last_known"`, `"opnsense_update":"minor"`,
		`"opnsense_newer_series":"26.7"`, `"opnsense_latest_source":"upstream"`, `"checked_at":1794509791`, `"stale":true`,
		`"heavy_age_sec":{"updates":7400,"certs":7400}`, `"opnsense_package":"opnsense"`, `"last_check_unix":1794509791`} {
		if !strings.Contains(string(out), key) {
			t.Errorf("encoded telemetry lacks %s:\n%s", key, out)
		}
	}
}

// A control plane that predates them sends none of the fields, and the
// telemetry encodes as it always did.
func TestDeviceTelemetryFromAnOlderServerEncodesAsBefore(t *testing.T) {
	var d DeviceTelemetryResponse
	if err := json.Unmarshal([]byte(`{"name":"fw-01","status":"ENABLED","online":true,"sync":{"state":"in_sync"},"as_of":1}`), &d); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(d)
	for _, key := range []string{"opnsense", "checked_at", "stale", "heavy_age_sec"} {
		if strings.Contains(string(out), key) {
			t.Errorf("an older server's telemetry encodes %s:\n%s", key, out)
		}
	}
	if d.IsStale() || d.OPNsenseText() != "" {
		t.Errorf("stale %v, text %q", d.IsStale(), d.OPNsenseText())
	}
}

func TestDashboardKeepsTheOPNsenseReference(t *testing.T) {
	var d DashboardResponse
	raw := `{"as_of":1,"devices":{},"sync":{},"tasks_24h":{},"agent_versions":[],"latest_agent_version":"1.29.3",
	  "opnsense_versions":[{"version":"26.7.8_2","count":2},{"version":"26.1.9","count":1}],
	  "latest_opnsense_version":"26.7.9",
	  "latest_opnsense":{"source":"upstream","fetched_at":5,"newest":"26.7.9","newest_series":"26.7","by_series":[{"series":"26.7","latest":"26.7.9","date":"2026-11-11"}]},
	  "compact":[{"name":"fw-01","status":"ENABLED","sync":{"state":"in_sync"},"opnsense_version":"26.1.9","opnsense_update":"major","opnsense_newer_series":"26.7",
	    "telemetry":{"stale":true,"heavy_summary":{"pending_updates":3,"as_of":4,"updates_age_sec":90}}}]}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	row := d.Compact[0]
	if len(d.OPNsenseVersions) != 2 || d.LatestOPNsenseVersion != "26.7.9" || d.LatestOPNsense.BySeries[0].Latest != "26.7.9" ||
		row.OPNsenseUpdate != OPNsenseUpdateMajor || !*row.Telemetry.Stale || *row.Telemetry.HeavySummary.UpdatesAgeSec != 90 {
		t.Errorf("decoded %+v", d)
	}
}

func TestOPNsenseStatusOPNsenseText(t *testing.T) {
	for _, tc := range []struct {
		status OPNsenseStatus
		want   string
	}{
		{OPNsenseStatus{}, ""},
		{OPNsenseStatus{OPNsenseVersion: "26.7.8_2"}, "26.7.8_2"},
		{OPNsenseStatus{OPNsenseVersion: "26.7.8_2", OPNsenseVersionSource: "live", OPNsenseUpdate: "minor", OPNsenseLatest: "26.7.9"},
			"26.7.8_2 · update to 26.7.9 available"},
		{OPNsenseStatus{OPNsenseVersion: "26.1.11", OPNsenseUpdate: "minor", OPNsenseLatest: "26.1.12", OPNsenseNewerSeries: "26.7"},
			"26.1.11 · update to 26.1.12 available; series 26.7 is out too"},
		{OPNsenseStatus{OPNsenseVersion: "26.1.12", OPNsenseVersionSource: "facts", OPNsenseUpdate: "major", OPNsenseNewerSeries: "26.7"},
			"26.1.12 (from the device's facts) · 26.7 available (major upgrade)"},
		{OPNsenseStatus{OPNsenseVersion: "26.7.9", OPNsenseVersionSource: "last_known", OPNsenseUpdate: "none"},
			"26.7.9 (last known) · up to date"},
		{OPNsenseStatus{OPNsenseVersion: "26.7.9", OPNsenseUpdate: "unknown"}, "26.7.9 · update status unknown"},
		{OPNsenseStatus{OPNsenseVersion: "26.7.8", OPNsenseUpdate: "minor"}, "26.7.8 · a newer release is available"},
		{OPNsenseStatus{OPNsenseVersion: "26.7.9", OPNsenseUpdate: "sideways"}, "26.7.9 · update: sideways"},
	} {
		if got := tc.status.OPNsenseText(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.status, got, tc.want)
		}
	}
}
