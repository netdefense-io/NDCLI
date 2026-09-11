package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// renderAll renders p through every formatter that implements
// FormatVpnPrefixProvisioned, keyed by format name.
func renderAll(t *testing.T, p VpnPrefixProvision) map[string]string {
	t.Helper()

	out := map[string]string{}

	var table bytes.Buffer
	if err := (&TableFormatter{BaseFormatter: BaseFormatter{Writer: &table}}).FormatVpnPrefixProvisioned(p); err != nil {
		t.Fatalf("table: %v", err)
	}
	out["table"] = table.String()

	var simple bytes.Buffer
	if err := (&SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &simple}}).FormatVpnPrefixProvisioned(p); err != nil {
		t.Fatalf("simple: %v", err)
	}
	out["simple"] = simple.String()

	var detailed bytes.Buffer
	if err := (&DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &detailed}}).FormatVpnPrefixProvisioned(p); err != nil {
		t.Fatalf("detailed: %v", err)
	}
	out["detailed"] = detailed.String()

	var jsonOut bytes.Buffer
	if err := (&JSONFormatter{BaseFormatter: BaseFormatter{Writer: &jsonOut}}).FormatVpnPrefixProvisioned(p); err != nil {
		t.Fatalf("json: %v", err)
	}
	out["json"] = jsonOut.String()

	return out
}

func base() VpnPrefixProvision {
	return VpnPrefixProvision{
		Network: "hq", Device: "fw01", Variable: "office-lan", Value: "10.20.0.0/24",
	}
}

// TestVpnPrefixProvisionRenderingNeverContradictsItself is the invariant this
// file exists for.
//
// "publish" names two things — creating the prefix, and the flag deciding
// whether peers see it — so any line claiming the prefix was published must
// not appear beside one saying it is not advertised, or that the flag was
// never read. The header, the step list and the note are three separate places
// that render the same state; fixing two of them and leaving the third is how
// the contradiction survived a wording fix once already.
func TestVpnPrefixProvisionRenderingNeverContradictsItself(t *testing.T) {
	cases := []VpnPrefixProvision{
		func() VpnPrefixProvision { // fresh provision
			p := base()
			p.OrgVariableCreated, p.DeviceVariableCreated, p.PrefixCreated = true, true, true
			p.Published = boolPtr(true)
			return p
		}(),
		func() VpnPrefixProvision { // the screen that contradicted itself
			p := base()
			p.Published = boolPtr(false)
			return p
		}(),
		func() VpnPrefixProvision { // read-back failed
			p := base()
			p.Published = nil
			return p
		}(),
		func() VpnPrefixProvision { // resumed partway
			p := base()
			p.DeviceVariableCreated, p.PrefixCreated = true, true
			p.Published = boolPtr(true)
			return p
		}(),
	}

	for _, p := range cases {
		for format, got := range renderAll(t, p) {
			if format == "json" {
				continue // structured fields, no prose to contradict
			}
			// Phrasing-independent on purpose: it matches the word, not a
			// sentence. An earlier version of this check looked for
			// "published on" and would have missed the header that actually
			// shipped — "Prefix ... published: ..." — which is the exact
			// failure it exists to catch.
			if !strings.Contains(got, "not advertised to peers") &&
				!strings.Contains(got, "could not be read back") {
				continue
			}

			// Whenever the note fires, PrefixCreated is false, so nothing on
			// this screen was published by this call. Remove the note lines,
			// which legitimately say "publish", and no form of the word may
			// remain anywhere else.
			rest := got
			for _, note := range []string{
				"publish=false — the prefix exists but is not advertised to peers.",
				"the prefix already existed; its publish flag could not be read back and is unknown.",
			} {
				rest = strings.ReplaceAll(rest, note, "")
			}
			if strings.Contains(rest, "publish") {
				t.Errorf("%s: a line other than the note claims something about publishing, on a screen that says the prefix is not advertised or its flag is unknown:\n%s", format, got)
			}
		}
	}
}

func TestVpnPrefixProvisionHeadline(t *testing.T) {
	tests := []struct {
		name          string
		prefixCreated bool
		want          string
		notWant       string
	}{
		{
			name:          "a prefix this run created is published",
			prefixCreated: true,
			want:          "published on fw01 in hq",
		},
		{
			// The regression: the header said "published" regardless, so a
			// re-run against an existing publish=false prefix announced a
			// publish in its first line and denied it three lines later.
			name:          "a prefix that was already there is not",
			prefixCreated: false,
			want:          "already exists on fw01 in hq",
			notWant:       "published on",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base()
			p.PrefixCreated = tt.prefixCreated
			p.Published = boolPtr(true)

			for format, got := range renderAll(t, p) {
				if format == "json" {
					continue
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("%s: missing %q:\n%s", format, tt.want, got)
				}
				if tt.notWant != "" && strings.Contains(got, tt.notWant) {
					t.Errorf("%s: should not claim %q:\n%s", format, tt.notWant, got)
				}
			}
		})
	}
}

func TestVpnPrefixProvisionSteps(t *testing.T) {
	p := base()
	p.OrgVariableCreated = true

	got := renderAll(t, p)["table"]

	for _, want := range []string{
		`created organization-scope variable "office-lan"`,
		`device-scope override on "fw01" already had this value`,
		`prefix already exists on "hq"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("step line missing %q:\n%s", want, got)
		}
	}
	// "published" is the word that caused the trouble; the already-there steps
	// must not use it.
	if strings.Contains(got, "already published") {
		t.Errorf(`"already published" reintroduces the ambiguity:\n%s`, got)
	}
}

func TestVpnPrefixProvisionPublishNote(t *testing.T) {
	t.Run("publish=true says nothing extra", func(t *testing.T) {
		p := base()
		p.PrefixCreated = true
		p.Published = boolPtr(true)

		if got := renderAll(t, p)["table"]; strings.Contains(got, "Note:") {
			t.Errorf("no caveat is warranted here:\n%s", got)
		}
	})

	t.Run("publish=false and unknown both get the label", func(t *testing.T) {
		for _, published := range []*bool{boolPtr(false), nil} {
			p := base()
			p.Published = published

			got := renderAll(t, p)["table"]
			if !strings.Contains(got, "Note:") {
				t.Errorf("published=%v should carry a note; an unknown state must not be quieter than a false one:\n%s", published, got)
			}
		}
	})
}

// TestVpnPrefixProvisionJSON: the machine format carries the tri-state as a
// real null rather than omitting the key or guessing a value.
func TestVpnPrefixProvisionJSON(t *testing.T) {
	p := base()
	p.Published = nil

	var doc map[string]interface{}
	raw := renderAll(t, p)["json"]
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("json output is not one parseable document: %v\n%s", err, raw)
	}

	published, present := doc["published"]
	if !present {
		t.Fatalf("published must be present as null, not omitted:\n%s", raw)
	}
	if published != nil {
		t.Errorf("published = %v, want null when the flag was never read back", published)
	}
	if doc["prefix_created"] != false {
		t.Errorf("prefix_created = %v, want false", doc["prefix_created"])
	}
}
