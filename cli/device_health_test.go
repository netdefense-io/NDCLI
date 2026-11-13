package cli

import (
	"io"
	"net/http"
	"testing"
)

// device health asks for retained telemetry only with --retained (#256).
func TestDeviceHealthRetainedFlag(t *testing.T) {
	cmd := findCommand(t, "device", "health")
	for _, tc := range []struct {
		flags map[string]string
		want  string
	}{
		{nil, ""},
		{map[string]string{"retained": "true"}, "retained=true"},
	} {
		var query string
		handler := func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/organizations/acme/devices/fw-01/telemetry" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			query = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fw-01","sync":{"state":"in_sync"},"stale":false,"as_of":1}`)
		}
		reset := setFlags(t, cmd, tc.flags)
		runStreams(t, handler, "json", "", func() error { return runDeviceHealth(cmd, []string{"fw-01"}) })
		reset()
		if query != tc.want {
			t.Errorf("flags %v: query %q, want %q", tc.flags, query, tc.want)
		}
	}
}
