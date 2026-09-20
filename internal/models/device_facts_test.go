package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

const deviceWithFactsJSON = `{
  "uuid": "1b0f0a2e-0000-4000-8000-000000000001",
  "name": "e2e-a",
  "status": "ENABLED",
  "organization": "acme",
  "facts": {
    "v": 1,
    "hash": "a1b2c3d4e5f60718",
    "timezone": {"name": "America/Sao_Paulo", "utc_offset_sec": -10800, "abbrev": "-03"},
    "interfaces": [
      {"role": "wan", "if": "em0", "descr": "WAN", "enabled": true},
      {"role": "lan", "if": "em1", "descr": "LAN", "enabled": true},
      {"role": "opt1", "if": "em2", "descr": "GUEST", "enabled": false}
    ],
    "opnsense": {"version": "26.1.9", "series": "26.1"},
    "os": {"platform": "FreeBSD", "version": "15.0-RELEASE"},
    "hostname": "e2e-a.lab.netdefense.io"
  }
}`

// TestDeviceDecodesFacts is the baseline: every fact in the contract shape
// reaches a typed accessor.
func TestDeviceDecodesFacts(t *testing.T) {
	var d Device
	if err := json.Unmarshal([]byte(deviceWithFactsJSON), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !d.HasFacts() {
		t.Fatal("expected the device to carry facts")
	}
	tz, ok := d.TimezoneFact()
	if !ok {
		t.Fatal("expected a timezone fact")
	}
	if tz.Name != "America/Sao_Paulo" || tz.UTCOffsetSec != -10800 || tz.Abbrev != "-03" {
		t.Fatalf("timezone fact decoded wrong: %+v", tz)
	}
	if got, want := tz.Display(), "America/Sao_Paulo (UTC-03:00)"; got != want {
		t.Fatalf("timezone display = %q, want %q", got, want)
	}
	if got, want := d.InterfacesSummary(), "wan=em0 lan=em1 opt1=em2 (disabled: opt1)"; got != want {
		t.Fatalf("interfaces summary = %q, want %q", got, want)
	}
	opn, ok := d.OPNsenseFact()
	if !ok || opn.Version != "26.1.9" || opn.Series != "26.1" {
		t.Fatalf("opnsense fact decoded wrong: %+v (ok=%v)", opn, ok)
	}
	os, ok := d.OSFact()
	if !ok || os.Display() != "FreeBSD 15.0-RELEASE" {
		t.Fatalf("os fact decoded wrong: %+v (ok=%v)", os, ok)
	}
	host, ok := d.HostnameFact()
	if !ok || host != "e2e-a.lab.netdefense.io" {
		t.Fatalf("hostname fact decoded wrong: %q (ok=%v)", host, ok)
	}
}

// TestDeviceWithoutFactsIsUnchanged pins the older-agent case: no facts key
// at all must leave every accessor quiet and every surface rendering exactly
// what it rendered before facts existed.
func TestDeviceWithoutFactsIsUnchanged(t *testing.T) {
	var d Device
	if err := json.Unmarshal([]byte(`{"name":"legacy","status":"ENABLED"}`), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.HasFacts() {
		t.Fatal("a device with no facts key must report no facts")
	}
	if _, ok := d.TimezoneFact(); ok {
		t.Fatal("expected no timezone fact")
	}
	if got := d.InterfacesSummary(); got != "" {
		t.Fatalf("expected no interfaces summary, got %q", got)
	}
	if _, ok := d.OPNsenseFact(); ok {
		t.Fatal("expected no opnsense fact")
	}
	if _, ok := d.OSFact(); ok {
		t.Fatal("expected no os fact")
	}
	if _, ok := d.HostnameFact(); ok {
		t.Fatal("expected no hostname fact")
	}
	if got := d.UnknownFactKeys(); got != nil {
		t.Fatalf("expected no unknown keys, got %v", got)
	}
	// The field is omitempty, so a device that reported nothing must not
	// grow a null facts key on the way back out.
	out, err := json.Marshal(&d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]interface{}
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	if _, present := round["facts"]; present {
		t.Fatalf("facts must be omitted entirely when absent: %s", out)
	}
}

// TestDeviceFactsUnknownKeysAreCargo is the forward-compatibility contract:
// a fact this binary has never heard of survives a decode/encode round trip
// untouched, and is named rather than dropped for a human reader.
func TestDeviceFactsUnknownKeysAreCargo(t *testing.T) {
	raw := `{"name":"newer","facts":{"v":1,"hostname":"fw","ntp":{"synced":true,"server":"pool.ntp.org"},"hardware_model":"DEC750"}}`
	var d Device
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := d.UnknownFactKeys(), []string{"hardware_model", "ntp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown fact keys = %v, want %v", got, want)
	}
	out, err := json.Marshal(&d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]interface{}
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	facts, ok := round["facts"].(map[string]interface{})
	if !ok {
		t.Fatalf("facts missing from round trip: %s", out)
	}
	ntp, ok := facts["ntp"].(map[string]interface{})
	if !ok || ntp["server"] != "pool.ntp.org" || ntp["synced"] != true {
		t.Fatalf("unknown fact was not carried through verbatim: %s", out)
	}
	if facts["hardware_model"] != "DEC750" {
		t.Fatalf("unknown scalar fact was not carried through: %s", out)
	}
}

// TestDeviceFactsToleratesWrongTypes covers the unsigned channel: facts are
// agent-reported and nothing here may panic or report nonsense because a
// device sent the wrong JSON type.
func TestDeviceFactsToleratesWrongTypes(t *testing.T) {
	cases := map[string]string{
		"timezone is a string":        `{"facts":{"timezone":"America/Sao_Paulo"}}`,
		"timezone name is a number":   `{"facts":{"timezone":{"name":42,"utc_offset_sec":-10800}}}`,
		"timezone is null":            `{"facts":{"timezone":null}}`,
		"offset is a string":          `{"facts":{"timezone":{"name":"UTC","utc_offset_sec":"0"}}}`,
		"interfaces is an object":     `{"facts":{"interfaces":{"wan":"em0"}}}`,
		"interface entry is a string": `{"facts":{"interfaces":["em0"]}}`,
		"os is a list":                `{"facts":{"os":[1,2,3]}}`,
		"hostname is a bool":          `{"facts":{"hostname":true}}`,
		"facts is empty":              `{"facts":{}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var d Device
			if err := json.Unmarshal([]byte(raw), &d); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// None of these must panic, and none may invent a value.
			if tz, ok := d.TimezoneFact(); ok && tz.Name == "" {
				t.Fatal("a timezone fact was reported with no name")
			}
			if tz, ok := d.TimezoneFact(); ok && tz.Name == "UTC" && tz.UTCOffsetSec != 0 {
				t.Fatalf("a non-integer offset must read as 0, got %d", tz.UTCOffsetSec)
			}
			_ = d.InterfacesSummary()
			_, _ = d.OPNsenseFact()
			_, _ = d.OSFact()
			if host, ok := d.HostnameFact(); ok && host == "" {
				t.Fatal("an empty hostname must not be reported as present")
			}
		})
	}
}

// TestInterfacesSummaryShapes pins the compact line's edge cases: a missing
// enabled flag means enabled (an agent that omits it must not render every
// interface as disabled), and a half-filled entry still prints what it has.
func TestInterfacesSummaryShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "enabled omitted means enabled",
			raw:  `{"facts":{"interfaces":[{"role":"wan","if":"em0"},{"role":"lan","if":"em1"}]}}`,
			want: "wan=em0 lan=em1",
		},
		{
			name: "several disabled are named once each",
			raw:  `{"facts":{"interfaces":[{"role":"wan","if":"em0","enabled":true},{"role":"opt1","if":"em2","enabled":false},{"role":"opt2","if":"em3","enabled":false}]}}`,
			want: "wan=em0 opt1=em2 opt2=em3 (disabled: opt1, opt2)",
		},
		{
			name: "role with no interface prints the role alone",
			raw:  `{"facts":{"interfaces":[{"role":"wan","enabled":true}]}}`,
			want: "wan",
		},
		{
			name: "interface with no role prints the interface alone",
			raw:  `{"facts":{"interfaces":[{"if":"em0","enabled":true}]}}`,
			want: "em0",
		},
		{
			name: "entry with neither is dropped",
			raw:  `{"facts":{"interfaces":[{"descr":"nothing"},{"role":"lan","if":"em1","enabled":true}]}}`,
			want: "lan=em1",
		},
		{
			name: "empty list renders nothing",
			raw:  `{"facts":{"interfaces":[]}}`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d Device
			if err := json.Unmarshal([]byte(tc.raw), &d); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := d.InterfacesSummary(); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatUTCOffset(t *testing.T) {
	cases := map[int]string{
		0:      "UTC+00:00",
		-10800: "UTC-03:00",
		19800:  "UTC+05:30",
		-1800:  "UTC-00:30",
		50400:  "UTC+14:00",
		-45900: "UTC-12:45",
	}
	for sec, want := range cases {
		if got := FormatUTCOffset(sec); got != want {
			t.Fatalf("FormatUTCOffset(%d) = %q, want %q", sec, got, want)
		}
	}
}
