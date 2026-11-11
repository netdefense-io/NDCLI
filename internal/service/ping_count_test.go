package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestCheckPingCount(t *testing.T) {
	for _, n := range []int{1, 4, 100} {
		if err := CheckPingCount(n, "count"); err != nil {
			t.Errorf("CheckPingCount(%d) = %v, want nil", n, err)
		}
	}
	for _, n := range []int{-1, 0, 101, 1000} {
		err := CheckPingCount(n, "--count")
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != "--count must be between 1 and 100" {
			t.Errorf("CheckPingCount(%d) = %#v, want INVALID_INPUT naming --count and 1 to 100", n, err)
		}
	}
}

// Run and RunRegisterSpec refuse an out-of-range PING count themselves, so a
// caller that skips its front-end's check cannot reach the endpoint with one.
func TestRunRefusesAPingCountOutOfRange(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&posts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"type": "PING", "total": 1, "tasks": []interface{}{}})
	}))
	defer srv.Close()
	svc := newTestService(t, srv)

	send := func(schedule string, count interface{}) error {
		opts := RunOpts{
			Type:     models.TaskTypePing,
			Devices:  []string{"fw-a"},
			Payload:  map[string]interface{}{"target": "1.1.1.1", "count": count},
			Schedule: schedule,
		}
		if schedule != "" {
			_, err := svc.RunRegisterSpec(context.Background(), "acme", opts)
			return err
		}
		_, err := svc.Run(context.Background(), "acme", opts)
		return err
	}

	for _, schedule := range []string{"", "nightly"} {
		for _, tc := range []struct {
			count interface{}
			want  string
		}{
			{0, "count must be between 1 and 100"},
			{101, "count must be between 1 and 100"},
			{float64(10), "count must be a whole number between 1 and 100"},
			{"10", "count must be a whole number between 1 and 100"},
		} {
			atomic.StoreInt32(&posts, 0)
			err := send(schedule, tc.count)
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != tc.want {
				t.Errorf("schedule %q, count %#v: err = %v, want INVALID_INPUT %q", schedule, tc.count, err, tc.want)
			}
			if n := atomic.LoadInt32(&posts); n != 0 {
				t.Errorf("schedule %q, count %#v: %d request(s) sent, want none", schedule, tc.count, n)
			}
		}

		atomic.StoreInt32(&posts, 0)
		if err := send(schedule, 100); err != nil {
			t.Errorf("schedule %q, count 100: %v", schedule, err)
		}
		if n := atomic.LoadInt32(&posts); n != 1 {
			t.Errorf("schedule %q, count 100: %d request(s) sent, want 1", schedule, n)
		}
	}
}
