package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// deviceWithFacts builds the device every test here renders: the shape the
// contract defines, with one disabled interface so the compact line's
// trailing note is exercised too.
func deviceWithFacts(t *testing.T) *models.Device {
	t.Helper()
	raw := `{
	  "name": "e2e-a", "uuid": "1b0f0a2e-0000-4000-8000-000000000001",
	  "status": "ENABLED", "organization": "acme",
	  "facts": {
	    "v": 1,
	    "timezone": {"name": "America/Sao_Paulo", "utc_offset_sec": -10800, "abbrev": "-03"},
	    "interfaces": [
	      {"role": "wan", "if": "em0", "enabled": true},
	      {"role": "lan", "if": "em1", "enabled": true},
	      {"role": "opt1", "if": "em2", "enabled": false}
	    ],
	    "opnsense": {"version": "26.1.9", "series": "26.1"},
	    "os": {"platform": "FreeBSD", "version": "15.0-RELEASE"},
	    "hostname": "e2e-a.lab.netdefense.io"
	  }
	}`
	var d models.Device
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &d
}

// factSubstrings are the renderings every human-facing describe must carry
// when the device reports facts.
var factSubstrings = []string{
	"Timezone",
	"America/Sao_Paulo (UTC-03:00)",
	"Interfaces",
	"wan=em0 lan=em1 opt1=em2 (disabled: opt1)",
	"OPNsense",
	"26.1.9",
	"FreeBSD 15.0-RELEASE",
	"e2e-a.lab.netdefense.io",
}

func TestDetailedFormatDeviceRendersFacts(t *testing.T) {
	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatDevice(deviceWithFacts(t)); err != nil {
		t.Fatalf("FormatDevice: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Facts") {
		t.Fatalf("expected a Facts section, got:\n%s", out)
	}
	for _, want := range factSubstrings {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in detailed output:\n%s", want, out)
		}
	}
}

// TestDetailedFormatDeviceWithoutFactsRendersNoSection is the older-agent
// guarantee: a device that reports nothing renders exactly what it did before
// facts existed, with no empty box and no placeholder.
func TestDetailedFormatDeviceWithoutFactsRendersNoSection(t *testing.T) {
	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	d := &models.Device{Name: "legacy", Status: "ENABLED", Organization: "acme"}
	if err := f.FormatDevice(d); err != nil {
		t.Fatalf("FormatDevice: %v", err)
	}
	out := buf.String()
	for _, unwanted := range []string{"Facts", "Timezone", "Interfaces", "Hostname"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("a device with no facts must not render %q:\n%s", unwanted, out)
		}
	}
}

func TestSimpleFormatDeviceRendersFacts(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatDevice(deviceWithFacts(t)); err != nil {
		t.Fatalf("FormatDevice: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "  Facts:\n") {
		t.Fatalf("expected a Facts heading in simple output:\n%s", out)
	}
	for _, want := range factSubstrings {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in simple output:\n%s", want, out)
		}
	}
	// The fact lines sit one level under the heading, not level with the
	// control-plane fields above it.
	if !strings.Contains(out, "    Hostname: e2e-a.lab.netdefense.io\n") {
		t.Fatalf("expected fact lines indented under the heading:\n%s", out)
	}

	buf.Reset()
	if err := f.FormatDevice(&models.Device{Name: "legacy", Status: "ENABLED"}); err != nil {
		t.Fatalf("FormatDevice: %v", err)
	}
	for _, unwanted := range []string{"Facts:", "Timezone"} {
		if strings.Contains(buf.String(), unwanted) {
			t.Fatalf("a device with no facts must not render %q:\n%s", unwanted, buf.String())
		}
	}
}

// renderDeviceTable captures stdout: the table formatter prints there
// directly, so a buffer on the formatter would come back empty.
func renderDeviceTable(t *testing.T, d *models.Device) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	f := NewTableFormatter()
	formatErr := f.FormatDevice(d)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if formatErr != nil {
		t.Fatal(formatErr)
	}
	return buf.String()
}

func TestTableFormatDeviceRendersFacts(t *testing.T) {
	out := renderDeviceTable(t, deviceWithFacts(t))
	// Without the heading the fact lines read as more control-plane fields,
	// which is what the reader of a describe would take them for.
	if !strings.Contains(out, "\nFacts:\n") {
		t.Fatalf("expected a Facts heading in table output:\n%s", out)
	}
	for _, want := range factSubstrings {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in table output:\n%s", want, out)
		}
	}
	// Indented under the heading, values aligned with the block above it.
	if !strings.Contains(out, "  Timezone:   America/Sao_Paulo (UTC-03:00)\n") {
		t.Fatalf("expected aligned fact lines under the heading:\n%s", out)
	}
}

func TestTableFormatDeviceWithoutFactsRendersNoFactLines(t *testing.T) {
	out := renderDeviceTable(t, &models.Device{Name: "legacy", Status: "ENABLED"})
	for _, unwanted := range []string{"Facts:", "Timezone", "Interfaces", "Hostname", "OPNsense"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("a device with no facts must not render %q:\n%s", unwanted, out)
		}
	}
}

// TestTableFormatDevicesGrowsNoFactsColumn pins the deliberate omission: the
// list view keeps its columns. A fleet mostly shares one timezone, so a
// column would repeat one value down every row at the cost of width the
// columns that vary need.
func TestTableFormatDevicesGrowsNoFactsColumn(t *testing.T) {
	d := deviceWithFacts(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	f := NewTableFormatter()
	formatErr := f.FormatDevices([]models.Device{*d}, 1, nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdout
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if formatErr != nil {
		t.Fatal(formatErr)
	}
	if strings.Contains(buf.String(), "America/Sao_Paulo") {
		t.Fatalf("the device list must not carry a facts column:\n%s", buf.String())
	}
}

// TestJSONFormatDeviceCarriesRawFacts covers the machine-readable surface,
// unknown keys included — nothing in the JSON path filters the map.
func TestJSONFormatDeviceCarriesRawFacts(t *testing.T) {
	d := deviceWithFacts(t)
	d.Facts["ntp"] = map[string]interface{}{"synced": true}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	f := NewJSONFormatter()
	formatErr := f.FormatDevice(d)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdout
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if formatErr != nil {
		t.Fatal(formatErr)
	}

	var round map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, buf.String())
	}
	facts, ok := round["facts"].(map[string]interface{})
	if !ok {
		t.Fatalf("facts missing from json output:\n%s", buf.String())
	}
	if _, ok := facts["ntp"]; !ok {
		t.Fatalf("an unknown fact must survive to json output:\n%s", buf.String())
	}
	tz, ok := facts["timezone"].(map[string]interface{})
	if !ok || tz["name"] != "America/Sao_Paulo" {
		t.Fatalf("timezone fact missing from json output:\n%s", buf.String())
	}
}

// TestDeviceFactLinesNamesUnknownKeys: a human reader is told the device
// reported something this binary cannot lay out, rather than the fact being
// silently dropped from every format but json.
func TestDeviceFactLinesNamesUnknownKeys(t *testing.T) {
	var d models.Device
	if err := json.Unmarshal([]byte(`{"facts":{"hostname":"fw","ntp":{"synced":true}}}`), &d); err != nil {
		t.Fatal(err)
	}
	lines := deviceFactLines(&d)
	var found bool
	for _, l := range lines {
		if l[0] == "Other" && strings.Contains(l[1], "ntp") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an Other line naming the unknown fact, got %v", lines)
	}
}
