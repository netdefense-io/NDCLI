//go:build !windows

// The socket tests rely on half-close (CloseWrite) and drain behaviour that
// differs on Windows; the gate itself is tested on every platform in
// tunnel_gate_unit_test.go.

package pathfinder

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gateTunnel is a tunnel on a link that keeps what the tunnel sends, with the
// device's side played through the stream manager.
type gateTunnel struct {
	tunnel *Tunnel
	mgr    *StreamManager
	link   *captureLink
}

func startGateTunnel(t *testing.T) *gateTunnel {
	t.Helper()
	link := &captureLink{}
	mgr := NewStreamManager(link)
	tunnel := NewTunnel(0, "webadmin", mgr)
	tunnel.secret = testSecret()
	if err := tunnel.Start(); err != nil {
		t.Fatalf("start tunnel: %v", err)
	}
	t.Cleanup(func() { _ = tunnel.Stop() })
	return &gateTunnel{tunnel: tunnel, mgr: mgr, link: link}
}

func (g *gateTunnel) dial(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", g.tunnel.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return conn
}

func (g *gateTunnel) host() string { return fmt.Sprintf("localhost:%d", g.tunnel.Port()) }

func (g *gateTunnel) reply(id uint32, data string) {
	g.mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: id, Data: []byte(data)}))
}

func (g *gateTunnel) frames(typ uint8) []*Frame {
	g.link.mu.Lock()
	defer g.link.mu.Unlock()
	var out []*Frame
	for _, f := range g.link.frames {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

func (g *gateTunnel) sent() string {
	var b strings.Builder
	for _, f := range g.frames(FrameTypeData) {
		b.Write(f.Data)
	}
	return b.String()
}

// waitSent waits until the device has been sent want, and fails on anything
// else.
func (g *gateTunnel) waitSent(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := g.sent()
		if got == want {
			return
		}
		if len(got) >= len(want) {
			t.Fatalf("the device was sent %q, want %q", got, want)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the device was sent %q, want %q", g.sent(), want)
}

func readResponse(t *testing.T, br *bufio.Reader) (*http.Response, string) {
	t.Helper()
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func get(host string, extra ...string) string {
	return "GET /index.php HTTP/1.1\r\nHost: " + host + "\r\n" + cookieLine(host) + strings.Join(extra, "") + "\r\n"
}

// cookieLine is the cookie the tunnel's link set, as the browser sends it.
func cookieLine(host string) string {
	_, port, _ := net.SplitHostPort(host)
	return "Cookie: ndcli-tunnel-" + port + "=" + testCookie + "\r\n"
}

const okReply = "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"

// A refused first request is answered by the tunnel itself: no stream is
// opened, so the device is never asked.
func TestTunnelRefusesTheFirstRequestWithoutTheDevice(t *testing.T) {
	cases := []struct {
		name    string
		request func(host string) string
		status  int
	}{
		{"rebound name", func(string) string { return get("rebound.example:1234") }, http.StatusMisdirectedRequest},
		{"absolute target on another host", func(h string) string {
			return "GET http://evil.example/ HTTP/1.1\r\nHost: " + h + "\r\n\r\n"
		}, http.StatusMisdirectedRequest},
		{"cross-site form post", func(h string) string {
			return "POST /system_general.php HTTP/1.1\r\nHost: " + h + "\r\nOrigin: https://evil.example\r\nSec-Fetch-Site: cross-site\r\nContent-Length: 7\r\n\r\na=b&c=d"
		}, http.StatusForbidden},
		{"image from another localhost port", func(h string) string { return get(h, "Sec-Fetch-Site: same-site\r\n") }, http.StatusForbidden},
		{"page load from another site", func(h string) string {
			return get(h, "Sec-Fetch-Site: cross-site\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-Dest: document\r\n")
		}, http.StatusOK},
		{"two Host headers", func(h string) string { return get(h, "Host: evil.example\r\n") }, http.StatusBadRequest},
		{"length and chunks", func(h string) string {
			return "POST / HTTP/1.1\r\nHost: " + h + "\r\nContent-Length: 4\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"
		}, http.StatusBadRequest},
		{"not HTTP", func(string) string { return "\x16\x03\x01\x00\xa5\x01\x00\x00\xa1\x03\x03\r\n\r\n" }, http.StatusBadRequest},
		{"head too large", func(h string) string {
			return get(h, "X-Big: "+strings.Repeat("a", maxRequestHead)+"\r\n")
		}, http.StatusRequestHeaderFieldsTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := startGateTunnel(t)
			conn := g.dial(t)
			go func() { _, _ = io.WriteString(conn, tc.request(g.host())) }()

			resp, body := readResponse(t, bufio.NewReader(conn))
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d (%q), want %d", resp.StatusCode, body, tc.status)
			}
			if !resp.Close {
				t.Error("the refusal does not close the connection")
			}
			if n := len(g.frames(FrameTypeOpen)); n != 0 {
				t.Errorf("%d streams opened, want none", n)
			}
		})
	}
}

func TestTunnelForwardsTheWebAdminsRequestsUnchanged(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	post := "POST /api/core/firmware/status HTTP/1.1\r\nHost: " + g.host() +
		"\r\nOrigin: http://" + g.host() + "\r\nSec-Fetch-Site: same-origin\r\nCookie: PHPSESSID=x; ndcli-tunnel-" + strconv.Itoa(g.tunnel.Port()) + "=" + testCookie + "\r\nContent-Length: 11\r\n\r\n{\"a\":\"b c\"}"
	if _, err := io.WriteString(conn, post); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, post)
	g.reply(1, okReply)
	if resp, body := readResponse(t, br); resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}

	second := get("127.0.0.1:"+strings.Split(g.host(), ":")[1], "Sec-Fetch-Site: none\r\n")
	if _, err := io.WriteString(conn, second); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, post+second)
	g.reply(1, okReply)
	if resp, _ := readResponse(t, br); resp.StatusCode != 200 {
		t.Fatalf("second request: %d", resp.StatusCode)
	}
	if n := len(g.frames(FrameTypeOpen)); n != 1 {
		t.Errorf("%d streams opened for one connection, want 1", n)
	}
}

// The answer to a page load from another site moves on to the tunnel's front
// page only, never to the target the link named.
func TestTunnelAnswersAPageLoadFromElsewhereWithTheFrontPage(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	go func() {
		_, _ = io.WriteString(conn, "GET /diag_backup.php?download=1 HTTP/1.1\r\nHost: "+g.host()+
			"\r\nSec-Fetch-Site: cross-site\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-Dest: document\r\n\r\n")
	}()
	resp, body := readResponse(t, bufio.NewReader(conn))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("got %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", resp.Header.Get("Cache-Control"))
	}
	if !strings.Contains(body, `content="0; url=/?"`) || !strings.Contains(body, `<a href="/">`) || strings.Contains(body, "diag_backup") {
		t.Errorf("body %q, want it to move on to the front page only", body)
	}
	if n := len(g.frames(FrameTypeOpen)); n != 0 {
		t.Errorf("%d streams opened, want none", n)
	}
}

// A HEAD request gets the tunnel's answer without a body.
func TestTunnelAnswersHeadWithoutABody(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	go func() {
		_, _ = io.WriteString(conn, "HEAD / HTTP/1.1\r\nHost: "+g.host()+
			"\r\nSec-Fetch-Site: cross-site\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-Dest: document\r\n\r\n")
	}()
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodHead})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("got %v %v", resp, err)
	}
	if rest, _ := io.ReadAll(br); len(rest) != 0 {
		t.Errorf("a body followed the answer to HEAD: %q", rest)
	}
}

// Every request of a keep-alive connection is checked, not only the first.
func TestTunnelChecksEveryRequestOfAConnection(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	first := get(g.host(), "Sec-Fetch-Site: same-origin\r\n")
	if _, err := io.WriteString(conn, first); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, first)
	g.reply(1, okReply)
	if resp, _ := readResponse(t, br); resp.StatusCode != 200 {
		t.Fatalf("first request: %d", resp.StatusCode)
	}

	if _, err := io.WriteString(conn, get(g.host(), "Sec-Fetch-Site: cross-site\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, _ := readResponse(t, br)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("second request: %d, want 403", resp.StatusCode)
	}
	if got := g.sent(); got != first {
		t.Errorf("the device was sent %q, want only the first request", got)
	}
	if n := len(g.frames(FrameTypeClose)); n != 1 {
		t.Errorf("%d CLOSE frames, want the stream closed", n)
	}
}

// A chunked body is re-chunked, so a request hidden in it reaches the device
// as body bytes and the one after it is still checked.
func TestTunnelFramesAChunkedBodyAsTheDeviceWill(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	hidden := "GET / HTTP/1.1\r\nHost: evil.example\r\n\r\n"
	head := "POST /upload HTTP/1.1\r\nHost: " + g.host() + "\r\n" + cookieLine(g.host()) + "Origin: http://" + g.host() + "\r\nTransfer-Encoding: chunked\r\n\r\n"
	request := head + fmt.Sprintf("%x;ext=1\r\n%s\r\n0\r\nX-Trailer: 1\r\n\r\n", len(hidden), hidden)
	if _, err := io.WriteString(conn, request+get("evil.example")); err != nil {
		t.Fatal(err)
	}
	resp, _ := readResponse(t, br)
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("the request after the body: %d, want 421", resp.StatusCode)
	}

	forwarded := bufio.NewReader(strings.NewReader(g.sent()))
	req, err := http.ReadRequest(forwarded)
	if err != nil {
		t.Fatalf("the device cannot read what it was sent: %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil || string(body) != hidden {
		t.Fatalf("body %q (%v), want %q", body, err, hidden)
	}
	if rest, _ := io.ReadAll(forwarded); len(rest) != 0 {
		t.Errorf("the device was sent more after the request: %q", rest)
	}
}

// An upgrade is answered by the tunnel: the device carries none.
func TestTunnelRefusesAnUpgrade(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	go func() {
		_, _ = io.WriteString(conn, get(g.host(), "Origin: http://"+g.host()+"\r\nSec-Fetch-Site: same-origin\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"))
	}()
	resp, _ := readResponse(t, bufio.NewReader(conn))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501", resp.StatusCode)
	}
	if n := len(g.frames(FrameTypeOpen)); n != 0 {
		t.Errorf("%d streams opened, want none", n)
	}
}

// A long poll: the device's reply reaches the browser as it arrives, while
// the connection stays open.
func TestTunnelStreamsALongPollReply(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	request := get(g.host(), "Accept: text/event-stream\r\nSec-Fetch-Site: same-origin\r\n")
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, request)
	g.reply(1, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("reply head: %v %v", resp, err)
	}
	for i := range 3 {
		event := fmt.Sprintf("data: %d\n\n", i)
		g.reply(1, fmt.Sprintf("%x\r\n%s\r\n", len(event), event))
		got := make([]byte, len(event))
		if _, err := io.ReadFull(resp.Body, got); err != nil || string(got) != event {
			t.Fatalf("event %d: %q %v", i, got, err)
		}
	}
	if !bytes.Equal([]byte(g.sent()), []byte(request)) {
		t.Errorf("the device was sent %q", g.sent())
	}
}

// curl sends Expect: 100-continue with a larger body and waits for the
// interim answer: the device's 100 reaches the client, and the body the device
// after it.
func TestTunnelCarriesExpectContinue(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	head := "POST /api/core/backup/upload HTTP/1.1\r\nHost: " + g.host() + "\r\n" + cookieLine(g.host()) + "Origin: http://" + g.host() +
		"\r\nContent-Length: 5\r\nExpect: 100-continue\r\n\r\n"
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, head)
	g.reply(1, "HTTP/1.1 100 Continue\r\n\r\n")
	line, err := br.ReadString('\n')
	if err != nil || line != "HTTP/1.1 100 Continue\r\n" {
		t.Fatalf("interim answer %q, %v", line, err)
	}
	if blank, err := br.ReadString('\n'); err != nil || blank != "\r\n" {
		t.Fatalf("after the interim answer %q, %v", blank, err)
	}
	if _, err := io.WriteString(conn, "hello"); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, head+"hello")
	g.reply(1, okReply)
	if resp, body := readResponse(t, br); resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
}

// A chunked body's trailer is dropped, and the next request of the
// connection is read from where the body ended.
func TestTunnelDropsATrailerAndReadsTheNextRequest(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	br := bufio.NewReader(conn)

	head := "POST /upload HTTP/1.1\r\nHost: " + g.host() + "\r\n" + cookieLine(g.host()) + "Origin: http://" + g.host() + "\r\nTransfer-Encoding: chunked\r\n\r\n"
	next := get(g.host(), "Sec-Fetch-Site: same-origin\r\n")
	if _, err := io.WriteString(conn, head+"3\r\nabc\r\n0\r\nX-Checksum: 1\r\nX-Other: 2\r\n\r\n"+next); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, head+"3\r\nabc\r\n0\r\n\r\n"+next)
	for range 2 {
		g.reply(1, okReply)
		if resp, _ := readResponse(t, br); resp.StatusCode != 200 {
			t.Fatalf("got %d", resp.StatusCode)
		}
	}
}

// The tunnel answers the moving page's own move, should it arrive
// cross-site, with a page that does not move on again.
func TestTunnelDoesNotMoveOnTwice(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	go func() {
		_, _ = io.WriteString(conn, "GET "+rootTarget+" HTTP/1.1\r\nHost: "+g.host()+
			"\r\nSec-Fetch-Site: cross-site\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-Dest: document\r\n\r\n")
	}()
	resp, body := readResponse(t, bufio.NewReader(conn))
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "refresh") || !strings.Contains(body, `<a href="/">`) {
		t.Fatalf("got %d %q, want a page that only links to the front page", resp.StatusCode, body)
	}
}

// A refusal sent to a client that has stopped reading, while the device's
// reply is still on its way, does not hold the connection: the tunnel lets go
// of it within its write deadlines.
func TestTunnelLetsGoOfAClientThatStopsReading(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)

	first := get(g.host(), "Sec-Fetch-Site: same-origin\r\n")
	if _, err := io.WriteString(conn, first); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, first)
	chunk := strings.Repeat("x", maxStreamFrameData)
	g.reply(1, fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", 512*len(chunk)))
	for range 512 {
		g.reply(1, chunk)
	}
	if _, err := io.WriteString(conn, get(g.host(), "Sec-Fetch-Site: cross-site\r\n")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(4*refusalDrain + 2*time.Second)
	for {
		g.tunnel.connsMu.Lock()
		open := len(g.tunnel.conns)
		g.tunnel.connsMu.Unlock()
		if open == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tunnel still holds the connection after %v", 4*refusalDrain+2*time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The link is answered by the tunnel, never the device, and leaves the token
// out of every later request: the browser comes back to "/?" with the cookie.
func TestTunnelExchangesTheLinkWithoutTheDevice(t *testing.T) {
	g := startGateTunnel(t)
	conn := g.dial(t)
	go func() {
		_, _ = io.WriteString(conn, "GET /?ndcli="+testToken+" HTTP/1.1\r\nHost: "+g.host()+"\r\nSec-Fetch-Site: none\r\n\r\n")
	}()
	resp, body := readResponse(t, bufio.NewReader(conn))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `url=/?"`) {
		t.Fatalf("got %d %q, want the moving page", resp.StatusCode, body)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Value != testCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies %+v", cookies)
	}
	if n := len(g.frames(FrameTypeOpen)); n != 0 {
		t.Errorf("%d streams opened, want none", n)
	}

	next := g.dial(t)
	move := "GET /? HTTP/1.1\r\nHost: " + g.host() + "\r\n" + cookieLine(g.host()) + "Sec-Fetch-Site: same-origin\r\n\r\n"
	if _, err := io.WriteString(next, move); err != nil {
		t.Fatal(err)
	}
	g.waitSent(t, move)
}
