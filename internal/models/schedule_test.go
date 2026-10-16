package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// The descriptors below are NDManager's own (_task_descriptor): the same shape
// on the org-wide list, the read by code, the per-schedule list and the specs
// nested in GET /schedules/{name}. firmware_track is on every one of them: the
// track of a FIRMWARE spec, null for every other kind.
const (
	firmwareSpecJSON = `{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","firmware_track":"fast","request":{},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
	backupSpecJSON = `{"code":"Bk123456","schedule_name":"night","kind":"BACKUP","firmware_track":null,"request":{"type":"BACKUP"},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
	// A control plane from before the field was added sends no key at all.
	specWithoutTheKeyJSON = `{"code":"Fw654321","schedule_name":"night","kind":"FIRMWARE","request":{},"enabled":true,` +
		`"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
)

func TestScheduledTaskCarriesTheTrackOfAFirmwareSpec(t *testing.T) {
	var firmware, backup, old ScheduledTask
	for raw, into := range map[string]*ScheduledTask{firmwareSpecJSON: &firmware, backupSpecJSON: &backup, specWithoutTheKeyJSON: &old} {
		if err := json.Unmarshal([]byte(raw), into); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}

	if firmware.FirmwareTrack == nil || *firmware.FirmwareTrack != "fast" || firmware.Track() != "fast" {
		t.Errorf("a FIRMWARE spec names its track: FirmwareTrack = %v, Track() = %q", firmware.FirmwareTrack, firmware.Track())
	}
	if backup.FirmwareTrack != nil || backup.Track() != "" {
		t.Errorf("a BACKUP spec has no track: FirmwareTrack = %v, Track() = %q", backup.FirmwareTrack, backup.Track())
	}
	if old.FirmwareTrack != nil || old.Track() != "" {
		t.Errorf("a descriptor without the key has no track: FirmwareTrack = %v, Track() = %q", old.FirmwareTrack, old.Track())
	}

	// The key is always present on what the CLI prints (-f json and the MCP
	// results are these structs, encoded again): the track for a FIRMWARE spec,
	// null otherwise, never an empty string and never missing.
	for name, tc := range map[string]struct {
		spec ScheduledTask
		want string
	}{
		"firmware":    {firmware, `"firmware_track":"fast"`},
		"backup":      {backup, `"firmware_track":null`},
		"without key": {old, `"firmware_track":null`},
	} {
		out, err := json.Marshal(tc.spec)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Errorf("%s: encoded %s, want it to carry %s", name, out, tc.want)
		}
	}
}

// A schedule's nested specs are the same descriptors.
func TestScheduleNestedSpecsCarryTheirTrack(t *testing.T) {
	raw := `{"organization_name":"acme","name":"night","enabled":true,"schedule":"0 2 * * 6","timezone":"UTC",` +
		`"scheduled_tasks":[` + firmwareSpecJSON + `,` + backupSpecJSON + `]}`
	var sch Schedule
	if err := json.Unmarshal([]byte(raw), &sch); err != nil {
		t.Fatal(err)
	}
	if len(sch.ScheduledTasks) != 2 {
		t.Fatalf("specs = %d, want 2", len(sch.ScheduledTasks))
	}
	if got := sch.ScheduledTasks[0].Track(); got != "fast" {
		t.Errorf("nested FIRMWARE spec track = %q, want fast", got)
	}
	if got := sch.ScheduledTasks[1].Track(); got != "" {
		t.Errorf("nested BACKUP spec track = %q, want none", got)
	}
}
