package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func int64p(v int64) *int64 { return &v }

// staleTelemetry is a device whose live snapshot expired, served last known
// with retained telemetry, on a release with a minor update out.
func staleTelemetry() *models.DeviceTelemetryResponse {
	stale := true
	return &models.DeviceTelemetryResponse{
		Name: "fw-01", Status: "ENABLED", AgentVersion: "1.29.3", Sync: models.DeviceTelemetrySync{State: "in_sync"},
		OPNsenseStatus: models.OPNsenseStatus{OPNsenseVersion: "26.7.8_2", OPNsenseVersionSource: "live",
			OPNsenseUpdate: models.OPNsenseUpdateMinor, OPNsenseLatest: "26.7.9"},
		SnapshotAgeSec: int64p(7200), Stale: &stale,
		HeavyAgeSec: &models.TelemetryHeavyAges{Services: int64p(360), Updates: int64p(360)},
		Snapshot: &models.TelemetrySnapshot{UptimeSec: 10, Heavy: &models.TelemetryHeavy{
			Services: &models.TelemetryServicesBlock{Items: []models.TelemetryService{{Name: "unbound", Running: true}}},
			Updates:  &models.TelemetryUpdates{Status: "update", UpgradeCount: 2, OPNsenseLatest: "26.7.9", OPNsensePackage: "opnsense"},
			Certs:    &models.TelemetryCertsBlock{Items: []models.TelemetryCert{{Description: "web", DaysLeft: 90}}},
		}},
	}
}

// device health names the OPNsense release and what is available, and never
// shows a last-known snapshot as current: it says it is stale and how old
// each block is (#256).
func TestDeviceHealthShowsOPNsenseAndStaleness(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		out := render(t, format, "FormatDeviceHealth", staleTelemetry())
		mustContain(t, format, out, "26.7.8_2 · update to 26.7.9 available", "2h", "(last known, stale)", "6m old")
		// The certificates' age is not known, so none is shown for them.
		if strings.Count(out, "6m old") != 2 {
			t.Errorf("%s: want the age of services and updates only:\n%s", format, out)
		}
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(render(t, "json", "FormatDeviceHealth", staleTelemetry())), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["opnsense_update"] != "minor" || doc["stale"] != true || doc["heavy_age_sec"] == nil {
		t.Errorf("json %v", doc)
	}
}

// A live answer from a control plane that predates the fields shows what it
// always did, and a device with no snapshot is told about --retained.
func TestDeviceHealthFromAnOlderServerShowsWhatItDid(t *testing.T) {
	d := staleTelemetry()
	d.OPNsenseStatus, d.Stale, d.HeavyAgeSec = models.OPNsenseStatus{}, nil, nil
	for _, format := range []string{"table", "simple", "detailed"} {
		out := render(t, format, "FormatDeviceHealth", d)
		mustNotContain(t, format, out, "opnsense:", "stale", " old")
	}
	d.Snapshot = nil
	for _, format := range []string{"table", "simple", "detailed"} {
		mustContain(t, format, render(t, format, "FormatDeviceHealth", d), "--retained shows the last-known snapshot")
	}
}

// A stale snapshot whose age the server could not give still says it is last
// known, in every format.
func TestDeviceHealthMarksAStaleSnapshotWithoutAnAge(t *testing.T) {
	d := staleTelemetry()
	d.SnapshotAgeSec = nil
	for _, format := range []string{"table", "simple", "detailed"} {
		mustContain(t, format, render(t, format, "FormatDeviceHealth", d), "— (last known, stale)")
	}
}
