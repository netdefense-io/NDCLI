package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// NDManager names the firmware track on every schedule spec descriptor: the
// org-wide list, the read by code, the per-schedule list (the CLI reaches it as
// the list's ?schedule= filter) and the specs nested in GET /schedules/{name}.
// These are its descriptors as _task_descriptor builds them.
const (
	trackedFirmwareSpec = `{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","firmware_track":"fast","request":{},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
	trackedBackupSpec = `{"code":"Bk123456","schedule_name":"night","kind":"BACKUP","firmware_track":null,"request":{"type":"BACKUP"},` +
		`"enabled":true,"created_by":"admin@acme.com","last_fired_at":null,` +
		`"created_at":"2026-10-08T12:00:00+00:00","updated_at":"2026-10-08T12:00:00+00:00"}`
)

func TestScheduleSpecDescriptorsCarryTheirTrack(t *testing.T) {
	t.Run("the org-wide list and its per-schedule filter", func(t *testing.T) {
		for _, filter := range []string{"", "night"} {
			srv, seen := firmwareServer(t, http.StatusOK, `{"items":[`+trackedFirmwareSpec+`,`+trackedBackupSpec+`],"total":2}`)
			tasks, err := newTestService(t, srv).ScheduleTaskList(context.Background(), "acme", filter)
			if err != nil {
				t.Fatal(err)
			}
			if seen.path != "/api/v1/organizations/acme/scheduled-tasks" || seen.query.Get("schedule") != filter {
				t.Errorf("filter %q: sent %s ?%s", filter, seen.path, seen.query.Encode())
			}
			if len(tasks) != 2 {
				t.Fatalf("filter %q: %d specs, want 2", filter, len(tasks))
			}
			if tasks[0].Track() != "fast" || tasks[1].Track() != "" {
				t.Errorf("filter %q: tracks = %q, %q, want fast and none", filter, tasks[0].Track(), tasks[1].Track())
			}
			if tasks[1].FirmwareTrack != nil {
				t.Errorf("a BACKUP spec's null track must stay nil, got %q", *tasks[1].FirmwareTrack)
			}
		}
	})

	t.Run("the read by code", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, trackedFirmwareSpec)
		spec, err := newTestService(t, srv).ScheduledTaskGet(context.Background(), "acme", "Fw123456")
		if err != nil {
			t.Fatal(err)
		}
		if seen.path != "/api/v1/organizations/acme/scheduled-tasks/Fw123456" {
			t.Errorf("sent %s", seen.path)
		}
		if spec.Kind != "FIRMWARE" || spec.Track() != "fast" {
			t.Errorf("spec = kind %q, track %q, want FIRMWARE and fast", spec.Kind, spec.Track())
		}
	})

	t.Run("the specs nested in a schedule", func(t *testing.T) {
		body := `{"organization_name":"acme","name":"night","enabled":true,"schedule":"0 2 * * 6","timezone":"UTC",` +
			`"scheduled_tasks":[` + trackedFirmwareSpec + `,` + trackedBackupSpec + `]}`
		srv, _ := firmwareServer(t, http.StatusOK, body)
		sch, err := newTestService(t, srv).ScheduleGet(context.Background(), "acme", "night")
		if err != nil {
			t.Fatal(err)
		}
		if len(sch.ScheduledTasks) != 2 || sch.ScheduledTasks[0].Track() != "fast" || sch.ScheduledTasks[1].Track() != "" {
			t.Errorf("nested specs = %+v", sch.ScheduledTasks)
		}
	})
}

// The track is control-plane text that reaches a terminal, so it goes through
// the same scrub as every other string of a response: the decode boundary.
func TestScheduleSpecTrackIsScrubbedAtTheDecodeBoundary(t *testing.T) {
	hostile := "fa\x1b[31m\x1b]0;pwn\x07st\u009b2J"
	encoded, err := json.Marshal(hostile)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(trackedFirmwareSpec, `"firmware_track":"fast"`, `"firmware_track":`+string(encoded), 1)
	if !strings.Contains(body, `\u001b`) {
		t.Fatalf("the hostile track did not reach the body: %s", body)
	}

	srv, _ := firmwareServer(t, http.StatusOK, `{"items":[`+body+`],"total":1}`)
	tasks, err := newTestService(t, srv).ScheduleTaskList(context.Background(), "acme", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := tasks[0].Track(); got != "fa[31m]0;pwnst2J" {
		t.Errorf("track = %q, want the control characters stripped", got)
	}
	if hasControl(tasks[0].Track()) {
		t.Errorf("a control character survived in %q", tasks[0].Track())
	}
}
