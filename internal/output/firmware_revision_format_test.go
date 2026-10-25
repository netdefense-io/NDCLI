package output

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestFirmwarePendingText(t *testing.T) {
	for _, tc := range []struct {
		in   *models.FirmwarePending
		want string
	}{
		{nil, ""},
		{&models.FirmwarePending{Count: 0}, ""},
		{&models.FirmwarePending{Count: 17, Kind: "hotfix"}, "Hotfix: 17 package updates pending"},
		{&models.FirmwarePending{Count: 1, Kind: "hotfix"}, "Hotfix: 1 package update pending"},
		{&models.FirmwarePending{Count: 3, Kind: "update"}, "3 package updates pending"},
	} {
		if got := firmwarePendingText(tc.in); got != tc.want {
			t.Errorf("firmwarePendingText(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func revisionDeviceList() *models.FirmwareDeviceList {
	return &models.FirmwareDeviceList{Total: 1, Items: []models.FirmwareDevice{{
		DeviceName: "fw-a", DeviceStatus: "ENABLED", Track: "fast", State: "enrolled", PinnedVersion: "26.1.10",
		Revision: intRef(2), Blocked: models.FirmwareBlockedAgentUpgradeRequired, AgentVersion: "1.27.0",
		Pending:       &models.FirmwarePending{Count: 17, Kind: "hotfix", CheckedAt: "2026-10-23T18:14:08Z"},
		HotfixWarning: &models.FirmwareHotfixWarning{Release: "26.1.11", Revision: 2, ApprovedInTrack: false},
	}}}
}

func TestFirmwareDevicesShowRevisionHotfixAndBlock(t *testing.T) {
	fs, bufs := plainFormatters(t)
	for _, name := range []string{"table", "simple", "detailed"} {
		if err := fs[name].FormatFirmwareDevices(revisionDeviceList()); err != nil {
			t.Fatal(err)
		}
		out := bufs[name].String()
		for _, want := range []string{"26.1.10 rev 2", "Hotfix: 17 package updates pending", "agent upgrade required", "26.1.11"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s output lacks %q:\n%s", name, want, out)
			}
		}
	}
	if err := fs["json"].FormatFirmwareDevices(revisionDeviceList()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"pending"`, `"hotfix_warning"`, `"revision": 2`} {
		if !strings.Contains(bufs["json"].String(), want) {
			t.Errorf("json lacks %s:\n%s", want, bufs["json"].String())
		}
	}
}

func TestFirmwareBoardShowsRevisions(t *testing.T) {
	r := &models.FirmwareRollout{OrganizationName: "acme", Tracks: []models.FirmwareTrackStatus{{
		Track: "fast", Series: []models.FirmwareSeriesStatus{{
			Series: "26.1", Target: "26.1.10", TargetRevision: intRef(1),
			LatestRelease: &models.FirmwareLatestRelease{Version: "26.1.11", LatestRevision: 2},
			NextCandidate: &models.FirmwareCandidate{Version: "26.1.11", Revision: intRef(2), State: "soaking"},
		}},
	}}, Failures: []models.FirmwareFailure{{Version: "26.1.10", Revision: intRef(1), Track: "fast", Devices: []string{"fw-a"}, Count: 1}},
		Halts: []models.FirmwareHalt{{Version: "26.1.11", Revision: intRef(2), Kind: "minor", Reason: "r", HaltedBy: "a"}}}
	fs, bufs := plainFormatters(t)
	for _, name := range []string{"table", "simple", "detailed"} {
		if err := fs[name].FormatFirmwareRollout(r); err != nil {
			t.Fatal(err)
		}
		out := bufs[name].String()
		for _, want := range []string{"26.1.10 rev 1", "26.1.11 rev 2", "latest 26.1.11 rev 2"} {
			if name == "detailed" && want == "latest 26.1.11 rev 2" {
				want = "26.1.11 rev 2"
			}
			if !strings.Contains(out, want) {
				t.Errorf("%s output lacks %q:\n%s", name, want, out)
			}
		}
	}
}

func TestFirmwareRevisionActionsShowTheRevision(t *testing.T) {
	fs, bufs := plainFormatters(t)
	p := &models.FirmwarePromotion{Version: "26.1.11", Revision: intRef(2), Results: []models.FirmwarePromotionResult{
		{Track: "fast", Result: "approved", Target: "26.1.11", TargetRevision: intRef(2)}}}
	c := &models.FirmwareFailuresCleared{Version: "26.1.11", Revision: intRef(2), Cleared: 1}
	h := &models.FirmwareHaltResult{Halt: models.FirmwareHalt{Version: "26.1.11", Revision: intRef(2), Kind: "minor", Reason: "r", HaltedBy: "a"}}
	for _, name := range []string{"table", "simple", "detailed"} {
		_ = fs[name].FormatFirmwarePromotion(p)
		_ = fs[name].FormatFirmwareFailuresCleared(c)
		_ = fs[name].FormatFirmwareHalt(h)
		if n := strings.Count(bufs[name].String(), "26.1.11 rev 2"); n < 3 {
			t.Errorf("%s shows the revision %d time(s), want at least 3:\n%s", name, n, bufs[name].String())
		}
	}
}

func TestFirmwareLatestNote(t *testing.T) {
	latest := &models.FirmwareLatestRelease{Version: "26.1.11", LatestRevision: 2}
	for _, tc := range []struct {
		name string
		s    models.FirmwareSeriesStatus
		want string
	}{
		{"behind", models.FirmwareSeriesStatus{Target: "26.1.10", TargetRevision: intRef(1), LatestRelease: latest}, "latest 26.1.11 rev 2"},
		{"same release, older revision", models.FirmwareSeriesStatus{Target: "26.1.11", TargetRevision: intRef(1), LatestRelease: latest}, "latest 26.1.11 rev 2"},
		{"at latest", models.FirmwareSeriesStatus{Target: "26.1.11", TargetRevision: intRef(2), LatestRelease: latest}, ""},
		{"at latest, no revision known", models.FirmwareSeriesStatus{Target: "26.1.11", LatestRelease: latest}, ""},
		{"no latest", models.FirmwareSeriesStatus{Target: "26.1.11"}, ""},
	} {
		if got := firmwareLatestNote(tc.s); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
