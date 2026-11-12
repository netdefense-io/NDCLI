package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// cancelServer answers PUT /api/v1/tasks/{task}/cancel with each scripted
// status in turn and records the requests.
func cancelServer(t *testing.T, answers ...answer) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n := len(seen)
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		a := answer{status: http.StatusOK, raw: `{"message": "Remote session closed"}`}
		if n < len(answers) {
			a = answers[n]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.raw)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// A CONNECT session ndcli gives up is closed through the task's cancel, so the
// device's leg stops waiting out the TTL (NDPathFinder #30). An unavailable
// relay is tried once more; any other refusal is not.
func TestCloseConnectSession(t *testing.T) {
	waits := recordWaits(t)
	const cancelPath = "PUT /api/v1/tasks/task-123/cancel"
	unavailableRelay := answer{status: http.StatusBadGateway, raw: `{"error": "The relay could not close the session", "code": "REMOTE_SESSION_UNAVAILABLE"}`}
	closed := answer{status: http.StatusOK, raw: `{"message": "Remote session closed"}`}
	for _, tc := range []struct {
		name     string
		answers  []answer
		requests int
		wantErr  bool
		waits    []time.Duration
	}{
		{"closed", []answer{closed}, 1, false, nil},
		{"relay unavailable, then closed", []answer{unavailableRelay, closed}, 2, false, []time.Duration{time.Second}},
		{"relay unavailable twice", []answer{unavailableRelay, unavailableRelay}, 2, true, []time.Duration{time.Second}},
		{"not cancellable", []answer{{status: http.StatusConflict, raw: `{"error": "x", "code": "TASK_NOT_CANCELLABLE"}`}}, 1, true, nil},
		{"read-only token", []answer{{status: http.StatusForbidden, raw: `{"error": "x", "code": "TOKEN_READ_ONLY"}`}}, 1, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*waits = nil
			srv, seen := cancelServer(t, tc.answers...)
			err := newTestService(t, srv).CloseConnectSession("task-123")
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want error %v", err, tc.wantErr)
			}
			got := seen()
			if len(got) != tc.requests {
				t.Fatalf("requests = %v, want %d", got, tc.requests)
			}
			for _, r := range got {
				if r != cancelPath {
					t.Errorf("sent %s, want %s", r, cancelPath)
				}
			}
			if !equalWaits(*waits, tc.waits...) {
				t.Errorf("waits = %v, want %v", *waits, tc.waits)
			}
		})
	}

	t.Run("no task", func(t *testing.T) {
		srv, seen := cancelServer(t)
		if err := newTestService(t, srv).CloseConnectSession(""); err != nil || len(seen()) != 0 {
			t.Errorf("err = %v, requests = %v; want nothing sent", err, seen())
		}
	})
}

// ConsoleConnect hands back the CONNECT task with the session, so a caller can
// close it.
func TestConsoleConnectReturnsTheTask(t *testing.T) {
	recordWaits(t)
	srv, _, _ := scriptedConnect(t, []answer{connectOK}, []answer{completed})
	res, err := newTestService(t, srv).ConsoleConnect(t.Context(), "acme", "fw-01", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Task != "task-123" || res.PathfinderSession != "sess-abc" {
		t.Errorf("result = %+v, want task task-123 and session sess-abc", res)
	}
}

// A connect given up before the task completes cancels it, so a device that
// picks it up later does not wait on the relay for nobody. A task that ended
// on its own, or a session handed back, is left alone.
func TestConsoleConnectCancelsATaskItGivesUp(t *testing.T) {
	recordWaits(t)
	statusOf := func(status string) answer {
		return answer{status: http.StatusOK, body: map[string]interface{}{"task": "task-123", "status": status}}
	}
	for _, tc := range []struct {
		name    string
		poll    answer
		timeout time.Duration
		cancels int
	}{
		{"timeout while pending", pending, 50 * time.Millisecond, 1},
		{"a poll refused for good", answer{status: http.StatusNotFound}, time.Minute, 1},
		{"session handed back", completed, time.Minute, 0},
		{"task failed", statusOf("FAILED"), time.Minute, 0},
		{"task cancelled elsewhere", statusOf("CANCELED"), time.Minute, 0},
		{"task expired", statusOf("EXPIRED"), time.Minute, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			cancels := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				a := tc.poll
				switch r.Method {
				case http.MethodPost:
					a = connectOK
				case http.MethodPut:
					mu.Lock()
					cancels++
					mu.Unlock()
					a = answer{status: http.StatusOK, raw: `{"message": "Task cancelled successfully"}`}
				}
				w.WriteHeader(a.status)
				if a.raw != "" {
					_, _ = io.WriteString(w, a.raw)
				} else if a.body != nil {
					_ = json.NewEncoder(w).Encode(a.body)
				} else {
					_, _ = io.WriteString(w, `{"error": "refused"}`)
				}
			}))
			defer srv.Close()
			_, _ = newTestService(t, srv).ConsoleConnect(t.Context(), "acme", "fw-01", tc.timeout)
			mu.Lock()
			defer mu.Unlock()
			if cancels != tc.cancels {
				t.Errorf("cancels = %d, want %d", cancels, tc.cancels)
			}
		})
	}

	t.Run("caller gives up", func(t *testing.T) {
		var mu sync.Mutex
		cancels := 0
		ctx, cancel := context.WithCancel(t.Context())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.Method {
			case http.MethodPost:
				_ = json.NewEncoder(w).Encode(connectOK.body)
			case http.MethodPut:
				mu.Lock()
				cancels++
				mu.Unlock()
				_, _ = io.WriteString(w, `{"message": "Task cancelled successfully"}`)
			default:
				cancel()
				_ = json.NewEncoder(w).Encode(pending.body)
			}
		}))
		defer srv.Close()
		if _, err := newTestService(t, srv).ConsoleConnect(ctx, "acme", "fw-01", time.Minute); err == nil {
			t.Fatal("a cancelled connect succeeded")
		}
		mu.Lock()
		defer mu.Unlock()
		if cancels != 1 {
			t.Errorf("cancels = %d, want 1 (the cancel runs on its own deadline)", cancels)
		}
	})
}
