package mcp

import (
	"net/http"
	"testing"
)

// Disabling a schedule or a spec stops scheduled work, so both preview
// unless confirmed, like every other disable tool (#315).
func TestScheduleDisableToolsPreviewUnlessConfirmed(t *testing.T) {
	for _, tc := range []struct {
		tool, args, target, body string
	}{
		{"ndcli.schedule.disable", `{"name":"night"}`, "night",
			`{"organization_name":"acme","name":"night","enabled":false,"schedule":"0 2 * * 6","timezone":"UTC"}`},
		{"ndcli.schedule.tasks.disable", `{"code":"Bk123456"}`, "Bk123456", mcpBackupSpec},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.body)
			resp := callTool(t, cs, tc.tool, tc.args)
			data, _ := resp.Data.(map[string]interface{})
			if !resp.Success || data["preview"] != true || data["target"] != tc.target {
				t.Errorf("without confirm: %+v, want a preview of %s", resp, tc.target)
			}
			if seen.hits != 0 {
				t.Errorf("without confirm the tool sent %s %s", seen.method, seen.path)
			}

			confirmed := tc.args[:len(tc.args)-1] + `,"confirm":true}`
			resp = callTool(t, cs, tc.tool, confirmed)
			if !resp.Success || seen.hits != 1 {
				t.Errorf("with confirm: %+v after %d request(s), want the disable sent once", resp, seen.hits)
			}
			if data, _ := resp.Data.(map[string]interface{}); data["preview"] == true {
				t.Errorf("with confirm the tool still previewed: %+v", resp)
			}
		})
	}
}
