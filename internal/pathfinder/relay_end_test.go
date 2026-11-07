package pathfinder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeRelay upgrades /ws, answers register with registered+paired, then runs
// script on the connection.
func fakeRelay(t *testing.T, script func(*websocket.Conn)) string {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": true}})
		conn.WriteJSON(map[string]any{"type": "paired"})
		script(conn)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func connectPaired(t *testing.T, url string) *RelayClient {
	t.Helper()
	rc := NewRelayClient(url, "key", true)
	if err := rc.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rc.Close() })
	if err := rc.WaitForPairing(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	return rc
}

func waitDone(t *testing.T, rc *RelayClient) {
	t.Helper()
	select {
	case <-rc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("relay session did not end")
	}
}

func TestRelayPeerOfflineEndsSession(t *testing.T) {
	url := fakeRelay(t, func(c *websocket.Conn) {
		time.Sleep(300 * time.Millisecond)
		c.WriteJSON(map[string]any{"type": "peer_offline"})
		time.Sleep(time.Second)
	})
	rc := connectPaired(t, url)
	waitDone(t, rc)
	if got := rc.EndReason(); got != "device went offline" {
		t.Errorf("reason = %q", got)
	}
	if rc.IsConnected() {
		t.Error("still connected after peer_offline")
	}
}

func TestRelayCloseFrameReasons(t *testing.T) {
	cases := []struct {
		name string
		code int
		text string
		want string
	}{
		{"normal with text", websocket.CloseNormalClosure, "peer disconnected (agent)", "peer disconnected (agent)"},
		{"going away", websocket.CloseGoingAway, "server restarting", "server restarting"},
		{"normal empty", websocket.CloseNormalClosure, "", "session closed by the relay"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := fakeRelay(t, func(c *websocket.Conn) {
				time.Sleep(300 * time.Millisecond)
				c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(tc.code, tc.text), time.Now().Add(time.Second))
				time.Sleep(200 * time.Millisecond)
			})
			rc := connectPaired(t, url)
			waitDone(t, rc)
			if got := rc.EndReason(); got != tc.want {
				t.Errorf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRelayConnectRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	rc := NewRelayClient("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", "key", true)
	err := rc.Connect()
	rl, ok := err.(*RateLimitedError)
	if !ok {
		t.Fatalf("err = %v, want *RateLimitedError", err)
	}
	if rl.RetryAfter != 7*time.Second || !strings.Contains(rl.Error(), "retry in 7s") {
		t.Errorf("unexpected error: %v", rl)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if parseRetryAfter("") != 0 || parseRetryAfter("junk") != 0 || parseRetryAfter("-3") != 0 {
		t.Error("expected 0 for absent/invalid")
	}
	d := parseRetryAfter(time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat))
	if d <= 0 || d > 31*time.Second {
		t.Errorf("http-date parse = %v", d)
	}
}

func TestExecHandleEndedByRelay(t *testing.T) {
	url := fakeRelay(t, func(c *websocket.Conn) {
		time.Sleep(600 * time.Millisecond)
		c.WriteJSON(map[string]any{"type": "peer_offline"})
		time.Sleep(time.Second)
	})
	rc := connectPaired(t, url)
	mgr := NewStreamManager(rc)
	rc.SetStreamManager(mgr)
	st, err := mgr.OpenStream("exec")
	if err != nil {
		t.Fatal(err)
	}
	h := &ExecHandle{streamMgr: mgr, execStream: st, relay: rc,
		frameCh: make(chan frameMsg, 256), closedCh: make(chan struct{}), deadCh: make(chan struct{})}
	go h.readerLoop()

	_, _, _, _, err = h.Exec(t.Context(), "sleep 60", time.Minute)
	ended, ok := err.(*SessionEndedError)
	if !ok || ended.Reason != "device went offline" {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("handle Done not closed")
	}
}

func TestExecSkipsLateFramesOfAbandonedRequest(t *testing.T) {
	client, agent := newFakeStreamPair()
	h := newTestExecHandleViaOverride(client)
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() {
		first := agentReadRequest(t, agent)
		time.Sleep(300 * time.Millisecond)
		agentSend(t, agent, execResponse{ID: first.ID, Type: "stdout", Data: b64("late")})
		agentSend(t, agent, execResponse{ID: first.ID, Type: "result"})
		second := agentReadRequest(t, agent)
		agentSend(t, agent, execResponse{ID: second.ID, Type: "stdout", Data: b64("ok")})
		agentSend(t, agent, execResponse{ID: second.ID, Type: "result"})
	}()

	if _, _, _, _, err := h.Exec(ctx, "slow", time.Second); err == nil {
		t.Fatal("first Exec should time out")
	}
	out, _, code, _, err := h.Exec(context.Background(), "next", time.Second)
	if err != nil || out != "ok" || code != 0 {
		t.Fatalf("second Exec = %q %d %v", out, code, err)
	}
}

func TestConnectExecContextStopsWaitingForPairing(t *testing.T) {
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ReadMessage()
		conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": false}})
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	c := &Client{host: "ws" + strings.TrimPrefix(srv.URL, "http"), sslVerify: true, sessionID: "k"}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ConnectExecContext(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %v to give up", elapsed)
	}
}

func TestRelayAbruptDropRecordsReasonBeforeStreamsClose(t *testing.T) {
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.ReadMessage()
		conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": true}})
		conn.WriteJSON(map[string]any{"type": "paired"})
		time.Sleep(600 * time.Millisecond)
		conn.UnderlyingConn().Close()
	}))
	defer srv.Close()

	rc := connectPaired(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	mgr := NewStreamManager(rc)
	rc.SetStreamManager(mgr)
	st, err := mgr.OpenStream("exec")
	if err != nil {
		t.Fatal(err)
	}
	h := &ExecHandle{streamMgr: mgr, execStream: st, relay: rc,
		frameCh: make(chan frameMsg, 256), closedCh: make(chan struct{}), deadCh: make(chan struct{})}
	go h.readerLoop()

	_, _, _, _, err = h.Exec(context.Background(), "x", time.Minute)
	ended, ok := err.(*SessionEndedError)
	if !ok || ended.Reason != "connection to the relay was lost" {
		t.Fatalf("err = %v", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestReportEndSilentOnUserQuitWhenRunBeforeClose(t *testing.T) {
	url := fakeRelay(t, func(c *websocket.Conn) { time.Sleep(2 * time.Second) })
	rc := connectPaired(t, url)
	c := &Client{}

	if out := captureStderr(t, func() { c.reportEnd(rc) }); out != "" {
		t.Errorf("reportEnd before Close printed %q", out)
	}
	rc.Close()
	c2 := &Client{}
	if out := captureStderr(t, func() { c2.reportEnd(rc) }); out == "" {
		t.Error("after Close the relay reports an end: the defer order in Connect matters")
	}
}

func TestCloseReasonStripsControlCharacters(t *testing.T) {
	got := closeReasonText(websocket.CloseNormalClosure, "bye\x1b[2J\x1b]0;pwn\x07\u009b31m\nnext\tline")
	if strings.ContainsAny(got, "\x1b\x07\n\t\u009b") {
		t.Errorf("control characters survived: %q", got)
	}
	if long := closeReasonText(websocket.CloseNormalClosure, strings.Repeat("a", 1000)); len([]rune(long)) != maxReasonRunes {
		t.Errorf("reason not bounded: %d", len(long))
	}
}

func TestAbandonedEvictsOldestFirst(t *testing.T) {
	client, _ := newFakeStreamPair()
	h := newTestExecHandleViaOverride(client)
	defer h.Close()
	for i := 0; i < maxAbandonedRequests+1; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.Exec(ctx, "x", time.Second)
	}
	h.abandonedMu.Lock()
	defer h.abandonedMu.Unlock()
	if len(h.abandoned) != maxAbandonedRequests || len(h.abandonedOrder) != maxAbandonedRequests {
		t.Errorf("abandoned = %d, order = %d, want %d", len(h.abandoned), len(h.abandonedOrder), maxAbandonedRequests)
	}
}
