package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Task items as NDManager serves them: before 1.99 (no Z, none of the new
// fields), 1.99 (where a task came from, how its device stands) and 1.100 (who
// asked, from which client, the run, when it was sent and finished).
const (
	outTaskBefore199 = `{"task": "Ab12Cd34", "device_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"device_name": "fw-branch-01", "organization": "acme-corp", "type": "PING", "status": "COMPLETED",
		"message": "pong", "created_at": "2026-10-18T03:00:04", "updated_at": "2026-10-18T03:00:09",
		"expires_at": "2026-10-18T03:05:04", "scheduled_at": null}`

	outTaskScheduled199 = `{"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"device_name": "fw-branch-01", "device_deleted": false, "organization": "acme-corp", "type": "BACKUP",
		"status": "PENDING", "message": null, "created_at": "2026-10-18T03:00:04Z",
		"updated_at": "2026-10-18T03:00:04Z", "expires_at": "2026-10-19T03:00:04Z", "scheduled_at": null,
		"origin": "SCHEDULE",
		"schedule": {"name": "nightly-backup", "spec_code": "Bk7Qm2Zp", "kind": "BACKUP",
			"next_run_at": "2026-10-19T03:00:00Z", "firmware_track": null},
		"device_online": false, "device_last_seen_at": "2026-10-17T22:41:10Z", "last_response_at": null}`

	outTaskManual199 = `{"task": "Mn4Op5Qr", "device_uuid": "7d0c1f9e-3b2a-4c5d-8e6f-0a1b2c3d4e5f",
		"device_name": "e2e-a", "device_deleted": false, "organization": "acme-corp", "type": "REBOOT",
		"status": "SCHEDULED", "message": null, "created_at": "2026-10-18T09:00:00Z",
		"updated_at": "2026-10-18T09:00:00Z", "expires_at": "2026-10-19T09:30:00Z",
		"scheduled_at": "2026-10-19T06:00:00Z", "origin": "MANUAL", "schedule": null,
		"device_online": true, "device_last_seen_at": "2026-10-18T13:59:50Z", "last_response_at": null}`

	outTaskManual1100 = `{"task": "Pq3Lm8Xa", "device_uuid": "7d0c1f9e-3b2a-4c5d-8e6f-0a1b2c3d4e5f",
		"device_name": "e2e-a", "device_deleted": false, "organization": "acme-corp", "type": "PING",
		"status": "COMPLETED", "message": "pong", "created_at": "2026-10-18T14:02:11Z",
		"updated_at": "2026-10-18T14:04:17Z", "expires_at": "2026-10-18T14:07:11Z", "scheduled_at": null,
		"origin": "MANUAL", "schedule": null, "device_online": null, "device_last_seen_at": null,
		"last_response_at": "2026-10-18T14:04:17Z",
		"requested_by": {"email": "ana@acme.com", "name": "Ana Lima"}, "client": "CLI", "run_id": "R7x2Kp9Q",
		"dispatched_at": "2026-10-18T14:02:12Z", "completed_at": "2026-10-18T14:04:17Z"}`

	outTaskWindow1100 = `{"task": "Fw9Tr4Ck", "device_uuid": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
		"device_name": "e2e-b", "device_deleted": false, "organization": "acme-corp", "type": "FIRMWARE_TRACK",
		"status": "IN_PROGRESS", "message": "Firmware track fast: advance", "created_at": "2026-10-18T02:00:01Z",
		"updated_at": "2026-10-18T02:00:03Z", "expires_at": "2026-10-18T04:00:01Z", "scheduled_at": null,
		"origin": "FIRMWARE",
		"schedule": {"name": "fw-window", "spec_code": "Wn5Dw6Sp", "kind": "FIRMWARE",
			"next_run_at": null, "firmware_track": "fast"},
		"device_online": null, "device_last_seen_at": null, "last_response_at": null,
		"requested_by": null, "client": null, "run_id": "Rn8Wd2Fx",
		"dispatched_at": "2026-10-18T02:00:03Z", "completed_at": null}`

	outTaskEnroll1100 = `{"task": "En7Rl1Mt", "device_uuid": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
		"device_name": "e2e-b", "device_deleted": false, "organization": "acme-corp", "type": "FIRMWARE_TRACK",
		"status": "EXPIRED", "message": "Task expired before delivery", "created_at": "2026-10-17T01:00:00Z",
		"updated_at": "2026-10-17T03:01:00Z", "expires_at": "2026-10-17T03:00:00Z", "scheduled_at": null,
		"origin": "FIRMWARE", "schedule": null, "device_online": null, "device_last_seen_at": null,
		"last_response_at": null, "requested_by": null, "client": null, "run_id": "Rc3Np4Ss",
		"dispatched_at": null, "completed_at": "2026-10-17T03:01:00Z"}`

	outTaskDeleted1100 = `{"task": "Dl2Gn6Hk", "device_uuid": "1f2e3d4c-5b6a-4978-8695-a4b3c2d1e0f9",
		"device_name": "fw-old", "device_deleted": true, "organization": "acme-corp", "type": "SYNC",
		"status": "FAILED", "message": "the device was removed", "created_at": "2026-10-16T10:00:00Z",
		"updated_at": "2026-10-16T10:00:41Z", "expires_at": "2026-10-16T11:00:00Z", "scheduled_at": null,
		"origin": "MANUAL", "schedule": null, "device_online": null, "device_last_seen_at": null,
		"last_response_at": null, "requested_by": {"email": null, "name": null}, "client": "MCP",
		"run_id": "Rd5Lt7Xq", "dispatched_at": "2026-10-16T10:00:02Z", "completed_at": "2026-10-16T10:00:41Z"}`
)

func decodeOutTask(t *testing.T, body string) *models.Task {
	t.Helper()
	var task models.Task
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return &task
}

func outTasks(t *testing.T, bodies ...string) []models.Task {
	t.Helper()
	tasks := make([]models.Task, len(bodies))
	for i, body := range bodies {
		tasks[i] = *decodeOutTask(t, body)
	}
	return tasks
}

// inDisplayTimezone renders times in name for the rest of the test.
func inDisplayTimezone(t *testing.T, name string) {
	t.Helper()
	prev := displayLocation
	if err := SetTimezone(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { displayLocation = prev })
}

func tableHeaderLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Status") {
			return line
		}
	}
	return ""
}

func TestTaskTableShowsSourceAndDuration(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	tasks := outTasks(t, outTaskManual1100, outTaskScheduled199, outTaskWindow1100, outTaskEnroll1100, outTaskDeleted1100)
	out := render(t, "table", "FormatTasks", tasks, len(tasks))

	header := tableHeaderLine(out)
	for _, want := range []string{"ID", "Type", "Status", "Device", "Source", "Created", "Duration"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	if strings.Index(header, "Source") > strings.Index(header, "Created") ||
		strings.Index(header, "Created") > strings.Index(header, "Duration") {
		t.Errorf("columns out of order: %q", header)
	}
	mustContain(t, "table", out,
		"ana@acme.com · CLI",      // who asked, from where
		"Schedule nightly-backup", // a schedule's fire
		"fast track",              // a track's window step
		"Firmware track",          // a reconciler step: no schedule, no track
		"Deleted account · MCP",   // the requester's account is gone
		"fw-old (deleted)",        // the device is gone
		"fw-branch-01 (offline)",  // a waiting task's device is not connected
		"2m 05s", "39s",           // dispatched to completed
	)
	// The waiting row with a connected device is not marked.
	mustNotContain(t, "table", out, "(online)")
}

func TestTaskTableFromAnOlderNDManagerKeepsItsColumns(t *testing.T) {
	out := render(t, "table", "FormatTasks", outTasks(t, outTaskBefore199), 1)
	header := tableHeaderLine(out)
	for _, not := range []string{"Source", "Duration"} {
		if strings.Contains(header, not) {
			t.Errorf("a task list without the new fields grew a %s column: %q", not, header)
		}
	}
	mustContain(t, "table", out, "Ab12Cd34", "fw-branch-01")

	// 1.99 says where tasks came from but not how long they took.
	header = tableHeaderLine(render(t, "table", "FormatTasks", outTasks(t, outTaskScheduled199, outTaskManual199), 2))
	if !strings.Contains(header, "Source") || strings.Contains(header, "Duration") {
		t.Errorf("1.99 header = %q, want Source and no Duration", header)
	}
}

func TestTaskSimpleListAddsSourceAndDuration(t *testing.T) {
	out := render(t, "simple", "FormatTasks", outTasks(t, outTaskManual1100, outTaskBefore199), 2)
	mustContain(t, "simple", out, "• Pq3Lm8Xa - PING [COMPLETED] · ana@acme.com · CLI · took 2m 05s")
	mustContain(t, "simple", out, "• Ab12Cd34 - PING [COMPLETED]\n")
}

func TestTaskDescribeShowsProvenanceAndTimes(t *testing.T) {
	// The times are UTC on the wire and print in the display zone, as before.
	inDisplayTimezone(t, "America/Sao_Paulo")
	manual := decodeOutTask(t, outTaskManual1100)
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatTask", manual)
			for _, line := range [][2]string{
				{"Source", "Manual"},
				{"Requested by", "Ana Lima <ana@acme.com>"},
				{"Client", "CLI"},
				{"Run", "R7x2Kp9Q"},
				{"Created", "2026-10-18 11:02:11"},
				{"Dispatched", "2026-10-18 11:02:12"},
				{"Completed", "2026-10-18 11:04:17"},
				{"Duration", "2m 05s"},
				{"Responded", "2026-10-18 11:04:17"},
			} {
				if !hasField(out, line[0], line[1]) {
					t.Errorf("%s: no %q line with %q:\n%s", format, line[0], line[1], out)
				}
			}
			mustNotContain(t, format, out, "Started", "who started it is not recorded", "Spec")
		})
	}
}

func TestTaskDescribeShowsAWaitingTasksScheduleAndDevice(t *testing.T) {
	inDisplayTimezone(t, "America/Sao_Paulo")
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatTask", decodeOutTask(t, outTaskScheduled199))
			for _, line := range [][2]string{
				{"Device", "fw-branch-01 (offline, last seen 2026-10-17 19:41:10)"},
				{"Source", "Schedule"},
				{"Schedule", "nightly-backup"},
				{"Spec", "Bk7Qm2Zp (BACKUP)"},
				{"Next run", "2026-10-19 00:00:00"},
			} {
				if !hasField(out, line[0], line[1]) {
					t.Errorf("%s: no %q line with %q:\n%s", format, line[0], line[1], out)
				}
			}
			mustNotContain(t, format, out, "Requested by", "Client", "Dispatched", "Duration")

			out = render(t, format, "FormatTask", decodeOutTask(t, outTaskManual199))
			if !hasField(out, "Device", "e2e-a (online)") || !hasField(out, "Source", "Manual (who started it is not recorded)") {
				t.Errorf("%s: a 1.99 manual task:\n%s", format, out)
			}

			out = render(t, format, "FormatTask", decodeOutTask(t, outTaskWindow1100))
			if !hasField(out, "Source", "Firmware track fast") || !hasField(out, "Next run", "disabled") ||
				!hasField(out, "Spec", "Wn5Dw6Sp (FIRMWARE)") {
				t.Errorf("%s: a track's window step:\n%s", format, out)
			}

			out = render(t, format, "FormatTask", decodeOutTask(t, outTaskDeleted1100))
			if !hasField(out, "Device", "fw-old (deleted)") || !hasField(out, "Requested by", "Deleted account") ||
				!hasField(out, "Duration", "39s") {
				t.Errorf("%s: a deleted device and account:\n%s", format, out)
			}
		})
	}
}

func TestTaskDescribeFromAnOlderNDManagerIsUnchanged(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		out := render(t, format, "FormatTask", decodeOutTask(t, outTaskBefore199))
		mustNotContain(t, format, out, "Source", "Requested by", "Client", "Run ", "Dispatched", "Duration",
			"Responded", "(offline", "(online", "(deleted")
		mustContain(t, format, out, "fw-branch-01", "acme-corp")
	}
}

func TestTaskDetailedListShowsProvenance(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	out := render(t, "detailed", "FormatTasks", outTasks(t, outTaskManual1100, outTaskScheduled199), 2)
	for _, line := range [][2]string{
		{"Requested by", "Ana Lima <ana@acme.com>"},
		{"Run", "R7x2Kp9Q"},
		{"Dispatched", "2026-10-18 14:02:12"},
		{"Duration", "2m 05s"},
		{"Schedule", "nightly-backup"},
		{"Device", "fw-branch-01 (offline, last seen 2026-10-17 22:41:10)"},
	} {
		if !hasField(out, line[0], line[1]) {
			t.Errorf("no %q line with %q:\n%s", line[0], line[1], out)
		}
	}
}

func TestTaskJSONCarriesTheNewFieldsAndTheDuration(t *testing.T) {
	type jsonDoc map[string]any
	decode := func(t *testing.T, out string) jsonDoc {
		t.Helper()
		var doc jsonDoc
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("not one JSON document: %v\n%s", err, out)
		}
		return doc
	}

	doc := decode(t, render(t, "json", "FormatTask", decodeOutTask(t, outTaskManual1100)))
	for key, want := range map[string]any{
		"origin": "MANUAL", "client": "CLI", "run_id": "R7x2Kp9Q",
		"dispatched_at": "2026-10-18T14:02:12Z", "completed_at": "2026-10-18T14:04:17Z",
		"started_at": "2026-10-18T14:02:12Z", "duration_seconds": float64(125),
		"last_response_at": "2026-10-18T14:04:17Z", "device_deleted": false,
		"created_at": "2026-10-18T14:02:11Z",
	} {
		if doc[key] != want {
			t.Errorf("%s = %#v, want %#v", key, doc[key], want)
		}
	}
	if r, _ := doc["requested_by"].(map[string]any); r["email"] != "ana@acme.com" || r["name"] != "Ana Lima" {
		t.Errorf("requested_by = %#v", doc["requested_by"])
	}

	// A 1.99 task: the 1.100 keys are there, null.
	doc = decode(t, render(t, "json", "FormatTask", decodeOutTask(t, outTaskScheduled199)))
	for _, key := range []string{"requested_by", "client", "run_id", "dispatched_at", "completed_at", "started_at",
		"duration_seconds", "last_response_at"} {
		if v, ok := doc[key]; !ok || v != nil {
			t.Errorf("%s = %#v (present %v), want null", key, v, ok)
		}
	}
	if s, _ := doc["schedule"].(map[string]any); s["name"] != "nightly-backup" || s["spec_code"] != "Bk7Qm2Zp" {
		t.Errorf("schedule = %#v", doc["schedule"])
	}

	// An older task: started_at stays the null it always was, and nothing the
	// server did not send is invented.
	doc = decode(t, render(t, "json", "FormatTask", decodeOutTask(t, outTaskBefore199)))
	if v, ok := doc["started_at"]; !ok || v != nil {
		t.Errorf("started_at = %#v (present %v), want null", v, ok)
	}
	if _, ok := doc["origin"]; ok {
		t.Errorf("origin is not sent by an older NDManager: %#v", doc["origin"])
	}

	list := decode(t, render(t, "json", "FormatTasks", outTasks(t, outTaskManual1100, outTaskEnroll1100), 2))
	rows, _ := list["tasks"].([]any)
	if len(rows) != 2 || list["total"] != float64(2) {
		t.Fatalf("tasks = %#v", list)
	}
	if first := rows[0].(map[string]any); first["duration_seconds"] != float64(125) {
		t.Errorf("first row duration_seconds = %#v", first["duration_seconds"])
	}
	if second := rows[1].(map[string]any); second["duration_seconds"] != nil || second["completed_at"] != "2026-10-17T03:01:00Z" {
		t.Errorf("a task never sent has a completion and no duration: %#v", second)
	}
}

func TestFormatTaskDuration(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{0, "0s"}, {3, "3s"}, {59, "59s"}, {60, "1m 00s"}, {125, "2m 05s"}, {3599, "59m 59s"},
		{3600, "1h 00m"}, {3720, "1h 02m"}, {86399, "23h 59m"}, {86400, "1d 00h"}, {3*86400 + 4*3600, "3d 04h"},
	} {
		d := time.Duration(tc.seconds) * time.Second
		if got := formatTaskDuration(d); got != tc.want {
			t.Errorf("formatTaskDuration(%ds) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

// hasField reports whether out has a line naming label and then value, in any
// of the three human layouts: "Label:   value", "  Label: value" or
// "  Label       value".
func hasField(out, label, value string) bool {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, label) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, label)
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
		if rest == value {
			return true
		}
	}
	return false
}

// TestTaskRequesterWithANameAndNoEmail: NDManager sends both null for a deleted
// account. A name without an email is not that, so it is shown, not dropped.
func TestTaskRequesterWithANameAndNoEmail(t *testing.T) {
	task := decodeOutTask(t, strings.Replace(outTaskManual1100,
		`"requested_by": {"email": "ana@acme.com", "name": "Ana Lima"}`, `"requested_by": {"email": null, "name": "Ana Lima"}`, 1))
	if got := taskSourceCell(task); got != "Ana Lima · CLI" {
		t.Errorf("source cell = %q, want %q", got, "Ana Lima · CLI")
	}
	for _, format := range []string{"table", "simple", "detailed"} {
		if out := render(t, format, "FormatTask", task); !hasField(out, "Requested by", "Ana Lima") {
			t.Errorf("%s: no Requested by line with the name:\n%s", format, out)
		}
	}
}
