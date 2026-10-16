package mcp

import (
	"net/http"
	"strings"
	"testing"
)

// NDManager's spec descriptors, as the schedule tools return them to an agent:
// firmware_track is on every one, the track of a FIRMWARE spec and null otherwise.
const (
	mcpFirmwareSpec = `{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","firmware_track":"fast","request":{},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
	mcpBackupSpec = `{"code":"Bk123456","schedule_name":"night","kind":"BACKUP","firmware_track":null,"request":{"type":"BACKUP"},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
)

func assertSpecTracks(t *testing.T, specs []interface{}) {
	t.Helper()
	if len(specs) != 2 {
		t.Fatalf("specs = %v, want 2", specs)
	}
	firmware, backup := specs[0].(map[string]interface{}), specs[1].(map[string]interface{})
	if firmware["firmware_track"] != "fast" {
		t.Errorf("FIRMWARE spec firmware_track = %v, want fast", firmware["firmware_track"])
	}
	if track, present := backup["firmware_track"]; !present || track != nil {
		t.Errorf("BACKUP spec firmware_track = %v (present: %v), want an explicit null", track, present)
	}
}

func TestScheduleToolsReturnTheTrackOfAFirmwareSpec(t *testing.T) {
	t.Run("ndcli.schedule.tasks.list", func(t *testing.T) {
		for _, args := range []string{`{}`, `{"schedule":"night"}`} {
			cs, seen := firmwareClient(t, http.StatusOK, `{"items":[`+mcpFirmwareSpec+`,`+mcpBackupSpec+`],"total":2}`)
			resp := callTool(t, cs, "ndcli.schedule.tasks.list", args)
			if !resp.Success {
				t.Fatalf("%s: %+v", args, resp.Error)
			}
			if seen.path != "/api/v1/organizations/acme/scheduled-tasks" {
				t.Errorf("%s: sent %s", args, seen.path)
			}
			assertSpecTracks(t, resp.Data.(map[string]interface{})["tasks"].([]interface{}))
		}
	})
	t.Run("ndcli.schedule.describe", func(t *testing.T) {
		body := `{"organization_name":"acme","name":"night","enabled":true,"schedule":"0 2 * * 6","timezone":"UTC",` +
			`"scheduled_tasks":[` + mcpFirmwareSpec + `,` + mcpBackupSpec + `]}`
		cs, _ := firmwareClient(t, http.StatusOK, body)
		resp := callTool(t, cs, "ndcli.schedule.describe", `{"name":"night"}`)
		if !resp.Success {
			t.Fatalf("%+v", resp.Error)
		}
		assertSpecTracks(t, resp.Data.(map[string]interface{})["scheduled_tasks"].([]interface{}))
	})
}

// An agent reads firmware_track without guessing: the tools that return specs say
// what it is.
func TestScheduleSpecToolDescriptionsExplainTheTrack(t *testing.T) {
	tools := toolDefinitions(t)
	for _, name := range []string{"ndcli.schedule.describe", "ndcli.schedule.tasks.list"} {
		d := tools[name].Description
		for _, want := range []string{"FIRMWARE", "firmware_track", "fast, regular or stable", "null for every other kind"} {
			if !strings.Contains(d, want) {
				t.Errorf("%s: description is missing %q: %s", name, want, d)
			}
		}
	}
}
