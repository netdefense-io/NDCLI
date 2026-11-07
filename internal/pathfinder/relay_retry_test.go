package pathfinder

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// limitedRelay answers the first `refusals` upgrades with 429 and the given
// Retry-After, then accepts and registers.
func limitedRelay(t *testing.T, refusals int32, retryAfter string) (*Client, *int32) {
	t.Helper()
	var calls int32
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= refusals {
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ReadMessage()
		conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": false}})
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)
	return &Client{host: "ws" + strings.TrimPrefix(srv.URL, "http"), sslVerify: true, sessionID: "k"}, &calls
}

func rateLimitedFrom(t *testing.T, err error) *RateLimitedError {
	t.Helper()
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want RateLimitedError", err)
	}
	return rl
}

func TestDialRelayRetriesOnceAfterRetryAfter(t *testing.T) {
	c, calls := limitedRelay(t, 1, "1")
	start := time.Now()
	relay, stop, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	defer stop()
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Errorf("dials = %d, want 2", n)
	}
	if el := time.Since(start); el < time.Second {
		t.Errorf("retried after %v, before Retry-After", el)
	}
}

func TestDialRelayNoRetryWhenRetryAfterTooLong(t *testing.T) {
	c, calls := limitedRelay(t, 5, "11")
	_, _, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if rl := rateLimitedFrom(t, err); rl.RetryAfter != 11*time.Second {
		t.Errorf("retry after = %v", rl.RetryAfter)
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("dials = %d, want 1", n)
	}
}

func TestDialRelayTwoRefusalsReportSecondRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", strconv.Itoa(int(n)))
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := &Client{host: "ws" + strings.TrimPrefix(srv.URL, "http"), sslVerify: true, sessionID: "k"}
	_, _, err := c.dialRelay(context.Background(), c.buildWebSocketURL())
	if rl := rateLimitedFrom(t, err); rl.RetryAfter != 2*time.Second {
		t.Errorf("retry after = %v, want the second response's 2s", rl.RetryAfter)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("dials = %d, want exactly 2", n)
	}
}

func TestDialRelayContextCancelledDuringWait(t *testing.T) {
	c, calls := limitedRelay(t, 5, "5")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	_, _, err := c.dialRelay(ctx, c.buildWebSocketURL())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("returned after %v", el)
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("dials = %d, want 1", n)
	}
}

func TestDialRelayDeadlineShorterThanRetryAfter(t *testing.T) {
	c, calls := limitedRelay(t, 5, "5")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := c.dialRelay(ctx, c.buildWebSocketURL())
	rateLimitedFrom(t, err)
	if el := time.Since(start); el > time.Second {
		t.Errorf("waited %v despite the short deadline", el)
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("dials = %d, want 1", n)
	}
}
