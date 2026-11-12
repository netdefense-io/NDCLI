package pathfinder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// While the device has not joined, ConnectExecContext keeps reporting
// progress, so an MCP client that resets its timeout on progress does not
// give up during the pairing wait.
func TestConnectExecContextReportsProgressWhilePairing(t *testing.T) {
	orig := pairingHeartbeat
	pairingHeartbeat = 30 * time.Millisecond
	t.Cleanup(func() { pairingHeartbeat = orig })

	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": false}})
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	var mu sync.Mutex
	var got []string
	c := &Client{host: "ws" + strings.TrimPrefix(srv.URL, "http"), sslVerify: true, sessionID: "k",
		onProgress: func(m string) { mu.Lock(); got = append(got, m); mu.Unlock() }}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := c.ConnectExecContext(ctx); err == nil {
		t.Fatal("expected an error: the device never pairs")
	}

	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, m := range got {
			if strings.HasPrefix(m, "Still waiting for the device to join the relay (") {
				n++
			}
		}
		return n
	}
	beats := count()
	mu.Lock()
	for _, m := range got {
		if strings.HasPrefix(m, "Still waiting") && !strings.Contains(m, "(0s left)") {
			t.Errorf("heartbeat %q counts down past the 300 ms ctx deadline; want 0s left", m)
		}
	}
	mu.Unlock()
	if beats < 3 {
		t.Errorf("got %d heartbeat messages in a 300 ms pairing wait at 30 ms, want at least 3: %q", beats, got)
	}
	time.Sleep(100 * time.Millisecond)
	if after := count(); after != beats {
		t.Errorf("the heartbeat kept sending after ConnectExecContext returned: %d → %d", beats, after)
	}
}
