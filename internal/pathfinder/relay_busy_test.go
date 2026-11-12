package pathfinder

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func noJitter(t *testing.T) {
	t.Helper()
	orig := relayBusyJitter
	relayBusyJitter = func() time.Duration { return 0 }
	t.Cleanup(func() { relayBusyJitter = orig })
}

type refusal struct {
	status     int
	retryAfter string
}

// refusingRelay answers each upgrade with the next refusal in turn, then
// accepts and registers. A 503's jitter is fixed at 0 for the test.
func refusingRelay(t *testing.T, refusals ...refusal) (*Client, *int32) {
	t.Helper()
	noJitter(t)
	var calls int32
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := int(atomic.AddInt32(&calls, 1)); n <= len(refusals) {
			if ra := refusals[n-1].retryAfter; ra != "" {
				w.Header().Set("Retry-After", ra)
			}
			http.Error(w, "refused", refusals[n-1].status)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": false}})
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)
	return &Client{host: "ws" + strings.TrimPrefix(srv.URL, "http"), sslVerify: true, sessionID: "k"}, &calls
}

// A relay at capacity (503 with Retry-After) is waited out up to twice, and
// RelayRetryAfter reports the whole wait.
func TestDialRelayWaitsOutARelayAtCapacityTwice(t *testing.T) {
	busy := refusal{http.StatusServiceUnavailable, "1"}
	c, calls := refusingRelay(t, busy, busy)
	var lines []string
	c.onProgress = func(msg string) { lines = append(lines, msg) }
	start := time.Now()
	relay, stop, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	defer stop()
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("dials = %d, want 3", n)
	}
	if el := time.Since(start); el < 2*time.Second {
		t.Errorf("connected after %v, before both waits", el)
	}
	if got := c.RelayRetryAfter(); got != 2*time.Second {
		t.Errorf("RelayRetryAfter() = %v, want the 2s waited out", got)
	}
	if len(lines) != 2 || lines[0] != "Relay is at capacity; retrying in 1s..." {
		t.Errorf("progress = %q", lines)
	}
}

// A third 503 is not waited out: the dial was already retried twice.
func TestDialRelayGivesUpOnAThirdRelayAtCapacity(t *testing.T) {
	busy := refusal{http.StatusServiceUnavailable, "1"}
	c, calls := refusingRelay(t, busy, busy, busy)
	_, _, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	var be *RelayBusyError
	if !errors.As(err, &be) || be.RetryAfter != time.Second {
		t.Fatalf("err = %v, want RelayBusyError with the 1s Retry-After", err)
	}
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("dials = %d, want 3", n)
	}
}

// A 503 with no Retry-After, or one longer than maxRelayBusyWait, is returned
// at once.
func TestDialRelayDoesNotRetryOtherUnavailableAnswers(t *testing.T) {
	for _, tc := range []struct {
		retryAfter string
		want       time.Duration
		text       string
	}{
		{"", 0, "relay is unavailable (HTTP 503)"},
		{"16", 16 * time.Second, "relay is refusing new connections for now (HTTP 503); retry in 16s"},
	} {
		c, calls := refusingRelay(t, refusal{http.StatusServiceUnavailable, tc.retryAfter})
		_, _, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
		var be *RelayBusyError
		if !errors.As(err, &be) || be.RetryAfter != tc.want || be.Error() != tc.text {
			t.Errorf("Retry-After %q: err = %v, want RelayBusyError %q", tc.retryAfter, err, tc.text)
		}
		if n := atomic.LoadInt32(calls); n != 1 {
			t.Errorf("Retry-After %q: dials = %d, want 1", tc.retryAfter, n)
		}
	}
}

// A 429 keeps its single retry, and counts toward the two a dial gets: a 429
// then a 503 is waited out twice, a 429 then a 429 once.
func TestDialRelayRetriesCountTogether(t *testing.T) {
	limited := refusal{http.StatusTooManyRequests, "1"}
	busy := refusal{http.StatusServiceUnavailable, "1"}

	c, calls := refusingRelay(t, limited, busy)
	relay, stop, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if err != nil {
		t.Fatalf("429 then 503: %v", err)
	}
	_ = relay.Close()
	stop()
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("429 then 503: dials = %d, want 3", n)
	}

	c, calls = refusingRelay(t, limited, limited)
	_, _, err = c.dialRelay(context.Background(), c.buildWebSocketURL())
	_ = rateLimitedFrom(t, err)
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Errorf("429 then 429: dials = %d, want 2", n)
	}
}

func TestRelayBusyRetryAllowedBoundary(t *testing.T) {
	for _, tc := range []struct {
		retryAfter time.Duration
		want       bool
	}{
		{0, false},
		{time.Second, true},
		{maxRelayBusyWait, true},
		{maxRelayBusyWait + time.Nanosecond, false},
	} {
		if got := relayBusyRetryAllowed(tc.retryAfter); got != tc.want {
			t.Errorf("relayBusyRetryAllowed(%v) = %v, want %v", tc.retryAfter, got, tc.want)
		}
	}
	if maxRelayBusyWait != 15*time.Second || maxRelayRetries != 2 {
		t.Errorf("maxRelayBusyWait = %v, maxRelayRetries = %d; want the documented 15s and 2", maxRelayBusyWait, maxRelayRetries)
	}
}

// relayRetryWait decides each refusal on its own: a 429 once, a 503 while the
// dial has retries left, two in all, any order; a 503's wait gets the jitter.
func TestRelayRetryWait(t *testing.T) {
	orig := relayBusyJitter
	relayBusyJitter = func() time.Duration { return 300 * time.Millisecond }
	t.Cleanup(func() { relayBusyJitter = orig })
	limited := &RateLimitedError{RetryAfter: time.Second}
	busy := &RelayBusyError{RetryAfter: 15 * time.Second}
	for _, tc := range []struct {
		name        string
		err         error
		retries     int
		rateLimited bool
		wait        time.Duration
		kind        relayRefusal
	}{
		{"first 429", limited, 0, false, time.Second, relayRateLimited},
		{"429 after a 503", limited, 1, false, time.Second, relayRateLimited},
		{"second 429", limited, 1, true, 0, 0},
		{"first 503", busy, 0, false, 15*time.Second + 300*time.Millisecond, relayBusy},
		{"503 after a 429", busy, 1, true, 15*time.Second + 300*time.Millisecond, relayBusy},
		{"third refusal", busy, maxRelayRetries, false, 0, 0},
		{"503 without Retry-After", &RelayBusyError{}, 0, false, 0, 0},
		{"429 too long", &RateLimitedError{RetryAfter: 11 * time.Second}, 0, false, 0, 0},
		{"another error", errors.New("bad handshake"), 0, false, 0, 0},
	} {
		if wait, kind := relayRetryWait(tc.err, tc.retries, tc.rateLimited); wait != tc.wait || kind != tc.kind {
			t.Errorf("%s: wait %v, kind %d; want %v, %d", tc.name, wait, kind, tc.wait, tc.kind)
		}
	}
}

// A wait longer than pairingHeartbeat repeats its progress line, so a caller
// that resets its timeout on progress is not cut off during it.
func TestDialRelayRepeatsProgressThroughAWait(t *testing.T) {
	orig := pairingHeartbeat
	pairingHeartbeat = 300 * time.Millisecond
	t.Cleanup(func() { pairingHeartbeat = orig })
	c, _ := refusingRelay(t, refusal{http.StatusServiceUnavailable, "1"})
	var lines []string
	c.onProgress = func(msg string) { lines = append(lines, msg) }
	relay, stop, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	defer stop()
	if len(lines) < 3 || lines[0] != "Relay is at capacity; retrying in 1s..." {
		t.Errorf("progress = %q, want the line repeated through the 1s wait", lines)
	}
}

// A beat that fires as the wait ends does not announce "retrying in 0s".
func TestWaitOutRelaySkipsABeatAtTheEnd(t *testing.T) {
	orig := pairingHeartbeat
	pairingHeartbeat = 500 * time.Millisecond
	t.Cleanup(func() { pairingHeartbeat = orig })
	var lines []string
	c := &Client{onProgress: func(msg string) { lines = append(lines, msg) }}
	if err := c.waitOutRelay(context.Background(), 500*time.Millisecond, relayRateLimited); err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if strings.Contains(l, "retrying in 0s") {
			t.Errorf("progress %q", lines)
		}
	}
}
