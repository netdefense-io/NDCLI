package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// scheduledName is a helper to build a *string from a string literal.
func scheduledName(s string) *string { return &s }

func TestBackupConfig_SchedulingState_Unmarshal(t *testing.T) {
	// Verify that the three effective states unmarshal correctly from the
	// NDManager bb8744f+ response shape.

	cases := []struct {
		name             string
		payload          map[string]interface{}
		wantScheduled    bool
		wantAttachedName string // "" means nil expected
		wantStatus       string
	}{
		{
			name: "disabled",
			payload: map[string]interface{}{
				"status": "DISABLED", "scheduled": false,
				"attached_schedule": nil,
				"s3_endpoint":       "https://s3.example.com", "s3_bucket": "b",
				"s3_key_id": "k", "has_encryption_key": true,
				"organization": "acme",
				"created_at":   "2026-06-01T00:00:00+00:00",
				"updated_at":   "2026-06-01T00:00:00+00:00",
			},
			wantScheduled:    false,
			wantAttachedName: "",
			wantStatus:       "DISABLED",
		},
		{
			name: "enabled but not scheduled",
			payload: map[string]interface{}{
				"status": "ENABLED", "scheduled": false,
				"attached_schedule": nil,
				"s3_endpoint":       "https://s3.example.com", "s3_bucket": "b",
				"s3_key_id": "k", "has_encryption_key": true,
				"organization": "acme",
				"created_at":   "2026-06-01T00:00:00+00:00",
				"updated_at":   "2026-06-01T00:00:00+00:00",
			},
			wantScheduled:    false,
			wantAttachedName: "",
			wantStatus:       "ENABLED",
		},
		{
			name: "enabled and scheduled",
			payload: map[string]interface{}{
				"status": "ENABLED", "scheduled": true,
				"attached_schedule": "weekly-backup",
				"s3_endpoint":       "https://s3.example.com", "s3_bucket": "b",
				"s3_key_id": "k", "has_encryption_key": true,
				"organization": "acme",
				"created_at":   "2026-06-01T00:00:00+00:00",
				"updated_at":   "2026-06-01T00:00:00+00:00",
			},
			wantScheduled:    true,
			wantAttachedName: "weekly-backup",
			wantStatus:       "ENABLED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tc.payload)
			})
			srv := httptest.NewServer(handler)
			defer srv.Close()

			svc := newTestService(t, srv)
			cfg, err := svc.BackupConfigGet(context.Background(), "acme")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Status != tc.wantStatus {
				t.Errorf("Status: want %q, got %q", tc.wantStatus, cfg.Status)
			}
			if cfg.Scheduled != tc.wantScheduled {
				t.Errorf("Scheduled: want %v, got %v", tc.wantScheduled, cfg.Scheduled)
			}
			if tc.wantAttachedName == "" {
				if cfg.AttachedSchedule != nil {
					t.Errorf("AttachedSchedule: want nil, got %q", *cfg.AttachedSchedule)
				}
			} else {
				if cfg.AttachedSchedule == nil {
					t.Errorf("AttachedSchedule: want %q, got nil", tc.wantAttachedName)
				} else if *cfg.AttachedSchedule != tc.wantAttachedName {
					t.Errorf("AttachedSchedule: want %q, got %q", tc.wantAttachedName, *cfg.AttachedSchedule)
				}
			}
		})
	}
}

func TestScheduledTaskGet_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/organizations/acme/scheduled-tasks/BkCd5678" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":          "BkCd5678",
			"kind":          "BACKUP",
			"schedule_name": "weekly-backup",
			"enabled":       true,
			"created_at":    "2026-06-01T00:00:00+00:00",
			"updated_at":    "2026-06-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	spec, err := svc.ScheduledTaskGet(context.Background(), "acme", "BkCd5678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Code != "BkCd5678" {
		t.Errorf("expected code BkCd5678, got %q", spec.Code)
	}
	if spec.Kind != "BACKUP" {
		t.Errorf("expected kind BACKUP, got %q", spec.Kind)
	}
	if spec.ScheduleName != "weekly-backup" {
		t.Errorf("expected ScheduleName weekly-backup, got %q", spec.ScheduleName)
	}
}

func TestScheduledTaskGet_ValidationErrors(t *testing.T) {
	svc := &Service{}

	_, err := svc.ScheduledTaskGet(context.Background(), "", "BkCd5678")
	if err == nil {
		t.Fatal("expected error for missing org")
	}
	if svcErr, ok := err.(*Error); !ok || svcErr.Code != CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}

	_, err = svc.ScheduledTaskGet(context.Background(), "acme", "")
	if err == nil {
		t.Fatal("expected error for missing code")
	}
	if svcErr, ok := err.(*Error); !ok || svcErr.Code != CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

// TestBackupConfig_LegacyScheduleField verifies that the legacy schedule
// cron field is still parsed (server may still include it) without breaking
// the new attached_schedule/scheduled fields.
func TestBackupConfig_LegacyScheduleField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":             "ENABLED",
			"scheduled":          true,
			"attached_schedule":  "weekly-backup",
			"schedule":           "0 2 * * 0", // legacy cron field still present
			"s3_endpoint":        "https://s3.example.com",
			"s3_bucket":          "b",
			"s3_key_id":          "k",
			"has_encryption_key": true,
			"organization":       "acme",
			"created_at":         "2026-06-01T00:00:00+00:00",
			"updated_at":         "2026-06-01T00:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	cfg, err := svc.BackupConfigGet(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both old and new fields round-trip.
	if cfg.Schedule != "0 2 * * 0" {
		t.Errorf("legacy Schedule field: want %q, got %q", "0 2 * * 0", cfg.Schedule)
	}
	if cfg.AttachedSchedule == nil || *cfg.AttachedSchedule != "weekly-backup" {
		t.Errorf("AttachedSchedule: want weekly-backup, got %v", cfg.AttachedSchedule)
	}
	if !cfg.Scheduled {
		t.Error("Scheduled: want true")
	}

	// Construct the model directly to exercise the type.
	_ = models.BackupConfig{
		Status:           "ENABLED",
		Scheduled:        true,
		AttachedSchedule: scheduledName("weekly-backup"),
	}
}
