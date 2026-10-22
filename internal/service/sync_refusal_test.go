package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// A sync that every targeted device refused is an error for the front-ends.
// One that triggered any device, or only skipped devices already in sync, is
// not.
func TestSyncApplyRefusal(t *testing.T) {
	refused := models.SyncError{DeviceName: "fw-a", Error: "device offline"}
	for _, tc := range []struct {
		name   string
		status int
		resp   models.SyncApplyResponse
		want   bool
	}{
		{"every device refused", http.StatusBadRequest, models.SyncApplyResponse{Errors: []models.SyncError{refused, refused}}, true},
		{"refused and skipped, none triggered", http.StatusBadRequest, models.SyncApplyResponse{Skipped: 2, Errors: []models.SyncError{refused}}, true},
		{"some triggered", http.StatusMultiStatus, models.SyncApplyResponse{DevicesAffected: 1, Tasks: []models.SyncTaskResult{{Task: "T1"}}, Errors: []models.SyncError{refused}}, false},
		{"all triggered", http.StatusOK, models.SyncApplyResponse{DevicesAffected: 2}, false},
		{"only skipped", http.StatusOK, models.SyncApplyResponse{Skipped: 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := SyncApplyRefusal(&SyncApplyResult{Response: &tc.resp, StatusCode: tc.status})
			if tc.want {
				if err == nil || err.Error() != "sync failed for all devices" {
					t.Errorf("err = %v, want \"sync failed for all devices\"", err)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
	if err := SyncApplyRefusal(nil); err != nil {
		t.Errorf("nil result: err = %v", err)
	}
}

// A refusal of the request itself (a read-only token, no write access to the
// organization, an unknown schedule) is the error the server sent, code
// included, never an empty sync result.
func TestSyncRequestRefusalsAreErrors(t *testing.T) {
	answer := func(status int, body string) *Service {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return newTestService(t, srv)
	}

	for _, tc := range []struct {
		name, body, code string
	}{
		{"read-only token", `{"error":"This personal access token is read-only","code":"TOKEN_READ_ONLY"}`, "TOKEN_READ_ONLY"},
		{"no write access", `{"error":"Access denied: requires org:rw","code":"ACCESS_DENIED"}`, "ACCESS_DENIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := answer(http.StatusForbidden, tc.body).SyncApply(context.Background(), "acme", SyncFilter{}, false)
			if err == nil {
				t.Fatalf("SyncApply returned a result for a 403: %+v", res.Response)
			}
			if got := ErrorCode(err); got != tc.code {
				t.Errorf("code = %q, want %q", got, tc.code)
			}
		})
	}

	// Only the 400 that names refused devices is a sync result; NDManager
	// answers no other 400 on this route today, so this pins the fallback.
	t.Run("400 without device errors", func(t *testing.T) {
		svc := answer(http.StatusBadRequest, `{"error":"Invalid device pattern","code":"INVALID_PATTERN"}`)
		res, err := svc.SyncApply(context.Background(), "acme", SyncFilter{Device: "("}, false)
		if err == nil {
			t.Fatalf("SyncApply returned a result for a plain 400: %+v", res.Response)
		}
		if got := ErrorCode(err); got != "INVALID_PATTERN" {
			t.Errorf("code = %q, want INVALID_PATTERN", got)
		}
		if !strings.Contains(err.Error(), "Invalid device pattern") {
			t.Errorf("err = %v, want the server's message", err)
		}
	})

	t.Run("400 that is not JSON", func(t *testing.T) {
		svc := answer(http.StatusBadRequest, `<html><body>400 Bad Request</body></html>`)
		res, err := svc.SyncApply(context.Background(), "acme", SyncFilter{}, false)
		if err == nil {
			t.Fatalf("SyncApply returned a result for a non-JSON 400: %+v", res.Response)
		}
	})

	t.Run("unknown schedule", func(t *testing.T) {
		svc := answer(http.StatusNotFound, `{"detail":"Schedule 'nightly' not found"}`)
		spec, err := svc.SyncRegisterSpec(context.Background(), "acme", SyncFilter{Schedule: "nightly"}, false)
		if err == nil {
			t.Fatalf("SyncRegisterSpec returned a spec for a 404: %+v", spec)
		}
		if !strings.Contains(err.Error(), "Schedule 'nightly' not found") {
			t.Errorf("err = %v", err)
		}
	})
}
