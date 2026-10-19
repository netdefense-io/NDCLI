package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Task items as NDManager serves them. The 1.99 one is the example item of its
// API contract (GET /api/v1/tasks); the 1.100 one adds the provenance fields
// with the values its contract documents.
const (
	taskItemBefore199 = `{"task": "Ab12Cd34", "device_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"device_name": "fw-branch-01", "organization": "acme-corp", "type": "PING", "status": "COMPLETED",
		"message": "pong", "created_at": "2026-10-18T03:00:04", "updated_at": "2026-10-18T03:00:09",
		"expires_at": "2026-10-18T03:05:04", "scheduled_at": null}`

	taskItem199 = `{"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"device_name": "fw-branch-01", "device_deleted": false, "organization": "acme-corp", "type": "BACKUP",
		"status": "PENDING", "message": null, "created_at": "2026-10-18T03:00:04Z",
		"updated_at": "2026-10-18T03:00:04Z", "expires_at": "2026-10-19T03:00:04Z", "scheduled_at": null,
		"origin": "SCHEDULE",
		"schedule": {"name": "nightly-backup", "spec_code": "Bk7Qm2Zp", "kind": "BACKUP",
			"next_run_at": "2026-10-19T03:00:00Z", "firmware_track": null},
		"device_online": false, "device_last_seen_at": "2026-10-17T22:41:10Z", "last_response_at": null}`

	taskItem1100 = `{"task": "Pq3Lm8Xa", "device_uuid": "7d0c1f9e-3b2a-4c5d-8e6f-0a1b2c3d4e5f",
		"device_name": "e2e-a", "device_deleted": false, "organization": "acme-corp", "type": "PING",
		"status": "COMPLETED", "message": "pong", "created_at": "2026-10-18T14:02:11Z",
		"updated_at": "2026-10-18T14:02:14Z", "expires_at": "2026-10-18T14:07:11Z", "scheduled_at": null,
		"origin": "MANUAL", "schedule": null, "device_online": null, "device_last_seen_at": null,
		"last_response_at": "2026-10-18T14:02:14Z",
		"requested_by": {"email": "ana@acme.com", "name": "Ana Lima"}, "client": "CLI", "run_id": "R7x2Kp9Q",
		"dispatched_at": "2026-10-18T14:02:12Z", "completed_at": "2026-10-18T14:04:17Z"}`
)

func decodeTask(t *testing.T, body string) Task {
	t.Helper()
	var task Task
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return task
}

func utc(s string) time.Time {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return at
}

func TestTaskDecodesWhatNDManager199Adds(t *testing.T) {
	task := decodeTask(t, taskItem199)
	if task.Origin != TaskOriginSchedule || task.DeviceDeleted {
		t.Errorf("origin = %q, device_deleted = %v", task.Origin, task.DeviceDeleted)
	}
	s := task.Schedule
	if s == nil || s.Name != "nightly-backup" || s.Kind != "BACKUP" || s.SpecCode == nil || *s.SpecCode != "Bk7Qm2Zp" ||
		s.FirmwareTrack != nil || s.NextRunAt == nil || !s.NextRunAt.Equal(utc("2026-10-19T03:00:00Z")) {
		t.Errorf("schedule = %+v", s)
	}
	if task.DeviceOnline == nil || *task.DeviceOnline {
		t.Errorf("device_online = %v, want false", task.DeviceOnline)
	}
	if task.DeviceLastSeenAt == nil || !task.DeviceLastSeenAt.Equal(utc("2026-10-17T22:41:10Z")) {
		t.Errorf("device_last_seen_at = %v", task.DeviceLastSeenAt)
	}
	if task.LastResponseAt != nil || task.RequestedBy != nil || task.Client != nil || task.RunID != nil ||
		task.DispatchedAt != nil || task.CompletedAt != nil {
		t.Errorf("a 1.99 item has none of the 1.100 fields: %+v", task)
	}
}

func TestTaskDecodesWhatNDManager1100Adds(t *testing.T) {
	task := decodeTask(t, taskItem1100)
	r := task.RequestedBy
	if r == nil || r.Email == nil || *r.Email != "ana@acme.com" || r.Name == nil || *r.Name != "Ana Lima" {
		t.Errorf("requested_by = %+v", r)
	}
	if task.Client == nil || *task.Client != "CLI" || task.RunID == nil || *task.RunID != "R7x2Kp9Q" {
		t.Errorf("client = %v, run_id = %v", task.Client, task.RunID)
	}
	if task.DispatchedAt == nil || !task.DispatchedAt.Equal(utc("2026-10-18T14:02:12Z")) {
		t.Errorf("dispatched_at = %v", task.DispatchedAt)
	}
	if task.CompletedAt == nil || !task.CompletedAt.Equal(utc("2026-10-18T14:04:17Z")) {
		t.Errorf("completed_at = %v", task.CompletedAt)
	}
	if d, ok := task.Duration(); !ok || d != 2*time.Minute+5*time.Second {
		t.Errorf("Duration() = %v, %v; want 2m5s", d, ok)
	}
	if s := task.DurationSeconds(); s == nil || *s != 125 {
		t.Errorf("DurationSeconds() = %v, want 125", s)
	}
}

func TestTaskFromAnOlderNDManagerHasNoneOfTheNewFields(t *testing.T) {
	task := decodeTask(t, taskItemBefore199)
	if task.Origin != "" || task.Schedule != nil || task.DeviceOnline != nil || task.DeviceLastSeenAt != nil ||
		task.LastResponseAt != nil || task.RequestedBy != nil || task.Client != nil || task.RunID != nil ||
		task.DispatchedAt != nil || task.CompletedAt != nil || task.DeviceDeleted {
		t.Errorf("an older item decoded new fields: %+v", task)
	}
	if _, ok := task.Duration(); ok {
		t.Error("an older item has no duration")
	}
}

// TestTaskTimesWithAndWithoutTheirZ: since 1.99 every task time carries its Z;
// before, the same instant came without it. FlexibleTime reads both, the
// fractional form too, as the same UTC instant.
func TestTaskTimesWithAndWithoutTheirZ(t *testing.T) {
	want := utc("2026-10-18T03:00:04Z")
	for _, raw := range []string{
		"2026-10-18T03:00:04Z", "2026-10-18T03:00:04", "2026-10-18T03:00:04.000000Z", "2026-10-18T03:00:04+00:00",
	} {
		task := decodeTask(t, `{"task":"x","created_at":"`+raw+`"}`)
		if !task.CreatedAt.Equal(want) {
			t.Errorf("created_at %q decoded as %v, want %v", raw, task.CreatedAt.Time, want)
		}
	}
}

func TestTaskDuration(t *testing.T) {
	at := func(s string) *time.Time { v := utc(s); return &v }
	for _, tc := range []struct {
		name                  string
		dispatched, completed *time.Time
		want                  time.Duration
		ok                    bool
	}{
		{"both", at("2026-10-18T14:02:12Z"), at("2026-10-18T14:02:15Z"), 3 * time.Second, true},
		{"the same second", at("2026-10-18T14:02:12Z"), at("2026-10-18T14:02:12Z"), 0, true},
		{"never sent", nil, at("2026-10-18T14:02:15Z"), 0, false},
		{"still running", at("2026-10-18T14:02:12Z"), nil, 0, false},
		{"completed before it was sent", at("2026-10-18T14:02:15Z"), at("2026-10-18T14:02:12Z"), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := Task{DispatchedAt: tc.dispatched, CompletedAt: tc.completed}
			d, ok := task.Duration()
			if d != tc.want || ok != tc.ok {
				t.Errorf("Duration() = %v, %v; want %v, %v", d, ok, tc.want, tc.ok)
			}
			if s := task.DurationSeconds(); (s != nil) != tc.ok {
				t.Errorf("DurationSeconds() = %v, want set: %v", s, tc.ok)
			}
		})
	}
}

// TestTaskReencodesTheServersNulls: -f json prints the decoded task, so a field
// the server sent as null stays null rather than turning into "" or false.
func TestTaskReencodesTheServersNulls(t *testing.T) {
	task := decodeTask(t, taskItem199)
	out, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"last_response_at":null`, `"requested_by":null`, `"client":null`, `"run_id":null`,
		`"dispatched_at":null`, `"completed_at":null`, `"firmware_track":null`, `"device_online":false`,
		`"device_last_seen_at":"2026-10-17T22:41:10Z"`, `"next_run_at":"2026-10-19T03:00:00Z"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("re-encoded task is missing %s:\n%s", want, out)
		}
	}
}
