package mcp

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const mcpQuotaBody = `{"organization":"acme","plan":{"name":"team","display_name":"Team","price_per_device_cents":0},` +
	`"devices":{"limit":100,"used":12,"available":88,"unlimited":false},"users":{"limit":10,"used":3,"available":7,"unlimited":false},` +
	`"vpn_networks":{"limit":5,"used":1,"available":4,"unlimited":false},"snippets":{"limit":0,"used":20,"available":0,"unlimited":true},` +
	`"backup_enabled":true,"remoteadmin_enabled":false,"firmware_tracks_enabled":%s}`

// The firmware tools are listed for every plan and are refused on one without
// the feature, so the quota says up front whether the plan has it, and what the
// refusal covers: every tool, reads included, except for an organization that
// already has a configuration, which can still be wound down.
func TestOrgQuotaToolReportsTheFirmwareTracksFlag(t *testing.T) {
	d := toolDefinitions(t)["ndcli.org.quota"].Description
	for _, want := range []string{
		"backup_enabled", "remoteadmin_enabled", "firmware_tracks_enabled",
		"Check firmware_tracks_enabled before using the ndcli.firmware.* tools",
		"on a plan without it they answer FEATURE_NOT_AVAILABLE, reads included,",
		"unless the organization already has a firmware configuration, whose reads keep answering and which can still be wound down",
		"(see ndcli.firmware.config_show)",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("ndcli.org.quota description is missing %q: %s", want, d)
		}
	}
	// A plan without the feature does not answer every tool with the refusal: an
	// organization that has a configuration keeps its reads.
	if strings.Contains(d, "answers the ndcli.firmware.* tools with FEATURE_NOT_AVAILABLE") {
		t.Errorf("ndcli.org.quota still says a plan without firmware tracks refuses every tool: %s", d)
	}

	for _, flag := range []string{"true", "false"} {
		t.Run(flag, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, fmt.Sprintf(mcpQuotaBody, flag))
			resp := callTool(t, cs, "ndcli.org.quota", `{"name":"acme"}`)
			if !resp.Success {
				t.Fatalf("failed: %+v", resp.Error)
			}
			if seen.path != "/api/v1/organizations/acme/quota" {
				t.Errorf("asked for %s", seen.path)
			}
			quota, _ := resp.Data.(map[string]interface{})["quota"].(map[string]interface{})
			for key, want := range map[string]bool{"backup_enabled": true, "remoteadmin_enabled": false, "firmware_tracks_enabled": flag == "true"} {
				if got, ok := quota[key]; !ok || got != want {
					t.Errorf("quota.%s = %v (present: %v), want %v", key, got, ok, want)
				}
			}
		})
	}
}
