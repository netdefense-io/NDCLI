package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// scheduleJSON is the canonical API response for a single cadence schedule
// (GET-one, with nested scheduled_tasks). Includes organization_name to
// exercise the field that was previously missing from the Go struct.
var scheduleJSON = map[string]interface{}{
	"organization_name": "acme",
	"name":              "nightly-reboot",
	"enabled":           true,
	"schedule":          "0 2 * * 0",
	"timezone":          "UTC",
	"last_fired_at":     nil,
	"next_run_at":       "2026-06-02T02:00:00+00:00",
	"created_by":        "admin@acme.com",
	"created_at":        "2026-05-01T00:00:00+00:00",
	"updated_at":        "2026-05-01T00:00:00+00:00",
	"scheduled_tasks":   []interface{}{},
}

func TestScheduleList_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []interface{}{scheduleJSON},
			"total": 1,
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	result, err := svc.ScheduleList(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected total=1, got %d", result.Total)
	}
	if len(result.Schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(result.Schedules))
	}
	if result.Schedules[0].Name != "nightly-reboot" {
		t.Errorf("expected name nightly-reboot, got %q", result.Schedules[0].Name)
	}
}

func TestScheduleList_MissingOrg(t *testing.T) {
	svc := &Service{}
	_, err := svc.ScheduleList(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for missing org")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %q", svcErr.Code)
	}
}

func TestScheduleGet_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules/nightly-reboot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scheduleJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	sch, err := svc.ScheduleGet(context.Background(), "acme", "nightly-reboot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sch.Name != "nightly-reboot" {
		t.Errorf("expected nightly-reboot, got %q", sch.Name)
	}
	if sch.Schedule != "0 2 * * 0" {
		t.Errorf("expected cron 0 2 * * 0, got %q", sch.Schedule)
	}
	// Defect #2: organization_name must round-trip from the response.
	if sch.OrganizationName != "acme" {
		t.Errorf("expected OrganizationName=acme, got %q", sch.OrganizationName)
	}
}

func TestScheduleGet_ValidationErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.ScheduleGet(context.Background(), "", "x")
	if err == nil {
		t.Fatal("expected error for missing org")
	}
	_, err = svc.ScheduleGet(context.Background(), "acme", "")
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestScheduleCreate_Success(t *testing.T) {
	var body map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scheduleJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	sch, err := svc.ScheduleCreate(context.Background(), "acme", ScheduleCreateOpts{
		Name:     "nightly-reboot",
		Cron:     "0 2 * * 0",
		Timezone: "UTC",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sch.Name != "nightly-reboot" {
		t.Errorf("expected nightly-reboot, got %q", sch.Name)
	}
	// Request body must use "schedule" key for the cron expression.
	if body["schedule"] != "0 2 * * 0" {
		t.Errorf("expected body.schedule=0 2 * * 0, got %v", body["schedule"])
	}
	// Must NOT contain task_type, targets, payload_template, or created_by.
	for _, forbidden := range []string{"task_type", "targets", "payload_template", "created_by"} {
		if _, ok := body[forbidden]; ok {
			t.Errorf("request body must not contain %q (cadence-only endpoint)", forbidden)
		}
	}
}

func TestScheduleCreate_ValidationErrors(t *testing.T) {
	svc := &Service{}

	// Missing org
	_, err := svc.ScheduleCreate(context.Background(), "", ScheduleCreateOpts{Name: "x", Cron: "* * * * *"})
	if err == nil {
		t.Fatal("expected error for missing org")
	}

	// Missing name
	_, err = svc.ScheduleCreate(context.Background(), "acme", ScheduleCreateOpts{Cron: "* * * * *"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	// Missing cron
	_, err = svc.ScheduleCreate(context.Background(), "acme", ScheduleCreateOpts{Name: "x"})
	if err == nil {
		t.Fatal("expected error for missing cron")
	}
}

func TestScheduleDelete_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules/nightly-reboot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "name": "nightly-reboot"})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	if err := svc.ScheduleDelete(context.Background(), "acme", "nightly-reboot"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScheduleSetEnabled_Enable(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules/nightly-reboot/enable" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scheduleJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	sch, err := svc.ScheduleSetEnabled(context.Background(), "acme", "nightly-reboot", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sch == nil {
		t.Fatal("expected non-nil schedule")
	}
}

func TestScheduleSetEnabled_Disable(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/schedules/nightly-reboot/disable" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scheduleJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	if _, err := svc.ScheduleSetEnabled(context.Background(), "acme", "nightly-reboot", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScheduleTaskList_OrgWide(t *testing.T) {
	// Org-wide list: GET /scheduled-tasks with no query param.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/scheduled-tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if s := r.URL.Query().Get("schedule"); s != "" {
			t.Errorf("expected no schedule query param, got %q", s)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"code":          "AbCd1234",
					"schedule_name": "nightly-reboot",
					"kind":          "RUN",
					"request":       map[string]interface{}{"type": "REBOOT"},
					"enabled":       true,
					"created_by":    "admin@acme.com",
					"last_fired_at": nil,
					"created_at":    "2026-05-01T00:00:00+00:00",
					"updated_at":    "2026-05-01T00:00:00+00:00",
				},
			},
			"total": 1,
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	// Empty scheduleFilter = org-wide.
	tasks, err := svc.ScheduleTaskList(context.Background(), "acme", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Code != "AbCd1234" {
		t.Errorf("expected code AbCd1234, got %q", tasks[0].Code)
	}
	if tasks[0].ScheduleName != "nightly-reboot" {
		t.Errorf("expected ScheduleName=nightly-reboot, got %q", tasks[0].ScheduleName)
	}
}

func TestScheduleTaskList_WithFilter(t *testing.T) {
	// Filtered list: GET /scheduled-tasks?schedule=nightly-reboot.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/scheduled-tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if s := r.URL.Query().Get("schedule"); s != "nightly-reboot" {
			t.Errorf("expected schedule=nightly-reboot, got %q", s)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}, "total": 0})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	tasks, err := svc.ScheduleTaskList(context.Background(), "acme", "nightly-reboot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestScheduledTaskSetEnabledByCode_Enable(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/scheduled-tasks/AbCd1234/enable" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":          "AbCd1234",
			"schedule_name": "nightly-reboot",
			"kind":          "RUN",
			"enabled":       true,
			"created_at":    "2026-05-01T00:00:00+00:00",
			"updated_at":    "2026-05-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	spec, err := svc.ScheduledTaskSetEnabledByCode(context.Background(), "acme", "AbCd1234", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ScheduleName != "nightly-reboot" {
		t.Errorf("expected ScheduleName=nightly-reboot, got %q", spec.ScheduleName)
	}
}

func TestScheduledTaskRemoveByCode_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/scheduled-tasks/AbCd1234" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"deleted":           true,
			"code":              "AbCd1234",
			"organization_name": "acme",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	if err := svc.ScheduledTaskRemoveByCode(context.Background(), "acme", "AbCd1234"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScheduleTaskValidation(t *testing.T) {
	svc := &Service{}

	// ScheduleTaskList: org required; scheduleFilter is optional.
	if _, err := svc.ScheduleTaskList(context.Background(), "", ""); err == nil {
		t.Fatal("expected error for missing org")
	}

	// ScheduledTaskSetEnabledByCode: both fields required.
	if _, err := svc.ScheduledTaskSetEnabledByCode(context.Background(), "", "c", true); err == nil {
		t.Fatal("expected error for missing org")
	}
	if _, err := svc.ScheduledTaskSetEnabledByCode(context.Background(), "acme", "", true); err == nil {
		t.Fatal("expected error for missing code")
	}

	// ScheduledTaskRemoveByCode: both fields required.
	if err := svc.ScheduledTaskRemoveByCode(context.Background(), "", "c"); err == nil {
		t.Fatal("expected error for missing org")
	}
	if err := svc.ScheduledTaskRemoveByCode(context.Background(), "acme", ""); err == nil {
		t.Fatal("expected error for missing code")
	}
}

// TestScheduledTaskRegisterResult_ScheduleName exercises defect #3: the
// spec descriptor field was tagged json:"schedule" but the server sends
// "schedule_name". Verify the corrected tag round-trips properly.
func TestScheduledTaskRegisterResult_ScheduleName(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Server sends "schedule_name", not "schedule".
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":          "AbCd1234",
			"kind":          "RUN",
			"request":       map[string]interface{}{"type": "REBOOT"},
			"enabled":       true,
			"schedule_name": "nightly-reboot",
			"created_by":    "admin@acme.com",
			"last_fired_at": nil,
			"created_at":    "2026-05-01T00:00:00+00:00",
			"updated_at":    "2026-05-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	spec, err := svc.RunRegisterSpec(context.Background(), "acme", RunOpts{
		Type:       "REBOOT",
		AllDevices: true,
		Schedule:   "nightly-reboot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ScheduleName != "nightly-reboot" {
		t.Errorf("expected ScheduleName=nightly-reboot, got %q (json tag mismatch?)", spec.ScheduleName)
	}
}

// TestScheduledTaskListResponse_Envelope exercises defect #1: the tasks
// sub-resource returns {"items":[...],"total":N}; an earlier version parsed
// the body into a bare []ScheduledTask and silently returned nothing.
func TestScheduledTaskListResponse_Envelope(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{"code": "Aa111111", "kind": "RUN", "enabled": true,
					"created_at": "2026-05-01T00:00:00+00:00",
					"updated_at": "2026-05-01T00:00:00+00:00"},
				{"code": "Bb222222", "kind": "SYNC", "enabled": false,
					"created_at": "2026-05-01T00:00:00+00:00",
					"updated_at": "2026-05-01T00:00:00+00:00"},
			},
			"total": 2,
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	tasks, err := svc.ScheduleTaskList(context.Background(), "acme", "my-schedule")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks from envelope, got %d — bare-array parse bug?", len(tasks))
	}
	if tasks[0].Code != "Aa111111" {
		t.Errorf("expected first code Aa111111, got %q", tasks[0].Code)
	}
	if tasks[1].Code != "Bb222222" {
		t.Errorf("expected second code Bb222222, got %q", tasks[1].Code)
	}
}

// TestScheduleOrganizationName exercises defect #2: organization_name was
// missing from the Schedule struct, so it was silently dropped on unmarshal.
func TestScheduleOrganizationName(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organization_name": "my-org",
			"name":              "weekly-sync",
			"enabled":           true,
			"schedule":          "0 3 * * 1",
			"timezone":          "America/New_York",
			"created_by":        "ops@example.com",
			"created_at":        "2026-05-01T00:00:00+00:00",
			"updated_at":        "2026-05-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	sch, err := svc.ScheduleGet(context.Background(), "my-org", "weekly-sync")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sch.OrganizationName != "my-org" {
		t.Errorf("expected OrganizationName=my-org, got %q — field missing from struct?", sch.OrganizationName)
	}
}
