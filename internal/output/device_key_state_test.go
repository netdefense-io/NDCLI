package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func mismatchDevice(t *testing.T) *models.Device {
	t.Helper()
	raw := `{
	  "name": "bt007", "uuid": "1b0f0a2e-0000-4000-8000-000000000002",
	  "status": "ENABLED", "organization": "acme",
	  "key_state": "mismatch",
	  "key_state_detail": {"bound_kid": "aa11", "presented_kid": "bb22", "source": "auth", "since": "2026-10-27T10:00:00Z"}
	}`
	var d models.Device
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &d
}

const (
	wantMismatch = "KEY MISMATCH - rebind required"
	wantRebind   = "ndcli device rebind-token bt007"
)

func assertMismatch(t *testing.T, format, out string) {
	t.Helper()
	for _, want := range []string{wantMismatch, wantRebind} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s: expected %q in:\n%s", format, want, out)
		}
	}
}

func assertNoMismatch(t *testing.T, format, out string) {
	t.Helper()
	for _, unwanted := range []string{"KEY MISMATCH", "rebind", "key_state"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("%s: unexpected %q in:\n%s", format, unwanted, out)
		}
	}
}

func TestKeyMismatchAllFormats(t *testing.T) {
	d := mismatchDevice(t)
	list := []models.Device{*d}

	var buf bytes.Buffer
	s := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	_ = s.FormatDevice(d)
	assertMismatch(t, "simple describe", buf.String())
	buf.Reset()
	_ = s.FormatDevices(list, 1, nil)
	assertMismatch(t, "simple list", buf.String())

	buf.Reset()
	df := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	_ = df.FormatDevice(d)
	assertMismatch(t, "detailed describe", buf.String())
	if !strings.Contains(buf.String(), "bb22") {
		t.Fatalf("detailed should show the presented kid:\n%s", buf.String())
	}

	tf := &TableFormatter{}
	assertMismatch(t, "table describe", captureStdout(t, func() { _ = tf.FormatDevice(d) }))
	assertMismatch(t, "table list", captureStdout(t, func() { _ = tf.FormatDevices(list, 1, nil) }))

	for name, v := range map[string]interface{}{"device": d, "list": map[string]interface{}{"devices": list}} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out := string(b)
		for _, want := range []string{`"key_state":"mismatch"`, `"rebind_command":"ndcli device rebind-token bt007"`, `"presented_kid":"bb22"`} {
			if !strings.Contains(out, want) {
				t.Fatalf("json %s: expected %s in %s", name, want, out)
			}
		}
	}
}

func TestNoKeyStateRendersNothingNew(t *testing.T) {
	d := &models.Device{Name: "bt007", Status: "ENABLED", Organization: "acme"}
	list := []models.Device{*d}

	var buf bytes.Buffer
	s := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	_ = s.FormatDevice(d)
	_ = s.FormatDevices(list, 1, nil)
	df := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	_ = df.FormatDevice(d)
	assertNoMismatch(t, "simple/detailed", buf.String())

	tf := &TableFormatter{}
	assertNoMismatch(t, "table", captureStdout(t, func() {
		_ = tf.FormatDevice(d)
		_ = tf.FormatDevices(list, 1, nil)
	}))

	b, _ := json.Marshal(d)
	assertNoMismatch(t, "json", string(b))

	// An explicit null from NDManager is the same as absent.
	var n models.Device
	if err := json.Unmarshal([]byte(`{"name":"x","key_state":null,"key_state_detail":null}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.HasKeyMismatch() || n.KeyStateDetail != nil {
		t.Fatalf("null key_state must not flag: %+v", n)
	}
}
