package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const orgQuotaBody = `{"organization":"acme","plan":{"name":"team","display_name":"Team","price_per_device_cents":0},` +
	`"devices":{"limit":100,"used":12,"available":88,"unlimited":false},"users":{"limit":10,"used":3,"available":7,"unlimited":false},` +
	`"vpn_networks":{"limit":5,"used":1,"available":4,"unlimited":false},"snippets":{"limit":0,"used":20,"available":0,"unlimited":true},` +
	`"backup_enabled":true,"remoteadmin_enabled":false,"firmware_tracks_enabled":%s}`

// The commands are never hidden, so org quota is where a user learns whether
// the plan includes firmware tracks.
func TestOrgQuotaShowsWhetherThePlanIncludesFirmwareTracks(t *testing.T) {
	cmd := findCommand(t, "org", "quota")
	for _, tc := range []struct {
		name string
		flag string
		want string
	}{
		{"included", "true", "Enabled"},
		{"not included", "false", "Disabled"},
	} {
		body := fmt.Sprintf(orgQuotaBody, tc.flag)
		for _, format := range []string{"table", "simple", "detailed"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				seen := &firmwareSeen{}
				stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), format, "", func() error {
					return runOrgQuota(cmd, []string{"acme"})
				})
				if seen.path != "/api/v1/organizations/acme/quota" {
					t.Errorf("asked for %s", seen.path)
				}
				var line string
				for _, l := range strings.Split(stdout, "\n") {
					if strings.Contains(l, "Firmware Tracks") {
						line = strings.Join(strings.Fields(l), " ")
					}
				}
				if line == "" || !strings.HasSuffix(line, tc.want) {
					t.Errorf("want a Firmware Tracks line ending in %q, got %q in:\n%s", tc.want, line, stdout)
				}
			})
		}
		t.Run(tc.name+"/json", func(t *testing.T) {
			seen := &firmwareSeen{}
			stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), "json", "", func() error {
				return runOrgQuota(cmd, []string{"acme"})
			})
			var doc map[string]interface{}
			decodeOne(t, stdout, &doc)
			if doc["firmware_tracks_enabled"] != (tc.flag == "true") {
				t.Errorf("firmware_tracks_enabled = %v, want %s", doc["firmware_tracks_enabled"], tc.flag)
			}
		})
	}
}
