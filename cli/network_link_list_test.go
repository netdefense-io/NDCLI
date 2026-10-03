package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// linkListServer answers the three reads behind `network link list`: a hub
// with two spokes, one of the pairs overridden by an explicit link. With
// empty set, the network has no members.
func linkListServer(t *testing.T, empty bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/organizations/acme/vpn-networks/corp":
			_, _ = io.WriteString(w, `{"name":"corp","auto_connect_hubs":false}`)
		case "/api/v1/organizations/acme/vpn-networks/corp/members":
			if empty {
				_, _ = io.WriteString(w, `{"items":[],"total":0}`)
				return
			}
			_, _ = io.WriteString(w, `{"items":[{"device_name":"hub1","role":"HUB"},`+
				`{"device_name":"fw01","role":"SPOKE"},{"device_name":"fw02","role":"SPOKE"}],"total":3}`)
		case "/api/v1/organizations/acme/vpn-networks/corp/links":
			if empty {
				_, _ = io.WriteString(w, `{"items":[],"total":0}`)
				return
			}
			_, _ = io.WriteString(w, `{"items":[{"device_a_name":"hub1","device_b_name":"fw01","enabled":true,"has_psk":true}],"total":1}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// TestNetworkLinkListJSONIsOneDocument: the default (effective) view printed
// a blank line and a summary such as "2 connections (2 automatic, 1
// override)" after the formatter whatever the format, so -f json was a
// document followed by prose.
func TestNetworkLinkListJSONIsOneDocument(t *testing.T) {
	cmd := findCommand(t, "network", "link", "list")
	for name, empty := range map[string]bool{"connections": false, "none": true} {
		t.Run(name, func(t *testing.T) {
			stdout, _ := runStreams(t, linkListServer(t, empty), "json", "", func() error {
				return runNetworkLinkList(cmd, []string{"corp"})
			})
			var got struct {
				Connections []models.EffectiveConnection `json:"connections"`
				Total       int                          `json:"total"`
			}
			decodeOne(t, stdout, &got)
			want := 2
			if empty {
				want = 0
			}
			if got.Total != want || len(got.Connections) != want {
				t.Errorf("decoded %+v, want %d connections", got, want)
			}
		})
	}
}

// TestNetworkLinkListHumanSummaryIsUnchanged: the human formats still end
// with the summary, as they did before it moved into the formatters.
func TestNetworkLinkListHumanSummaryIsUnchanged(t *testing.T) {
	cmd := findCommand(t, "network", "link", "list")
	for _, format := range []string{"table", "simple", "detailed"} {
		for name, tc := range map[string]struct {
			empty bool
			want  string
		}{
			"connections": {false, "\n2 connections (2 automatic, 1 override)\n"},
			"none":        {true, "ℹ No VPN connections found\n\n0 connections ()\n"},
		} {
			t.Run(format+"/"+name, func(t *testing.T) {
				stdout, _ := runStreams(t, linkListServer(t, tc.empty), format, "", func() error {
					return runNetworkLinkList(cmd, []string{"corp"})
				})
				if !strings.HasSuffix(stdout, tc.want) {
					t.Errorf("stdout should end with %q, got:\n%s", tc.want, stdout)
				}
			})
		}
	}
}
