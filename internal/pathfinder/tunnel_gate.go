package pathfinder

import (
	"bufio"
	"bytes"
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
	// toRoot: a page that moves on to "/".
	toRoot bool
}

// requestGate decides, for each request the browser sends through the
// tunnel, whether it reaches the device. The device signs every forwarded
// request in as the session's user, so a request the browser was made to send
// by another site, or sent to a name rebound to 127.0.0.1, must stop here.
type requestGate struct {
	hosts   []string
	origins []string
}

func newRequestGate(port int) *requestGate {
	p := strconv.Itoa(port)
	g := &requestGate{hosts: []string{"localhost:" + p, "127.0.0.1:" + p}}
	for _, h := range g.hosts {
		g.origins = append(g.origins, "http://"+h)
	}
	return g
}

// check returns nil for a request the tunnel forwards.
//
//   - Host must be the tunnel's own (localhost or 127.0.0.1 with its port):
//     a DNS-rebound name carries the attacker's.
//   - Sec-Fetch-Site, when sent, must be same-origin or none (the user typed
//     the address). Another port on localhost is same-site, and it is treated
//     like cross-site: it is another origin.
//   - A page opened from elsewhere (a link in another site or app, or a
//     browser extension) is a cross-site navigation, and so is every redirect
//     after it. It is answered with a page of the tunnel's own that moves on
//     to "/", which the browser then loads as same-origin. Another site never
//     chooses the page the session opens.
//   - Origin, when sent, must be one of the tunnel's own; "null" is refused.
//   - A browser that sends neither (before fetch metadata) is judged by its
//     Referer: one from elsewhere is treated as cross-site, and none passes.
//   - An upgrade (a WebSocket) is refused: the device carries none.
func (g *requestGate) check(req *http.Request) *gateRefusal {
	if !matchesAny(req.Host, g.hosts) {
		return &gateRefusal{status: http.StatusMisdirectedRequest,
			body: fmt.Sprintf("This WebAdmin tunnel answers only at http://%s/ or http://%s/.", g.hosts[0], g.hosts[1])}
	}
	if asksForUpgrade(req) {
		return &gateRefusal{status: http.StatusNotImplemented,
			body: "This WebAdmin tunnel does not carry WebSocket connections."}
	}
	crossSite := &gateRefusal{status: http.StatusForbidden,
		body: "This WebAdmin tunnel refuses requests that another site makes. Open the tunnel's address directly."}
	toRoot := &gateRefusal{status: http.StatusOK, toRoot: true,
		body: "Opening the WebAdmin. This tunnel opens its front page when a link from elsewhere leads here."}
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
		switch {
		case len(referer) == 0 || len(req.Header.Values("Origin")) == 1:
			return nil
		case len(referer) == 1 && g.ownURL(referer[0]):
			return nil
		case req.Method == http.MethodGet || req.Method == http.MethodHead:
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
// Content-Length or by chunks, never both.
func parseRequestHead(head []byte) (*http.Request, error) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(head)))
	if err != nil {
		return nil, err
	}
	if len(req.TransferEncoding) > 0 {
		tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(head)))
		if _, err := tp.ReadLine(); err != nil {
			return nil, err
		}
		hdr, err := tp.ReadMIMEHeader()
		if err != nil {
			return nil, err
		}
		if len(hdr.Values("Content-Length")) > 0 {
			return nil, errors.New("both Content-Length and Transfer-Encoding")
		}
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
		body = "<!DOCTYPE html>\n<meta http-equiv=\"refresh\" content=\"0; url=/\">\n<title>WebAdmin tunnel</title>\n<p>" +
			html.EscapeString(r.body) + " <a href=\"/\">Open the WebAdmin</a>.</p>\n"
	}
	_, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		r.status, http.StatusText(r.status), contentType, len(body))
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
