package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// A console command's timeout is clamped before it becomes a duration, so a
// huge timeout_seconds cannot wrap to an arbitrary or negative one.
func TestConsoleExecTimeout(t *testing.T) {
	for in, want := range map[int]time.Duration{
		0:                60 * time.Second,
		-5:               60 * time.Second,
		30:               30 * time.Second,
		3600:             time.Hour,
		3601:             time.Hour,
		1 << 62:          time.Hour,
		9223372036854775: time.Hour,
	} {
		if got := consoleExecTimeout(in); got != want {
			t.Errorf("consoleExecTimeout(%d) = %v, want %v", in, got, want)
		}
	}
}

// A re-bind token's TTL past NDManager's 7-day cap is refused before any
// request.
func TestDeviceRebindTokenRefusesATTLPastSevenDays(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&posts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestService(t, srv).DeviceRebindToken(context.Background(), "acme", "fw-01", MaxRebindTokenTTL+time.Second)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != "ttl must be at most 7 days" {
		t.Errorf("err = %v, want INVALID_INPUT \"ttl must be at most 7 days\"", err)
	}
	if n := atomic.LoadInt32(&posts); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}
}

// A re-bind token's TTL under a second is refused before any request (it went
// out as ttl_seconds 0, which the control plane refuses), and a fraction of a
// second is rounded up.
func TestDeviceRebindTokenSendsWholeSeconds(t *testing.T) {
	var sent []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TTLSeconds int `json:"ttl_seconds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body.TTLSeconds)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	svc := newTestService(t, srv)
	for _, ttl := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		_, err := svc.DeviceRebindToken(context.Background(), "acme", "fw-01", ttl)
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != "ttl must be at least 1 second" {
			t.Errorf("ttl %v: err = %v, want INVALID_INPUT \"ttl must be at least 1 second\"", ttl, err)
		}
	}
	if len(sent) != 0 {
		t.Fatalf("%d request(s) sent for refused TTLs", len(sent))
	}
	for _, ttl := range []time.Duration{time.Second, 1500 * time.Millisecond, 24 * time.Hour} {
		if _, err := svc.DeviceRebindToken(context.Background(), "acme", "fw-01", ttl); err != nil {
			t.Errorf("ttl %v: %v", ttl, err)
		}
	}
	if want := []int{1, 2, 86400}; !reflect.DeepEqual(sent, want) {
		t.Errorf("ttl_seconds sent %v, want %v", sent, want)
	}
}
