package pathfinder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// unpairedRelay registers the client and then never pairs it, like a relay
// whose device has not joined.
func unpairedRelay(t *testing.T) string {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": false}})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// An interrupt while the device has not paired ends the wait at once, so the
// caller can cancel the CONNECT task; it used to wait up to 120 s or die to
// the signal with the task left behind.
func TestConnectContextGivesUpBeforePairing(t *testing.T) {
	c := &Client{host: unpairedRelay(t), sessionID: "key"}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	err := c.ConnectContext(ctx)
	if err == nil {
		t.Fatal("ConnectContext returned nil with the device never paired")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("gave up after %v, want right after the cancel", took)
	}
}

// Once the device pairs, the session no longer follows ctx, and OnPaired
// tells the caller so it can hand its signals back.
func TestConnectContextCallsOnPairedOnce(t *testing.T) {
	paired := 0
	c := &Client{host: fakeRelay(t, func(*websocket.Conn) { time.Sleep(time.Second) }), sessionID: "key", onPaired: func() { paired++ }}
	if err := c.ConnectContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if paired != 1 {
		t.Errorf("OnPaired ran %d times, want 1", paired)
	}
}
