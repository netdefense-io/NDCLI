package tui

import (
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The dashboard's OPNsense panel shows the control plane's histogram, which
// also counts devices silent past the snapshot TTL, in its order, and names
// the newest release; without it, the rows' own versions are counted (#256).
func TestOPNsensePanelPrefersTheServersHistogram(t *testing.T) {
	d := &models.DashboardResponse{
		OPNsenseVersions:      []models.DashboardAgentVersion{{Version: "26.7.8_2", Count: 2}, {Version: "26.1.9", Count: 3}},
		LatestOPNsenseVersion: "26.7.9",
		Compact:               []models.DashboardCompactRow{{Name: "fw-01", OPNsenseStatus: models.OPNsenseStatus{OPNsenseVersion: "25.7.1"}}},
	}
	items := opnsenseItems(d)
	if len(items) != 2 || items[0].label != "26.7.8_2" || items[0].count != 2 || items[1].label != "26.1.9" {
		t.Errorf("items = %+v, want the server's histogram in its order", items)
	}
	if got := opnsensePanelTitle(d); got != "OPNsense · latest 26.7.9" {
		t.Errorf("title = %q", got)
	}

	older := &models.DashboardResponse{Compact: []models.DashboardCompactRow{
		{Name: "fw-01", OPNsenseStatus: models.OPNsenseStatus{OPNsenseVersion: "26.1.9"}},
		{Name: "fw-02", OPNsenseStatus: models.OPNsenseStatus{OPNsenseVersion: "26.1.9"}},
		{Name: "fw-03"},
	}}
	items = opnsenseItems(older)
	if len(items) != 1 || items[0].label != "26.1.9" || items[0].count != 2 {
		t.Errorf("older server: items = %+v, want the rows counted", items)
	}
	if got := opnsensePanelTitle(older); got != "OPNsense versions" {
		t.Errorf("older server: title = %q", got)
	}
}
