package pathfinder

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// maxRequestHead bounds the request line and headers of one request.
const maxRequestHead = 1 << 20

var errHeadTooLarge = errors.New("request head too large")

// gateRefusal is the tunnel's own answer to a request it does not forward.
type gateRefusal struct {
	status int
	body   string
	// head: answer a HEAD request, with no body.
	head bool
	// toRoot: a page that moves on to the front page.
	toRoot bool
	// linkOnly: the page links to the front page and does not move on.
	linkOnly bool
	// setCookie: a Set-Cookie value, on the answer that spends the link.
	setCookie string
}

// rootTarget is where the moving page sends the browser. It is not "/", so
// that the page's own move arriving as cross-site is known, and answered
// without another move.
const rootTarget = "/?"

// requestGate decides, for each request the browser sends through the
// tunnel, whether it reaches the device. The device signs every forwarded
// request in as the session's user, so a request the browser was made to send
// by another site, or sent to a name rebound to 127.0.0.1, must stop here.
type requestGate struct {
	hosts      []string
	origins    []string
	secret     *tunnelSecret
	cookieName string
}

// tokenParam is the query parameter of the link ndcli prints.
const tokenParam = "ndcli"

// tunnelSecret is what a browser must hold for the tunnel to forward its
// requests: the link ndcli prints carries the token, which the first GET of
// it exchanges for the cookie. Another local user on the same host, or a
// browser that does not say where a request comes from, holds neither. The
// cookie is not the token, so that the spent link is worth nothing.
type tunnelSecret struct {
	token  string
	cookie string
	spent  atomic.Bool
}

func newTunnelSecret() *tunnelSecret {
	return &tunnelSecret{token: rand.Text(), cookie: rand.Text()}
}

func newRequestGate(port int, secret *tunnelSecret) *requestGate {
	p := strconv.Itoa(port)
	// Cookies ignore the port: the name keeps two sessions' cookies apart.
	g := &requestGate{hosts: []string{"127.0.0.1:" + p, "localhost:" + p}, secret: secret, cookieName: "ndcli-tunnel-" + p}
	for _, h := range g.hosts {
		g.origins = append(g.origins, "http://"+h)
	}
	return g
}

// check returns nil for a request the tunnel forwards.
//
//   - Host must be the tunnel's own (127.0.0.1 or localhost with its port):
//     a DNS-rebound name carries the attacker's. With an absolute-form
//     target, both the target's host and the Host header must be.
//   - Sec-Fetch-Site, when sent, must be same-origin or none (the user typed
//     the address). Another port on localhost is same-site, and it is treated
//     like cross-site: it is another origin.
//   - A page opened from elsewhere (a link in another site or app, or a
//     browser extension) is a cross-site navigation, and so is every redirect
//     after it. It is answered with a page of the tunnel's own that moves on
//     to the front page, which the browser then loads as same-origin. Another
//     site never chooses the page the session opens. Should that move itself
//     arrive cross-site, the page only links to the front page, so the tab
//     does not move on forever.
//   - Origin, when sent, must be one of the tunnel's own; "null" is refused.
//   - A browser that sends neither (before fetch metadata) is judged by its
//     Referer: one from elsewhere is treated as cross-site. With none of the
//     three, a GET or HEAD passes and any other method is refused: a browser
//     sends Origin or Referer with one.
//   - An upgrade (a WebSocket) is refused: the device carries none.
//   - The link ndcli prints (see exchange) is the tunnel's own, whatever site
//     it was opened from, and a request it forwards must carry the cookie
//     that link set. The tunnel's own pages need none: a SameSite=Strict
//     cookie is not sent on a navigation from another site.
func (g *requestGate) check(req *http.Request) *gateRefusal {
	if !matchesAny(req.Host, g.hosts) || !strings.HasPrefix(req.RequestURI, "/") && !g.ownHostHeader(req) {
		return &gateRefusal{status: http.StatusMisdirectedRequest,
			body: fmt.Sprintf("This WebAdmin tunnel answers only at http://%s/.", g.hosts[0])}
	}
	if asksForUpgrade(req) {
		return &gateRefusal{status: http.StatusNotImplemented,
			body: "This WebAdmin tunnel does not carry WebSocket connections."}
	}
	if r, ok := g.exchange(req); ok {
		return r
	}
	if r := g.checkSource(req); r != nil {
		return r
	}
	if !g.hasCookie(req) {
		return &gateRefusal{status: http.StatusForbidden,
			body: fmt.Sprintf("This WebAdmin tunnel opens through the link that ndcli device connect printed (http://%s/?%s=...), in the browser that opened it first. Open that link, or connect again for a new one.", g.hosts[0], tokenParam)}
	}
	return nil
}

// exchange answers the link ndcli prints, http://127.0.0.1:<port>/?ndcli=<token>.
// Its first GET sets the cookie and moves on to the front page, so the token
// leaves the address bar. In the browser that holds the cookie, it or any
// other link of this port only moves on. ok is false for any other request.
func (g *requestGate) exchange(req *http.Request) (r *gateRefusal, ok bool) {
	tokens, ok := req.URL.Query()[tokenParam]
	if req.URL.Path != "/" || !ok {
		return nil, false
	}
	if req.Method != http.MethodGet {
		return &gateRefusal{status: http.StatusMethodNotAllowed, body: "This WebAdmin tunnel's link opens with a GET request."}, true
	}
	moveOn := &gateRefusal{status: http.StatusOK, toRoot: true, body: "Opening the WebAdmin."}
	if g.hasCookie(req) {
		return moveOn, true
	}
	if len(tokens) == 1 && subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(g.secret.token)) == 1 &&
		g.secret.spent.CompareAndSwap(false, true) {
		moveOn.setCookie = g.cookieName + "=" + g.secret.cookie + "; Path=/; HttpOnly; SameSite=Strict"
		return moveOn, true
	}
	return &gateRefusal{status: http.StatusForbidden,
		body: "This WebAdmin tunnel's link opens one browser, once, and this link was used already or is not this session's. Open it in the browser that opened it first, or run ndcli device connect again for a new link."}, true
}

// hasCookie reports whether req carries the cookie the link set. A cookie of
// an earlier session on the same port has the same name and another value.
// The cookie stays in the head forwarded to the device, which gets every
// head byte for byte.
func (g *requestGate) hasCookie(req *http.Request) bool {
	for _, c := range req.Cookies() {
		if c.Name == g.cookieName && subtle.ConstantTimeCompare([]byte(c.Value), []byte(g.secret.cookie)) == 1 {
			return true
		}
	}
	return false
}

// checkSource returns nil for a request that comes from the tunnel's own
// pages or that the user typed.
func (g *requestGate) checkSource(req *http.Request) *gateRefusal {
	crossSite := &gateRefusal{status: http.StatusForbidden,
		body: "This WebAdmin tunnel refuses requests that another site makes. Open the tunnel's address directly."}
	toRoot := &gateRefusal{status: http.StatusOK, toRoot: true,
		body: "Opening the WebAdmin. This tunnel opens its front page when a link from elsewhere leads here."}
	if req.RequestURI == rootTarget {
		toRoot = &gateRefusal{status: http.StatusOK, toRoot: true, linkOnly: true,
			body: "This tunnel opens its front page when a link from elsewhere leads here, and this browser did not open it."}
	}
	if origin := req.Header.Values("Origin"); len(origin) > 1 ||
		len(origin) == 1 && !matchesAny(origin[0], g.origins) {
		return crossSite
	}
	site := req.Header.Values("Sec-Fetch-Site")
	if len(site) > 1 {
		return crossSite
	}
	if len(site) == 0 {
		referer := req.Header.Values("Referer")
		safe := req.Method == http.MethodGet || req.Method == http.MethodHead
		switch {
		case len(req.Header.Values("Origin")) == 1:
			return nil
		case len(referer) == 0 && !safe:
			return &gateRefusal{status: http.StatusForbidden,
				body: fmt.Sprintf("This WebAdmin tunnel refuses a %s request that does not say where it comes from. Send it with the header Origin: %s.", req.Method, g.origins[0])}
		case len(referer) == 0:
			return nil
		case len(referer) == 1 && g.ownURL(referer[0]):
			return nil
		case safe:
			return toRoot
		}
		return crossSite
	}
	if matchesAny(site[0], []string{"same-origin", "none"}) {
		return nil
	}
	if !isPageLoad(req) {
		return crossSite
	}
	return toRoot
}

// ownHostHeader reports whether the Host header, which an absolute-form
// target leaves aside, is the tunnel's own. The head reaches the device with
// it, and what serves the WebAdmin behind the device may read it.
func (g *requestGate) ownHostHeader(req *http.Request) bool {
	host := req.Header.Values("Host")
	return len(host) == 1 && matchesAny(host[0], g.hosts)
}

// ownURL reports whether a Referer is a page of the tunnel itself.
func (g *requestGate) ownURL(ref string) bool {
	u, err := url.Parse(ref)
	return err == nil && u.Scheme == "http" && matchesAny(u.Host, g.hosts)
}

// isPageLoad reports whether req loads a top-level page, as following a link
// does.
func isPageLoad(req *http.Request) bool {
	return (req.Method == http.MethodGet || req.Method == http.MethodHead) &&
		matchesAny(req.Header.Get("Sec-Fetch-Mode"), []string{"navigate"}) &&
		matchesAny(req.Header.Get("Sec-Fetch-Dest"), []string{"document"}) &&
		len(req.Header.Values("Sec-Fetch-Mode")) == 1 && len(req.Header.Values("Sec-Fetch-Dest")) == 1
}

func matchesAny(v string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return true
		}
	}
	return false
}

// readRequestHead reads one request head, through the empty line that ends
// it, skipping empty lines before it as Go's server does; io.EOF means the
// client closed the connection between requests.
func readRequestHead(br *bufio.Reader) ([]byte, error) {
	var head []byte
	lineStart := true
	read := 0
	for {
		line, err := br.ReadSlice('\n')
		read += len(line)
		if read > maxRequestHead {
			return nil, errHeadTooLarge
		}
		if len(head) == 0 && err == nil && isLineEnd(line) {
			continue
		}
		head = append(head, line...)
		switch {
		case err == bufio.ErrBufferFull:
			lineStart = false
			continue
		case err == io.EOF && len(head) == 0:
			return nil, io.EOF
		case err != nil:
			return nil, err
		}
		if lineStart && isLineEnd(line) {
			return head, nil
		}
		lineStart = true
	}
}

func isLineEnd(line []byte) bool {
	return bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n"))
}

// parseRequestHead parses a head the way the device will, since the device
// reads the same bytes with the same parser. A body is framed by
// Content-Length or by chunks, never both. The parser drops the Host header;
// for a target that is not a path, which takes the host from elsewhere, it is
// put back for the gate.
func parseRequestHead(head []byte) (*http.Request, error) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(head)))
	if err != nil {
		return nil, err
	}
	originForm := strings.HasPrefix(req.RequestURI, "/")
	if len(req.TransferEncoding) == 0 && originForm {
		return req, nil
	}
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(head)))
	if _, err := tp.ReadLine(); err != nil {
		return nil, err
	}
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	if len(req.TransferEncoding) > 0 && len(hdr.Values("Content-Length")) > 0 {
		return nil, errors.New("both Content-Length and Transfer-Encoding")
	}
	if host := hdr.Values("Host"); !originForm && len(host) > 0 {
		req.Header["Host"] = host
	}
	return req, nil
}

// copyRequestBody forwards the body of req from br to w. A chunked body is
// decoded and written again in chunks, so the device frames it exactly where
// the tunnel did; trailers are not forwarded.
func copyRequestBody(w io.Writer, br *bufio.Reader, req *http.Request) error {
	if len(req.TransferEncoding) > 0 {
		cw := httputil.NewChunkedWriter(w)
		if _, err := io.Copy(cw, httputil.NewChunkedReader(br)); err != nil {
			return err
		}
		if err := cw.Close(); err != nil {
			return err
		}
		if err := skipTrailer(br); err != nil {
			return err
		}
		_, err := io.WriteString(w, "\r\n")
		return err
	}
	if req.ContentLength > 0 {
		_, err := io.CopyN(w, br, req.ContentLength)
		return err
	}
	return nil
}

// skipTrailer reads the trailer of a chunked body through the empty line that
// ends it, no more than a request head.
func skipTrailer(br *bufio.Reader) error {
	lineStart, read := true, 0
	for {
		line, err := br.ReadSlice('\n')
		read += len(line)
		if read > maxRequestHead {
			return errHeadTooLarge
		}
		switch {
		case err == bufio.ErrBufferFull:
			lineStart = false
			continue
		case err != nil:
			return err
		}
		if lineStart && isLineEnd(line) {
			return nil
		}
		lineStart = true
	}
}

func asksForUpgrade(req *http.Request) bool {
	if req.Header.Get("Upgrade") == "" {
		return false
	}
	for _, v := range req.Header.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

func writeRefusal(w io.Writer, r *gateRefusal) error {
	contentType, body := "text/plain; charset=utf-8", r.body+"\n"
	if r.toRoot {
		contentType = "text/html; charset=utf-8"
		refresh := "<meta http-equiv=\"refresh\" content=\"0; url=" + rootTarget + "\">\n"
		if r.linkOnly {
			refresh = ""
		}
		body = "<!DOCTYPE html>\n" + refresh + "<title>WebAdmin tunnel</title>\n<p>" +
			html.EscapeString(r.body) + " <a href=\"/\">Open the WebAdmin</a>.</p>\n"
	}
	var cookie string
	if r.setCookie != "" {
		cookie = "Set-Cookie: " + r.setCookie + "\r\n"
	}
	// no-referrer: the page that moves on is the link with the token, and the
	// move must not hand it to the device as the Referer.
	_, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nCache-Control: no-store\r\nReferrer-Policy: no-referrer\r\n%sConnection: close\r\n\r\n",
		r.status, http.StatusText(r.status), contentType, len(body), cookie)
	if err == nil && !r.head {
		_, err = io.WriteString(w, body)
	}
	return err
}

// describeRequest is the debug line of a request; never cookies or the query.
func describeRequest(req *http.Request) string {
	return fmt.Sprintf("method=%s path=%q host=%q origin=%q sec-fetch-site=%q sec-fetch-mode=%q sec-fetch-dest=%q upgrade=%q",
		req.Method, req.URL.Path, req.Host, req.Header.Get("Origin"), req.Header.Get("Sec-Fetch-Site"),
		req.Header.Get("Sec-Fetch-Mode"), req.Header.Get("Sec-Fetch-Dest"), req.Header.Get("Upgrade"))
}
