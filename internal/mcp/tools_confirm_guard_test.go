package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every tool whose name says it deletes, removes, renames, revokes or
// disables something takes confirm, as the README promises. A new one that
// does not fails here instead of acting on an agent's first call.
func TestDestructiveToolsTakeConfirm(t *testing.T) {
	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	destructive := map[string]bool{"delete": true, "remove": true, "rename": true, "revoke": true, "disable": true}
	checked := 0
	for _, tool := range result.Tools {
		words := strings.FieldsFunc(strings.TrimPrefix(tool.Name, "ndcli."), func(r rune) bool { return r == '.' || r == '_' })
		named := false
		for _, w := range words {
			named = named || destructive[w]
		}
		if !named {
			continue
		}
		checked++
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(raw, &schema)
		if _, ok := schema.Properties["confirm"]; !ok {
			t.Errorf("%s takes no confirm, so it acts on the first call", tool.Name)
		}
	}
	if checked < 30 {
		t.Errorf("checked only %d tools; the name match is broken", checked)
	}
}

// Detaching the org's backup from its schedule stops scheduled backups, so
// it previews unless confirmed; attaching still acts at once.
func TestBackupScheduleDetachPreviewsUnlessConfirmed(t *testing.T) {
	const detached = `{"detached": true, "organization_name": "acme"}`
	for _, args := range []string{`{}`, `{"schedule_name": null}`, `{"schedule_name": ""}`} {
		cs, seen := firmwareClient(t, http.StatusOK, detached)
		resp := callTool(t, cs, "ndcli.backup.config_set_schedule", args)
		data, _ := resp.Data.(map[string]interface{})
		if !resp.Success || data["preview"] != true || data["target"] != "acme" {
			t.Errorf("%s: %+v, want a preview of acme", args, resp)
		}
		if seen.hits != 0 {
			t.Errorf("%s: without confirm the detach was sent: %s %s", args, seen.method, seen.path)
		}
	}

	cs, seen := firmwareClient(t, http.StatusOK, detached)
	resp := callTool(t, cs, "ndcli.backup.config_set_schedule", `{"confirm": true}`)
	if !resp.Success || seen.method != http.MethodPut || seen.path != "/api/v1/organizations/acme/backup-config/schedule" {
		t.Errorf("confirmed detach: %+v after %s %s", resp, seen.method, seen.path)
	}

	cs, seen = firmwareClient(t, http.StatusOK, `{"code": "Bk123456", "kind": "BACKUP", "schedule_name": "night", "enabled": true}`)
	resp = callTool(t, cs, "ndcli.backup.config_set_schedule", `{"schedule_name": "night"}`)
	if data, _ := resp.Data.(map[string]interface{}); !resp.Success || data["preview"] == true || seen.hits != 1 {
		t.Errorf("attach without confirm: %+v after %d request(s), want it sent at once", resp, seen.hits)
	}
}
