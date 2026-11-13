package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// confFor writes a config that points ndcli at srv, and returns the home and
// the config path a child run takes.
func confFor(t *testing.T, srv *httptest.Server) (home, conf string) {
	t.Helper()
	home = t.TempDir()
	conf = filepath.Join(home, "config.yaml")
	if err := os.WriteFile(conf, []byte("controlplane:\n  host: "+srv.URL+"\n  ssl_verify: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, conf
}

// A sync that triggered some devices and refused others (HTTP 207) prints its
// result and exits ExitPartial, which a script can tell from success (0) and
// from a sync that triggered nothing (1) (#255).
func TestSyncApplyPartlyRefusedExitsPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `{"message":"Sync triggered for 1 device","devices_affected":1,"skipped":0,`+
			`"tasks":[{"task":"T1","device_name":"fw01"}],`+
			`"errors":[{"device_name":"fw02","error":"Undefined variables in snippet content","code":"UNDEFINED_VARIABLES"}]}`)
	}))
	defer srv.Close()
	home, conf := confFor(t, srv)
	for _, format := range []string{"table", "json"} {
		stdout, stderr, code := runNdcli(t, home, "", "--conf", conf, "-o", "acme", "-f", format, "sync", "apply", "-y")
		if code != ExitPartial {
			t.Errorf("%s: exit %d, want %d\nstderr: %s", format, code, ExitPartial, stderr)
		}
		if !strings.Contains(stdout, "fw02") || !strings.Contains(stderr, "1 of 2 devices failed") {
			t.Errorf("%s: stdout %q, stderr %q", format, stdout, stderr)
		}
	}
}

// approve-all exits 1 when every approval fails, ExitPartial when some do,
// and 0 when none does. It prints its summary first, and an error that names
// the outcome; when every refusal shares a server code, the code survives.
func TestApproveAllExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name, format string
		failOn       map[string]bool
		want         int
		stderr       string
	}{
		{"every approval fails", "table", map[string]bool{"fw01": true, "fw02": true}, 1, "Error: [INVALID_STATE] every approval failed (2 devices)"},
		{"every approval fails, json", "json", map[string]bool{"fw01": true, "fw02": true}, 1, `"code": "INVALID_STATE"`},
		{"one approval fails", "table", map[string]bool{"fw02": true}, ExitPartial, "Error: 1 of 2 approvals failed"},
		{"none fails", "table", nil, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"items":[{"name":"fw01","status":"PENDING"},{"name":"fw02","status":"PENDING"}],"total":2}`)
					return
				}
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/organizations/acme/devices/"), "/approve")
				if tc.failOn[name] {
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, `{"error":"Device cannot be approved","code":"INVALID_STATE"}`)
					return
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			home, conf := confFor(t, srv)
			stdout, stderr, code := runNdcli(t, home, "", "--conf", conf, "-o", "acme", "-f", tc.format, "device", "approve-all", "-y")
			if code != tc.want {
				t.Errorf("exit %d, want %d\nstderr: %s", code, tc.want, stderr)
			}
			if tc.format == "json" {
				var doc struct {
					Approved      int                      `json:"approved"`
					ApprovedNames []string                 `json:"approved_names"`
					FailedCount   int                      `json:"failed_count"`
					Failed        []map[string]interface{} `json:"failed"`
				}
				decodeOne(t, stdout, &doc)
				if doc.Approved+doc.FailedCount != 2 || len(doc.ApprovedNames)+len(doc.Failed) != 2 {
					t.Errorf("json %+v, want both devices", doc)
				}
			} else if !strings.Contains(stdout, "Approved: ") {
				t.Errorf("stdout %q lacks the summary", stdout)
			}
			if tc.stderr != "" && !strings.Contains(stderr, tc.stderr) {
				t.Errorf("stderr %q lacks %q", stderr, tc.stderr)
			}
			if tc.stderr == "" && stderr != "" {
				t.Errorf("stderr %q, want none", stderr)
			}
		})
	}
}

// A sync that refused nothing exits 0.
func TestSyncApplyWithoutRefusalsExitsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":"Sync triggered for 2 devices","devices_affected":2,"skipped":1,`+
			`"tasks":[{"task":"T1","device_name":"fw01"},{"task":"T2","device_name":"fw02"}]}`)
	}))
	defer srv.Close()
	home, conf := confFor(t, srv)
	if _, stderr, code := runNdcli(t, home, "", "--conf", conf, "-o", "acme", "sync", "apply", "-y"); code != 0 {
		t.Errorf("exit %d, want 0\nstderr: %s", code, stderr)
	}
}
