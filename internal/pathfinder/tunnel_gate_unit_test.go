package pathfinder

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"testing"
)

func gateRequest(t *testing.T, host string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/index.php", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RequestURI = "/index.php"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func pageLoad(site string) map[string]string {
	return map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
}

// A page opened from elsewhere (a link in a chat or on another site, a
// browser extension) arrives cross-site, and so do the redirects after it.
// The tunnel answers it with its own page, whatever the target; everything
// else from another site is refused.
func TestRequestGateTurnsAPageLoadFromElsewhereToTheFrontPage(t *testing.T) {
	gate := newRequestGate(8443)
	cases := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
		want    int
		toRoot  bool
	}{
		{"front page from a link", "GET", "/", pageLoad("cross-site"), http.StatusOK, true},
		{"redirect after it", "GET", "/ui/core/dashboard", pageLoad("cross-site"), http.StatusOK, true},
		{"from another localhost port", "GET", "/", pageLoad("same-site"), http.StatusOK, true},
		{"HEAD", "HEAD", "/", pageLoad("cross-site"), http.StatusOK, true},
		{"deep link with a query", "GET", "/?act=del&id=0", pageLoad("cross-site"), http.StatusOK, true},
		{"form posted from another site", "POST", "/", pageLoad("cross-site"), http.StatusForbidden, false},
		{"page in a frame", "GET", "/", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, http.StatusForbidden, false},
		{"image", "GET", "/", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image"}, http.StatusForbidden, false},
		{"fetch", "GET", "/", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty", "Origin": "https://evil.example"}, http.StatusForbidden, false},
		{"the tunnel's own page moving on", "GET", "/", pageLoad("same-origin"), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := gateRequest(t, "localhost:8443", tc.headers)
			req.Method, req.RequestURI = tc.method, tc.target
			r := gate.check(req)
			switch {
			case tc.want == 0 && r != nil:
				t.Fatalf("refused with %d: %s", r.status, r.body)
			case tc.want != 0 && (r == nil || r.status != tc.want || r.toRoot != tc.toRoot):
				t.Fatalf("got %+v, want status %d toRoot %v", r, tc.want, tc.toRoot)
			}
		})
	}
}

func TestRequestGateAllows(t *testing.T) {
	gate := newRequestGate(8443)
	cases := []struct {
		name    string
		host    string
		headers map[string]string
	}{
		{"localhost, no fetch metadata", "localhost:8443", nil},
		{"127.0.0.1", "127.0.0.1:8443", nil},
		{"host in another case", "LocalHost:8443", nil},
		{"address typed by the user", "localhost:8443", map[string]string{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate"}},
		{"the WebAdmin's own fetch", "localhost:8443", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8443"}},
		{"the other name of the tunnel", "localhost:8443", map[string]string{"Origin": "http://127.0.0.1:8443"}},
		{"a link followed within the tunnel, before fetch metadata", "localhost:8443", map[string]string{"Referer": "http://127.0.0.1:8443/ui/core/dashboard"}},
		{"its own fetch, before fetch metadata", "localhost:8443", map[string]string{"Origin": "http://localhost:8443", "Referer": "http://localhost:8443/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if r := gate.check(gateRequest(t, tc.host, tc.headers)); r != nil {
				t.Errorf("refused with %d: %s", r.status, r.body)
			}
		})
	}
}

func TestRequestGateRefuses(t *testing.T) {
	gate := newRequestGate(8443)
	cases := []struct {
		name    string
		host    string
		headers map[string]string
		status  int
	}{
		{"rebound name", "evil.example:8443", nil, http.StatusMisdirectedRequest},
		{"another port", "localhost:9000", nil, http.StatusMisdirectedRequest},
		{"no port", "localhost", nil, http.StatusMisdirectedRequest},
		{"no host", "", nil, http.StatusMisdirectedRequest},
		{"trailing dot", "localhost.:8443", nil, http.StatusMisdirectedRequest},
		{"cross-site", "localhost:8443", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"another localhost port", "localhost:8443", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"foreign origin", "localhost:8443", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"origin of another port", "localhost:8443", map[string]string{"Origin": "http://localhost:9000"}, http.StatusForbidden},
		{"https origin", "localhost:8443", map[string]string{"Origin": "https://localhost:8443"}, http.StatusForbidden},
		{"null origin", "localhost:8443", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"cross-site WebSocket", "localhost:8443", map[string]string{"Origin": "http://evil.example", "Upgrade": "websocket", "Connection": "Upgrade"}, http.StatusNotImplemented},
		{"WebSocket from the WebAdmin", "localhost:8443", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8443", "Upgrade": "websocket", "Connection": "keep-alive, Upgrade"}, http.StatusNotImplemented},
		{"own origin, but cross-site", "localhost:8443", map[string]string{"Origin": "http://localhost:8443", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := gateRequest(t, "localhost:8443", tc.headers)
			req.Host = tc.host
			r := gate.check(req)
			if r == nil || r.status != tc.status {
				t.Fatalf("got %+v, want status %d", r, tc.status)
			}
		})
	}
}

func TestRequestGateRefusesRepeatedFetchHeaders(t *testing.T) {
	gate := newRequestGate(8443)
	for _, name := range []string{"Origin", "Sec-Fetch-Site"} {
		req := gateRequest(t, "localhost:8443", nil)
		req.Header.Add(name, "same-origin")
		req.Header.Add(name, "same-origin")
		if r := gate.check(req); r == nil || r.status != http.StatusForbidden {
			t.Errorf("two %s headers: %+v, want 403", name, r)
		}
	}
}

// Before fetch metadata, a load from another site is known by its Referer and
// gets the page that moves on to "/". A request with none of the three
// headers passes: that is the gap such a browser leaves.
func TestRequestGateJudgesABrowserWithoutFetchMetadataByItsReferer(t *testing.T) {
	gate := newRequestGate(8443)
	for _, referer := range []string{"https://evil.example/page", "http://localhost:9000/", "not a url"} {
		r := gate.check(gateRequest(t, "localhost:8443", map[string]string{"Referer": referer}))
		if r == nil || r.status != http.StatusOK || !r.toRoot {
			t.Errorf("Referer %q: got %+v, want the moving page", referer, r)
		}
	}
	if r := gate.check(gateRequest(t, "localhost:8443", nil)); r != nil {
		t.Errorf("no Referer, Origin or fetch metadata: refused with %d, want it to pass", r.status)
	}
}

// Before fetch metadata, a form another site posts carries only its Referer.
func TestRequestGateRefusesAPostFromElsewhereByItsReferer(t *testing.T) {
	gate := newRequestGate(8443)
	req := gateRequest(t, "localhost:8443", map[string]string{"Referer": "https://evil.example/form"})
	req.Method = http.MethodPost
	if r := gate.check(req); r == nil || r.status != http.StatusForbidden {
		t.Fatalf("got %+v, want 403", r)
	}
}

// An absolute-form target names the host, as the device's parser reads it.
func TestRequestGateReadsTheHostOfAnAbsoluteTarget(t *testing.T) {
	gate := newRequestGate(8443)
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"GET http://localhost:8443/ HTTP/1.1\r\nHost: evil.example\r\n\r\n", true},
		{"GET http://evil.example/ HTTP/1.1\r\nHost: localhost:8443\r\n\r\n", false},
	} {
		req, err := parseRequestHead([]byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		if got := gate.check(req) == nil; got != tc.want {
			t.Errorf("%q: allowed %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestParseRequestHeadRefusesUnknownCodings(t *testing.T) {
	for _, raw := range []string{
		"POST / HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: gzip, chunked\r\n\r\n",
		"POST / HTTP/1.1\r\nHost: a\r\nContent-Length: 4\r\nTransfer-Encoding: chunked\r\n\r\n",
		"POST / HTTP/1.1\r\nHost: a\r\nContent-Length: 4\r\nContent-Length: 5\r\n\r\n",
	} {
		if _, err := parseRequestHead([]byte(raw)); err == nil {
			t.Errorf("%q was read", raw)
		}
	}
}

func TestReadRequestHead(t *testing.T) {
	long := "X-Long: " + strings.Repeat("a", 32<<10-len("X-Long: "))
	cases := []struct {
		name, in, want string
	}{
		{"one request", "GET / HTTP/1.1\r\nHost: a\r\n\r\nNEXT", "GET / HTTP/1.1\r\nHost: a\r\n\r\n"},
		{"bare line feeds", "GET / HTTP/1.1\nHost: a\n\nNEXT", "GET / HTTP/1.1\nHost: a\n\n"},
		{"empty lines before it", "\r\n\r\nGET / HTTP/1.1\r\n\r\n", "GET / HTTP/1.1\r\n\r\n"},
		{"a line that fills the buffer", "GET / HTTP/1.1\r\n" + long + "\r\nHost: a\r\n\r\n", "GET / HTTP/1.1\r\n" + long + "\r\nHost: a\r\n\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br := bufio.NewReaderSize(strings.NewReader(tc.in), 32<<10)
			head, err := readRequestHead(br)
			if err != nil || string(head) != tc.want {
				t.Fatalf("got %q, %v; want %q", head, err, tc.want)
			}
		})
	}
	if _, err := readRequestHead(bufio.NewReader(strings.NewReader(""))); err != io.EOF {
		t.Errorf("closed between requests: %v, want io.EOF", err)
	}
}

func TestSkipTrailer(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("X-A: 1\r\nX-B: 2\r\n\r\nNEXT"))
	if err := skipTrailer(br); err != nil {
		t.Fatal(err)
	}
	if rest, _ := io.ReadAll(br); string(rest) != "NEXT" {
		t.Errorf("left %q, want the next request", rest)
	}
	huge := bufio.NewReader(strings.NewReader("X-A: " + strings.Repeat("a", maxRequestHead) + "\r\n\r\n"))
	if err := skipTrailer(huge); err != errHeadTooLarge {
		t.Errorf("a trailer over the cap: %v, want errHeadTooLarge", err)
	}
}
