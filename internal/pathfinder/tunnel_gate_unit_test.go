package pathfinder

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func gateRequest(t *testing.T, host string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/index.php", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RequestURI = "/index.php"
	req.Header.Set("Cookie", "PHPSESSID=x; ndcli-tunnel-8443="+testCookie)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

const (
	testToken  = "t0ken"
	testCookie = "c00kie"
)

func testSecret() *tunnelSecret { return &tunnelSecret{token: testToken, cookie: testCookie} }

func pageLoad(site string) map[string]string {
	return map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
}

// A page opened from elsewhere (a link in a chat or on another site, a
// browser extension) arrives cross-site, and so do the redirects after it.
// The tunnel answers it with its own page, whatever the target; everything
// else from another site is refused.
func TestRequestGateTurnsAPageLoadFromElsewhereToTheFrontPage(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
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
		{"the tunnel's own page moving on", "GET", rootTarget, pageLoad("same-origin"), 0, false},
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
	gate := newRequestGate(8443, testSecret())
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
	gate := newRequestGate(8443, testSecret())
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
	gate := newRequestGate(8443, testSecret())
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
// headers passes these checks, and is left to the cookie.
func TestRequestGateJudgesABrowserWithoutFetchMetadataByItsReferer(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
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

// The moving page's own move, should a browser send it as cross-site, gets a
// page that only links to the front page: the tab does not move on forever.
func TestRequestGateDoesNotMoveOnTwice(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	for _, headers := range []map[string]string{pageLoad("cross-site"), {"Referer": "https://evil.example/"}} {
		req := gateRequest(t, "localhost:8443", headers)
		req.RequestURI = rootTarget
		r := gate.check(req)
		if r == nil || r.status != http.StatusOK || !r.toRoot || !r.linkOnly {
			t.Errorf("%v: got %+v, want the page that only links", headers, r)
		}
		var b strings.Builder
		if err := writeRefusal(&b, r); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "refresh") || !strings.Contains(b.String(), `<a href="/">`) {
			t.Errorf("page %q, want a link and no refresh", b.String())
		}
	}
}

// A browser sends Origin or Referer with a request other than GET or HEAD; one that
// carries none of Origin, Referer and fetch metadata is refused.
func TestRequestGateRefusesAnUnsafeMethodThatSaysNothingOfWhereItComesFrom(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		req := gateRequest(t, "localhost:8443", nil)
		req.Method = method
		if r := gate.check(req); r == nil || r.status != http.StatusForbidden || !strings.Contains(r.body, "Origin: http://127.0.0.1:8443") {
			t.Errorf("%s: got %+v, want 403 naming the Origin to send", method, r)
		}
	}
	for _, headers := range []map[string]string{
		{"Origin": "http://localhost:8443"},
		{"Referer": "http://localhost:8443/ui/core/dashboard"},
		{"Sec-Fetch-Site": "same-origin"},
	} {
		req := gateRequest(t, "localhost:8443", headers)
		req.Method = http.MethodPost
		if r := gate.check(req); r != nil {
			t.Errorf("POST with %v: refused with %d", headers, r.status)
		}
	}
}

func TestRefusalsAreNotStored(t *testing.T) {
	for _, r := range []*gateRefusal{
		{status: http.StatusForbidden, body: "no"},
		{status: http.StatusOK, toRoot: true, body: "moving"},
		{status: http.StatusOK, toRoot: true, linkOnly: true, body: "link", head: true},
	} {
		var b strings.Builder
		if err := writeRefusal(&b, r); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(b.String())), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.Header.Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
			t.Errorf("%+v: Cache-Control %q, want no-store", r, got)
		}
	}
}

// Before fetch metadata, a form another site posts carries only its Referer.
func TestRequestGateRefusesAPostFromElsewhereByItsReferer(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	req := gateRequest(t, "localhost:8443", map[string]string{"Referer": "https://evil.example/form"})
	req.Method = http.MethodPost
	if r := gate.check(req); r == nil || r.status != http.StatusForbidden {
		t.Fatalf("got %+v, want 403", r)
	}
}

// An absolute-form target names the host, as the device's parser reads it,
// and the Host header beside it, which reaches the device too, must be the
// tunnel's as well.
func TestRequestGateReadsTheHostOfAnAbsoluteTarget(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"GET http://localhost:8443/ HTTP/1.1\r\nHost: 127.0.0.1:8443\r\n\r\n", true},
		{"GET http://localhost:8443/ HTTP/1.1\r\nHost: evil.example\r\n\r\n", false},
		{"GET http://localhost:8443/ HTTP/1.1\r\n\r\n", false},
		{"GET http://evil.example/ HTTP/1.1\r\nHost: localhost:8443\r\n\r\n", false},
		{"GET / HTTP/1.1\r\nHost: localhost:8443\r\n\r\n", true},
	} {
		req, err := parseRequestHead([]byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", "ndcli-tunnel-8443="+testCookie)
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

func linkRequest(t *testing.T, query string, headers map[string]string) *http.Request {
	t.Helper()
	req := gateRequest(t, "localhost:8443", headers)
	req.Header.Del("Cookie")
	req.RequestURI = "/?" + query
	req.URL.Path, req.URL.RawQuery = "/", query
	return req
}

// The link's first GET sets the cookie, which is not the token, and moves on
// to the front page, whatever site it was opened from; the link then opens
// nothing new, except in the browser that holds the cookie.
func TestRequestGateExchangesTheLinkOnce(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	r := gate.check(linkRequest(t, "ndcli="+testToken, pageLoad("cross-site")))
	if r == nil || r.status != http.StatusOK || !r.toRoot || r.linkOnly {
		t.Fatalf("first GET of the link: %+v, want the moving page", r)
	}
	if want := "ndcli-tunnel-8443=" + testCookie + "; Path=/; HttpOnly; SameSite=Strict"; r.setCookie != want {
		t.Fatalf("Set-Cookie %q, want %q", r.setCookie, want)
	}

	again := gate.check(linkRequest(t, "ndcli="+testToken, pageLoad("none")))
	if again == nil || again.status != http.StatusForbidden {
		t.Fatalf("the link again, without the cookie: %+v, want 403", again)
	}
	withCookie := linkRequest(t, "ndcli="+testToken, pageLoad("none"))
	withCookie.Header.Set("Cookie", "ndcli-tunnel-8443="+testCookie)
	if r := gate.check(withCookie); r == nil || !r.toRoot || r.setCookie != "" {
		t.Fatalf("the link again, with the cookie: %+v, want the moving page and no new cookie", r)
	}
	old := linkRequest(t, "ndcli=an-earlier-sessions-token", nil)
	old.Header.Set("Cookie", "ndcli-tunnel-8443="+testCookie)
	if r := gate.check(old); r == nil || !r.toRoot || r.setCookie != "" {
		t.Fatalf("another link, with the cookie: %+v, want the moving page", r)
	}
	spent := linkRequest(t, "ndcli="+testToken, nil)
	spent.Header.Set("Cookie", "ndcli-tunnel-8443="+testToken)
	if r := gate.check(spent); r == nil || r.status != http.StatusForbidden {
		t.Fatalf("the spent token as the cookie: %+v, want 403", r)
	}
}

// A wrong token, a second token or another method spends nothing.
func TestRequestGateRefusesALinkThatIsNotTheSessions(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	for _, tc := range []struct {
		name, method, query string
		status              int
	}{
		{"wrong token", "GET", "ndcli=guess", http.StatusForbidden},
		{"no token", "GET", "ndcli=", http.StatusForbidden},
		{"two tokens", "GET", "ndcli=" + testToken + "&ndcli=" + testToken, http.StatusForbidden},
		{"POST", "POST", "ndcli=" + testToken, http.StatusMethodNotAllowed},
		{"HEAD", "HEAD", "ndcli=" + testToken, http.StatusMethodNotAllowed},
	} {
		req := linkRequest(t, tc.query, nil)
		req.Method = tc.method
		if r := gate.check(req); r == nil || r.status != tc.status || r.setCookie != "" {
			t.Errorf("%s: %+v, want %d and no cookie", tc.name, r, tc.status)
		}
	}
	page := gateRequest(t, "localhost:8443", nil)
	page.Header.Del("Cookie")
	page.RequestURI, page.URL.RawQuery = "/index.php?ndcli="+testToken, "ndcli="+testToken
	if r := gate.check(page); r == nil || r.status != http.StatusForbidden || r.setCookie != "" {
		t.Errorf("the token on another page: %+v, want 403 and no cookie", r)
	}
	if r := gate.check(linkRequest(t, "ndcli="+testToken, nil)); r == nil || r.setCookie == "" {
		t.Fatalf("the link after the refusals: %+v, want the cookie", r)
	}
}

// A request the gate would forward needs the cookie: one from a browser that
// says nothing of where it comes from no longer passes without it, nor does
// another session's cookie. The tunnel's own pages need none.
func TestRequestGateForwardsOnlyWithTheCookie(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	for name, cookie := range map[string]string{
		"no cookie":                   "",
		"an earlier session's cookie": "ndcli-tunnel-8443=stale",
		"another port's cookie":       "ndcli-tunnel-9000=" + testCookie,
		"the token as the cookie":     "ndcli-tunnel-8443=" + testToken,
	} {
		for _, headers := range []map[string]string{nil, {"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8443"}} {
			req := gateRequest(t, "localhost:8443", headers)
			req.Header.Set("Cookie", cookie)
			if r := gate.check(req); r == nil || r.status != http.StatusForbidden || !strings.Contains(r.body, "ndcli device connect") {
				t.Errorf("%s, %v: %+v, want 403 naming the link", name, headers, r)
			}
		}
	}
	req := gateRequest(t, "localhost:8443", nil)
	req.Header.Set("Cookie", "ndcli-tunnel-8443=stale; ndcli-tunnel-8443="+testCookie)
	if r := gate.check(req); r != nil {
		t.Errorf("a stale cookie beside the session's: refused with %d", r.status)
	}
	moving := gateRequest(t, "localhost:8443", pageLoad("cross-site"))
	moving.Header.Del("Cookie")
	if r := gate.check(moving); r == nil || !r.toRoot {
		t.Errorf("a page load from elsewhere without the cookie: %+v, want the moving page", r)
	}
}

func TestTheLinksAnswerSetsTheCookieAndSendsNoReferrer(t *testing.T) {
	var b strings.Builder
	r := &gateRefusal{status: http.StatusOK, toRoot: true, body: "Opening the WebAdmin.", setCookie: "ndcli-tunnel-8443=c; Path=/; HttpOnly; SameSite=Strict"}
	if err := writeRefusal(&b, r); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(b.String())), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Values("Set-Cookie"); len(got) != 1 || got[0] != r.setCookie {
		t.Errorf("Set-Cookie %q", got)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy %q, want no-referrer", got)
	}
}

func TestEachTunnelHasItsOwnSecret(t *testing.T) {
	a, b := NewTunnel(0, "webadmin", nil), NewTunnel(0, "webadmin", nil)
	if a.secret.token == b.secret.token || a.secret.token == a.secret.cookie || len(a.secret.token) < 26 {
		t.Fatalf("secrets %+v and %+v", a.secret, b.secret)
	}
	a.actualPort = 54321
	if got, want := a.URL(), "http://127.0.0.1:54321/?ndcli="+a.secret.token; got != want {
		t.Fatalf("URL %q, want %q", got, want)
	}
}

// Of the browsers that open the link at once, only one gets the cookie.
func TestTheLinkIsSpentOnce(t *testing.T) {
	gate := newRequestGate(8443, testSecret())
	var wg sync.WaitGroup
	var cookies atomic.Int32
	for range 16 {
		req := linkRequest(t, "ndcli="+testToken, nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := gate.check(req); r != nil && r.setCookie != "" {
				cookies.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := cookies.Load(); n != 1 {
		t.Fatalf("%d answers set the cookie, want 1", n)
	}
}
