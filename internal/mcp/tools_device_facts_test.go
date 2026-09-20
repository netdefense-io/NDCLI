package mcp

import (
	"encoding/json"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// mcpDeviceWithFacts is the contract-shaped device the MCP map tests render.
func mcpDeviceWithFacts(t *testing.T) *models.Device {
	t.Helper()
	raw := `{
	  "name": "e2e-a", "uuid": "1b0f0a2e-0000-4000-8000-000000000001",
	  "status": "ENABLED", "organization": "acme",
	  "facts": {
	    "v": 1,
	    "timezone": {"name": "America/Sao_Paulo", "utc_offset_sec": -10800, "abbrev": "-03"},
	    "interfaces": [{"role": "wan", "if": "em0", "enabled": true}],
	    "opnsense": {"version": "26.1.9", "series": "26.1"},
	    "os": {"platform": "FreeBSD", "version": "15.0-RELEASE"},
	    "hostname": "e2e-a.lab.netdefense.io",
	    "ntp": {"synced": true}
	  }
	}`
	var d models.Device
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &d
}

// TestDeviceMapsCarryFacts guards the gotcha these two maps embody: they are
// hand-built, so a new field on models.Device reaches the CLI's json
// formatter for free and the MCP surface not at all unless it is added here.
func TestDeviceMapsCarryFacts(t *testing.T) {
	d := mcpDeviceWithFacts(t)
	for name, m := range map[string]map[string]interface{}{
		"deviceSummary": deviceSummary(d),
		"deviceFull":    deviceFull(d),
	} {
		t.Run(name, func(t *testing.T) {
			facts, ok := m["facts"].(map[string]interface{})
			if !ok {
				t.Fatalf("%s dropped facts: %v", name, m)
			}
			tz, ok := facts["timezone"].(map[string]interface{})
			if !ok || tz["name"] != "America/Sao_Paulo" {
				t.Fatalf("%s did not carry the timezone fact: %v", name, facts)
			}
			// Unknown keys reach the caller untouched: the consumer is a
			// model, and a fact with no accessor here is still readable.
			if _, ok := facts["ntp"]; !ok {
				t.Fatalf("%s filtered an unknown fact: %v", name, facts)
			}
		})
	}
}

// TestDeviceMapsOmitAbsentFacts keeps an older agent's device from growing a
// null facts key that a model could read as "reported nothing configured".
func TestDeviceMapsOmitAbsentFacts(t *testing.T) {
	d := &models.Device{Name: "legacy", Status: "ENABLED"}
	for name, m := range map[string]map[string]interface{}{
		"deviceSummary": deviceSummary(d),
		"deviceFull":    deviceFull(d),
	} {
		if _, present := m["facts"]; present {
			t.Fatalf("%s must omit facts entirely when the device reports none: %v", name, m)
		}
	}
}
