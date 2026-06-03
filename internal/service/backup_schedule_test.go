package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// attachResponseJSON is the canonical 200 body for an attach call.
var attachResponseJSON = map[string]interface{}{
	"code":          "BkCd5678",
	"kind":          "BACKUP",
	"schedule_name": "weekly-backup",
	"enabled":       true,
	"created_by":    "admin@acme.com",
	"created_at":    "2026-06-01T00:00:00+00:00",
	"updated_at":    "2026-06-01T00:00:00+00:00",
}

// detachResponseJSON is the canonical 200 body for a detach call.
var detachResponseJSON = map[string]interface{}{
	"detached":          true,
	"organization_name": "acme",
}

func TestBackupConfigSetSchedule_Attach(t *testing.T) {
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/backup-config/schedule" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(attachResponseJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	result, err := svc.BackupConfigSetSchedule(context.Background(), "acme", "weekly-backup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attached == nil {
		t.Fatal("expected Attached to be non-nil")
	}
	if result.Detached != nil {
		t.Error("expected Detached to be nil on attach")
	}
	if result.Attached.Code != "BkCd5678" {
		t.Errorf("expected code BkCd5678, got %q", result.Attached.Code)
	}
	if result.Attached.ScheduleName != "weekly-backup" {
		t.Errorf("expected schedule_name weekly-backup, got %q", result.Attached.ScheduleName)
	}
	if result.Attached.Kind != "BACKUP" {
		t.Errorf("expected kind BACKUP, got %q", result.Attached.Kind)
	}

	// Verify the request body sent "schedule":"weekly-backup".
	if receivedBody["schedule"] != "weekly-backup" {
		t.Errorf("expected body.schedule=weekly-backup, got %v", receivedBody["schedule"])
	}
}

func TestBackupConfigSetSchedule_Detach(t *testing.T) {
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/backup-config/schedule" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detachResponseJSON)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	result, err := svc.BackupConfigSetSchedule(context.Background(), "acme", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Detached == nil {
		t.Fatal("expected Detached to be non-nil")
	}
	if result.Attached != nil {
		t.Error("expected Attached to be nil on detach")
	}
	if !result.Detached.Detached {
		t.Error("expected Detached.Detached=true")
	}
	if result.Detached.OrganizationName != "acme" {
		t.Errorf("expected OrganizationName=acme, got %q", result.Detached.OrganizationName)
	}

	// Verify the request body sent "schedule":null.
	if v, ok := receivedBody["schedule"]; !ok || v != nil {
		t.Errorf("expected body.schedule=null on detach, got %v (present=%v)", v, ok)
	}
}

func TestBackupConfigSetSchedule_404_SurfacedCleanly(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"detail": "schedule not found",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.BackupConfigSetSchedule(context.Background(), "acme", "nonexistent-schedule")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	// The error must be a service error (wraps the API error) — not a panic or
	// nil-pointer, and not silently swallowed.
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *service.Error, got %T: %v", err, err)
	}
	if svcErr.Code != CodeAPIError {
		t.Errorf("expected CodeAPIError, got %q", svcErr.Code)
	}
}

func TestBackupConfigSetSchedule_ValidationErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.BackupConfigSetSchedule(context.Background(), "", "weekly-backup")
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

func TestBackupConfigCreate_NoScheduleField(t *testing.T) {
	// Verify that BackupConfigCreate no longer sends a "schedule" key in the
	// request body (schedule is now managed via BackupConfigSetSchedule).
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"s3_endpoint":        "https://s3.example.com",
			"s3_bucket":          "my-bucket",
			"s3_key_id":          "KEYID",
			"status":             "ENABLED",
			"has_encryption_key": true,
			"organization":       "acme",
			"created_at":         "2026-06-01T00:00:00+00:00",
			"updated_at":         "2026-06-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.BackupConfigCreate(context.Background(), "acme", BackupConfigCreateOpts{
		S3Endpoint:    "https://s3.example.com",
		S3Bucket:      "my-bucket",
		S3KeyID:       "KEYID",
		S3AccessKey:   "SECRET",
		EncryptionKey: "ENCKEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := receivedBody["schedule"]; ok {
		t.Error("BackupConfigCreate must not send 'schedule' in the request body; use BackupConfigSetSchedule instead")
	}
}
