package cli

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestPrefixAddWithoutValueGoesThroughTheFormatter: without --value, prefix
// add printed a green line straight to stdout and called no formatter, so -f
// json printed nothing at all on success.
func TestPrefixAddWithoutValueGoesThroughTheFormatter(t *testing.T) {
	const route = "/api/v1/organizations/acme/vpn-networks/site/members/fw-a/prefixes"
	server := func(publish bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != route {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"vpn_network":"site","device_name":"fw-a","variable_name":"LAN_PREFIX","publish":%t,"created_at":"2026-10-22T12:00:00Z"}`, publish)
		}
	}
	cmd := findCommand(t, "network", "prefix", "add")
	add := func() error { return runNetworkPrefixAdd(cmd, []string{"site", "fw-a", "LAN_PREFIX"}) }

	for _, publish := range []bool{true, false} {
		stdout, _ := runStreams(t, server(publish), "json", "", add)
		var got map[string]any
		decodeOne(t, stdout, &got)
		want := map[string]any{"network": "site", "device": "fw-a", "variable": "LAN_PREFIX", "prefix_created": true, "published": publish}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("json = %v, want %v", got, want)
		}
	}

	for _, format := range []string{"table", "simple", "detailed"} {
		stdout, _ := runStreams(t, server(false), format, "", add)
		if !strings.Contains(stdout, "Prefix added: LAN_PREFIX on fw-a in site") ||
			!strings.Contains(stdout, "publish=false — the prefix exists but is not advertised to peers.") {
			t.Errorf("%s output:\n%s", format, stdout)
		}
	}
}
