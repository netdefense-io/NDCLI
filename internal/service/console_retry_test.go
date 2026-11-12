package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// answer is one scripted control-plane response.
type answer struct {
	status     int
	retryAfter string
	body       map[string]interface{}
	raw        string
}

var (
	connectOK   = answer{status: 200, body: map[string]interface{}{"task": "task-123", "status": "PENDING"}}
	pending     = answer{status: 200, body: map[string]interface{}{"task": "task-123", "status": "PENDING"}}
	completed   = answer{status: 200, body: map[string]interface{}{"task": "task-123", "status": "COMPLETED", "payload": `{"jti":"j1","pathfinder_session":"sess-abc"}`}}
	tooMany     = answer{status: 429}
	unavailable = answer{status: 503}
	serverError = answer{status: 500}
)

func tooManyAfter(v string) answer { return answer{status: 429, retryAfter: v} }

// scriptedConnect answers POST /connect and GET connect-status from the two
// scripts in order, repeating the last answer when a script runs out.
func scriptedConnect(t *testing.T, posts, gets []answer) (*httptest.Server, func() int, func() int) {
	t.Helper()
	var mu sync.Mutex
	var nPost, nGet int
	next := func(script []answer, n int) answer {
		if n < len(script) {
			return script[n]
		}
		return script[len(script)-1]
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var a answer
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect"):
			a = next(posts, nPost)
			nPost++
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/connect-status"):
			a = next(gets, nGet)
			nGet++
		default:
			mu.Unlock()
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Unlock()
		if a.retryAfter != "" {
			w.Header().Set("Retry-After", a.retryAfter)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		if a.raw != "" {
			_, _ = w.Write([]byte(a.raw))
		} else if a.body != nil {
			_ = json.NewEncoder(w).Encode(a.body)
		} else {
			_, _ = w.Write([]byte(`{"error":"refused"}`))
		}
	}))
	t.Cleanup(srv.Close)
	count := func(n *int) func() int {
		return func() int {
			mu.Lock()
			defer mu.Unlock()
			return *n
		}
	}
	return srv, count(&nPost), count(&nGet)
}

// recordWaits replaces consoleWait with a recorder that does not sleep.
// consoleWait is package state, so a test that uses this must not call
// t.Parallel.
func recordWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	orig := consoleWait
	consoleWait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	t.Cleanup(func() { consoleWait = orig })
	return &waits
}

func equalWaits(got []time.Duration, want ...time.Duration) bool {
	return slices.Equal(got, want)
}

func TestConsoleConnectRetriesTheConnectPostOn429(t *testing.T) {
	for _, tc := range []struct {
		name      string
		posts     []answer
		wantPosts int
		wantWaits []time.Duration
	}{
		{"honours Retry-After", []answer{tooManyAfter("2"), connectOK}, 2, []time.Duration{2 * time.Second, 3 * time.Second}},
		{"backs off without Retry-After", []answer{tooMany, tooMany, connectOK}, 3, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			waits := recordWaits(t)
			srv, nPost, _ := scriptedConnect(t, tc.posts, []answer{completed})
			res, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.PathfinderSession != "sess-abc" {
				t.Errorf("session = %q", res.PathfinderSession)
			}
			if nPost() != tc.wantPosts {
				t.Errorf("POSTs = %d, want %d", nPost(), tc.wantPosts)
			}
			if !equalWaits(*waits, tc.wantWaits...) {
				t.Errorf("waits = %v, want %v", *waits, tc.wantWaits)
			}
		})
	}
}

func TestConsoleConnectDoesNotRepeatTheConnectPostWhenUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name      string
		posts     []answer
		wantPosts int
		status    int
	}{
		{"a 503 may have created the task", []answer{unavailable, connectOK}, 1, 503},
		{"a 500 may have created the task", []answer{serverError, connectOK}, 1, 500},
		{"Retry-After longer than the cap", []answer{tooManyAfter("60"), connectOK}, 1, 429},
		{"Retry-After past the connect deadline", []answer{tooManyAfter("20"), connectOK}, 1, 429},
		{"gives up after three retries", []answer{tooMany}, 1 + consoleTransientRetries, 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recordWaits(t)
			srv, nPost, nGet := scriptedConnect(t, tc.posts, []answer{completed})
			_, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", 10*time.Second)
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
				t.Fatalf("err = %v, want the %d refusal", err, tc.status)
			}
			if nPost() != tc.wantPosts || nGet() != 0 {
				t.Errorf("POSTs = %d (want %d), GETs = %d (want 0)", nPost(), tc.wantPosts, nGet())
			}
		})
	}
}

func TestConsoleConnectRetriesThePollOn429And5xx(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gets      []answer
		wantWaits []time.Duration
	}{
		{"a 503 waits at least the poll interval", []answer{unavailable, completed}, []time.Duration{3 * time.Second, 3 * time.Second}},
		{"a 429 honours a longer Retry-After", []answer{tooManyAfter("5"), completed}, []time.Duration{3 * time.Second, 5 * time.Second}},
		{"a 429 with a short Retry-After still waits the poll interval", []answer{tooManyAfter("1"), completed}, []time.Duration{3 * time.Second, 3 * time.Second}},
		{"a success resets the count", []answer{serverError, serverError, serverError, pending, serverError, serverError, serverError, completed},
			[]time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second, 4 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second, 4 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			waits := recordWaits(t)
			srv, _, _ := scriptedConnect(t, []answer{connectOK}, tc.gets)
			res, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.PathfinderSession != "sess-abc" {
				t.Errorf("session = %q", res.PathfinderSession)
			}
			if !equalWaits(*waits, tc.wantWaits...) {
				t.Errorf("waits = %v, want %v", *waits, tc.wantWaits)
			}
		})
	}
}

func TestConsoleConnectPollGivesUp(t *testing.T) {
	t.Run("after three retries in a row", func(t *testing.T) {
		recordWaits(t)
		srv, _, nGet := scriptedConnect(t, []answer{connectOK}, []answer{unavailable})
		_, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute)
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
			t.Fatalf("err = %v, want the 503", err)
		}
		if nGet() != 1+consoleTransientRetries {
			t.Errorf("GETs = %d, want %d", nGet(), 1+consoleTransientRetries)
		}
	})
	t.Run("when the wait would pass the deadline", func(t *testing.T) {
		waits := recordWaits(t)
		srv, _, nGet := scriptedConnect(t, []answer{connectOK}, []answer{tooManyAfter("20"), completed})
		_, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", 5*time.Second)
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
			t.Fatalf("err = %v, want the 429", err)
		}
		if nGet() != 1 || !equalWaits(*waits, 3*time.Second) {
			t.Errorf("GETs = %d (want 1), waits = %v (want only the 3s poll interval)", nGet(), *waits)
		}
	})
	for _, status := range []int{404, 501} {
		t.Run(fmt.Sprintf("a %d is not retried", status), func(t *testing.T) {
			recordWaits(t)
			srv, _, nGet := scriptedConnect(t, []answer{connectOK}, []answer{{status: status}, completed})
			if _, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute); err == nil {
				t.Fatalf("want the %d", status)
			}
			if nGet() != 1 {
				t.Errorf("GETs = %d, want 1", nGet())
			}
		})
	}
}

// A poll whose connection drops is a GET, so it is tried again.
func TestConsoleConnectRetriesAPollTransportError(t *testing.T) {
	waits := recordWaits(t)
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(connectOK.body)
			return
		}
		if gets.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(completed.body)
	}))
	// Without keep-alives every request has its own connection, so the
	// transport does not quietly retry the dropped GET itself.
	srv.Config.SetKeepAlivesEnabled(false)
	defer srv.Close()
	res, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PathfinderSession != "sess-abc" || gets.Load() != 2 {
		t.Errorf("session = %q, GETs = %d (want sess-abc, 2)", res.PathfinderSession, gets.Load())
	}
	if !equalWaits(*waits, 3*time.Second, 3*time.Second) {
		t.Errorf("waits = %v, want [3s 3s]", *waits)
	}
}

// Cancelling console_open during a retry wait returns at once.
func TestConsoleConnectStopsWaitingWhenCancelled(t *testing.T) {
	for _, tc := range []struct {
		name        string
		posts, gets []answer
		wantPosts   int
		wantGets    int
		cancelOnNth int
	}{
		{"during a connect retry", []answer{tooMany, connectOK}, []answer{completed}, 1, 0, 1},
		{"during a poll retry", []answer{connectOK}, []answer{unavailable, completed}, 1, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, nPost, nGet := scriptedConnect(t, tc.posts, tc.gets)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			orig := consoleWait
			n := 0
			consoleWait = func(ctx context.Context, d time.Duration) error {
				n++
				if n == tc.cancelOnNth {
					cancel()
				}
				return ctx.Err()
			}
			t.Cleanup(func() { consoleWait = orig })
			_, err := newTestService(t, srv).ConsoleConnect(ctx, "org", "dev", time.Minute)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if nPost() != tc.wantPosts || nGet() != tc.wantGets {
				t.Errorf("POSTs = %d, GETs = %d, want %d, %d", nPost(), nGet(), tc.wantPosts, tc.wantGets)
			}
		})
	}
}

// A 200 whose body does not decode is a contract problem, not a hiccup: it is
// reported with its own message and not retried.
func TestConsoleConnectDoesNotRetryAnUndecodableAnswer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		posts, gets []answer
		wantMsg     string
		wantGets    int
	}{
		{"connect answer", []answer{{status: 200, raw: "{not json"}}, []answer{completed}, "parse connect init response", 0},
		{"status answer", []answer{connectOK}, []answer{{status: 200, raw: "{not json"}, completed}, "parse connect status", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recordWaits(t)
			srv, nPost, nGet := scriptedConnect(t, tc.posts, tc.gets)
			_, err := newTestService(t, srv).ConsoleConnect(context.Background(), "org", "dev", time.Minute)
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want %q", err, tc.wantMsg)
			}
			if nPost() != 1 || nGet() != tc.wantGets {
				t.Errorf("POSTs = %d, GETs = %d, want 1, %d", nPost(), nGet(), tc.wantGets)
			}
		})
	}
}

// A retry is not started past the caller's own deadline either, so the
// refusal comes back instead of a bare context error.
func TestConsoleConnectRespectsTheCallersDeadline(t *testing.T) {
	recordWaits(t)
	srv, nPost, _ := scriptedConnect(t, []answer{tooManyAfter("5"), connectOK}, []answer{completed})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := newTestService(t, srv).ConsoleConnect(ctx, "org", "dev", time.Minute)
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Fatalf("err = %v, want the 429", err)
	}
	if nPost() != 1 {
		t.Errorf("POSTs = %d, want 1", nPost())
	}
}

// failingAuthAfter hands out a token n times, then fails like an expired
// session that cannot be refreshed.
type failingAuthAfter struct{ n int }

func (f *failingAuthAfter) GetAccessToken() (string, error) {
	if f.n <= 0 {
		return "", errors.New("session expired")
	}
	f.n--
	return "test-token", nil
}
func (f *failingAuthAfter) ForceRefresh() error { return errors.New("session expired") }

// A failed token refresh is not a transient network error: a signed-out user
// must see it at once, not after three retries.
func TestConsoleConnectDoesNotRetryAnAuthFailure(t *testing.T) {
	waits := recordWaits(t)
	srv, nPost, nGet := scriptedConnect(t, []answer{connectOK}, []answer{completed})
	svc := New(api.NewClient(srv.URL, false, &failingAuthAfter{n: 1}), nil, nil)
	_, err := svc.ConsoleConnect(context.Background(), "org", "dev", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "failed to get access token") {
		t.Fatalf("err = %v, want the token failure", err)
	}
	if nPost() != 1 || nGet() != 0 || !equalWaits(*waits, 3*time.Second) {
		t.Errorf("POSTs = %d, GETs = %d, waits = %v; want 1, 0, [3s]", nPost(), nGet(), *waits)
	}
}

// Cancelling during an in-flight poll ends the connect with the cancel, not a
// retry.
func TestConsoleConnectCancelDuringAPollIsNotRetried(t *testing.T) {
	waits := recordWaits(t)
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(connectOK.body)
			return
		}
		gets.Add(1)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, err := newTestService(t, srv).ConsoleConnect(ctx, "org", "dev", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if gets.Load() != 1 || !equalWaits(*waits, 3*time.Second) {
		t.Errorf("GETs = %d, waits = %v; want 1 and only the poll interval", gets.Load(), *waits)
	}
}

// The deadline check uses the wait the loop really takes, the poll interval
// floor included.
func TestConsoleConnectPollRetryRespectsTheCallersDeadline(t *testing.T) {
	recordWaits(t)
	srv, _, nGet := scriptedConnect(t, []answer{connectOK}, []answer{unavailable, completed})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := newTestService(t, srv).ConsoleConnect(ctx, "org", "dev", time.Minute)
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
		t.Fatalf("err = %v, want the 503 (the 3s wait would pass the 2s deadline)", err)
	}
	if nGet() != 1 {
		t.Errorf("GETs = %d, want 1", nGet())
	}
}

// refreshUnreachable fails the token refresh the way Auth0 being unreachable
// does: the provider wraps the *url.Error of its own HTTP call.
type refreshUnreachable struct{ n int }

func (f *refreshUnreachable) GetAccessToken() (string, error) {
	if f.n <= 0 {
		return "", fmt.Errorf("failed to refresh token: %w", &url.Error{Op: "Post", URL: "https://auth.example/oauth/token", Err: errors.New("dial tcp: connection refused")})
	}
	f.n--
	return "test-token", nil
}
func (f *refreshUnreachable) ForceRefresh() error { return errors.New("unreachable") }

// A refresh that failed in transit is still not retried: the refresh token
// rotates, and a repeat after a lost answer could get the session revoked.
func TestConsoleConnectDoesNotRetryARefreshThatFailedInTransit(t *testing.T) {
	waits := recordWaits(t)
	srv, _, nGet := scriptedConnect(t, []answer{connectOK}, []answer{completed})
	svc := New(api.NewClient(srv.URL, false, &refreshUnreachable{n: 1}), nil, nil)
	_, err := svc.ConsoleConnect(context.Background(), "org", "dev", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "failed to refresh token") {
		t.Fatalf("err = %v, want the refresh failure", err)
	}
	if nGet() != 0 || !equalWaits(*waits, 3*time.Second) {
		t.Errorf("GETs = %d, waits = %v; want 0 and only the poll interval", nGet(), *waits)
	}
}

func TestConsoleConnectWithProgressReportsEachStep(t *testing.T) {
	recordWaits(t)
	srv, _, _ := scriptedConnect(t, []answer{tooMany, connectOK}, []answer{pending, unavailable, completed})
	var got []string
	_, err := newTestService(t, srv).ConsoleConnectWithProgress(context.Background(), "org", "fw-01", time.Minute, func(m string) { got = append(got, m) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"Requesting a connect task for fw-01",
		"The control plane refused the connect request; retrying in 1s",
		"Waiting for fw-01 to pick up the connect task (PENDING, ",
		"The control plane did not answer the status poll; retrying in 3s",
	}
	if len(got) != len(want) {
		t.Fatalf("progress = %q, want %d messages", got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("progress[%d] = %q, want it to start with %q", i, got[i], want[i])
		}
	}
}
