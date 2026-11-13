package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/sanitize"
	"github.com/netdefense-io/NDCLI/internal/update"
)

// AuthProvider provides access tokens for API requests
type AuthProvider interface {
	GetAccessToken() (string, error)
	ForceRefresh() error
}

// maxResponseBodyBytes caps how much of an HTTP response body is read/decoded.
// A var (not const) so tests can lower it to exercise the cap.
var maxResponseBodyBytes int64 = 32 << 20 // 32 MiB

// capBody wraps r so reads never exceed maxResponseBodyBytes, protecting
// against a malicious or misbehaving server streaming an oversized body.
func capBody(r io.Reader) io.Reader {
	return io.LimitReader(r, maxResponseBodyBytes)
}

// DecodeJSON decodes r into target, bounding the read to
// maxResponseBodyBytes and then scrubbing every string reachable from
// target via sanitize.Struct. It is the shared cap+sanitize primitive
// behind ParseResponse/ParseResponseWithStatus and ParseError, exported so
// callers that must decode a response body without going through those
// entry points still get the same two protections — a same-shape 2xx/4xx
// body decoded directly by the caller (SyncApply's 200/207/400 envelope),
// a helper decoding outside the *Client type (device name resolution), or
// an entirely separate HTTP client talking to a different trust boundary
// (the OAuth2 provider's calls to Auth0). Any of those, left unrouted
// through this helper, would let a malicious/misbehaving server stream an
// unbounded body or smuggle terminal escape sequences into decoded
// strings that are later printed verbatim. target is scrubbed even when the
// decode fails: a field of the wrong type still leaves the others filled,
// and a caller that keeps them must not get them raw.
func DecodeJSON(r io.Reader, target interface{}) error {
	err := json.NewDecoder(capBody(r)).Decode(target)
	sanitize.Struct(reflect.ValueOf(target))
	return err
}

// ReadBody reads r fully, bounded to maxResponseBodyBytes. It is the raw-
// bytes counterpart to DecodeJSON for callers that need the body itself
// rather than (or in addition to) a decoded struct — e.g. embedding a
// snippet of a non-JSON or error-shaped body in a message (run the result
// through sanitize.String first — ReadBody itself does not sanitize), or
// trying more than one JSON shape against the same bytes.
func ReadBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(capBody(r))
}

// userAgentProduct opens the User-Agent of every NDCLI binary. NDManager
// records how a login's request reached it from this header: a User-Agent that
// starts with NDCLI-Go/ is the CLI, or the MCP server when its comment holds
// the item mcp. A task carries that as its client, which is informational:
// nothing authorizes on it.
const userAgentProduct = "NDCLI-Go/"

// CLIUserAgent is the User-Agent of ndcli and the TUI: NDCLI-Go/<version>.
func CLIUserAgent() string { return userAgentProduct + config.Version }

// MCPUserAgent is the User-Agent of netdefense-mcp: NDCLI-Go/<version> (mcp).
func MCPUserAgent() string { return CLIUserAgent() + " (mcp)" }

// Client is the API client for NDManager
type Client struct {
	baseURL    string
	httpClient *http.Client
	rawClient  *http.Client
	authMgr    AuthProvider
	userAgent  string
}

// NewClient creates a new API client
func NewClient(baseURL string, sslVerify bool, authMgr AuthProvider) *Client {
	transport := newTransport(sslVerify)
	// The MCP server polls with several consoles at once; over HTTP/1.1, with
	// Go's default of 2 idle connections per host, most polls would open a new
	// TLS connection. Over HTTP/2 they share one connection.
	transport.MaxIdleConnsPerHost = 16

	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		// rawClient carries no global timeout: attachment uploads and
		// downloads are bounded by a per-transfer context deadline
		// (rawTransferTimeout) instead, because a 25 MiB transfer over a slow
		// link legitimately outlives the 30 s budget that suits a JSON call.
		// It has its own transport: over HTTP/2 a shared one would put a big
		// transfer on the same connection as the JSON calls and MCP polls.
		rawClient: &http.Client{
			Transport: newTransport(sslVerify),
		},
		authMgr:   authMgr,
		userAgent: CLIUserAgent(),
	}
}

// SetUserAgent replaces the User-Agent of every request the client sends, JSON
// and raw transfers alike.
func (c *Client) SetUserAgent(userAgent string) {
	c.userAgent = userAgent
}

// NewClientFromConfig creates a new API client from the current configuration
func NewClientFromConfig(authMgr AuthProvider) *Client {
	cfg := config.Get()
	return NewClient(cfg.Controlplane.Host, cfg.Controlplane.SSLVerify, authMgr)
}

// Request performs an HTTP request with authentication
func (c *Client) Request(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	return c.doRequest(ctx, method, path, body, true)
}

func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, retry bool) (*http.Response, error) {
	// Build URL
	reqURL := c.baseURL + path

	// Prepare body
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	// Add auth header
	if c.authMgr != nil {
		token, err := c.authMgr.GetAccessToken()
		if err != nil {
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.handleNetworkError(err)
	}

	// Process version headers from response (non-blocking)
	update.ProcessResponseHeaders(resp.Header)

	// Handle 401 - token refresh and retry
	if resp.StatusCode == http.StatusUnauthorized && retry && c.authMgr != nil {
		rejected := ParseError(resp)
		resp.Body.Close()

		// Brief backoff before refresh attempt
		time.Sleep(100 * time.Millisecond)

		if err := c.authMgr.ForceRefresh(); err != nil {
			return nil, refreshFailedError(rejected, err)
		}

		// Retry with new token
		return c.doRequest(ctx, method, path, body, false)
	}

	return resp, nil
}

// refreshFailedError is the error of a request the server refused with 401
// when the token could not be refreshed: the provider's own words (the
// static-token sentinel, say) or the re-login hint, with the code the server
// answered, so [AUTH_INVALID] and -f json's code survive.
func refreshFailedError(rejected *APIError, refreshErr error) *APIError {
	msg := "Authentication failed. Please run 'ndcli auth login' to re-authenticate."
	if refreshErr.Error() != "" {
		msg = refreshErr.Error()
	}
	return &APIError{StatusCode: http.StatusUnauthorized, Code: rejected.Code, Message: msg}
}

func (c *Client) handleNetworkError(err error) error {
	cfg := config.Get()

	if urlErr, ok := err.(*url.Error); ok {
		if urlErr.Timeout() {
			return &networkError{msg: fmt.Sprintf("request timed out connecting to %s\nPlease check your network connection", cfg.Controlplane.Host), err: err}
		}
	}

	return &networkError{msg: fmt.Sprintf("cannot connect to controlplane at %s\n\nPlease check:\n  - Your network connection\n  - The controlplane host setting\n  - If SSL verification is required", cfg.Controlplane.Host), err: err}
}

// h2PingTimeout bounds how long an HTTP/2 connection may stay silent before a
// PING checks it, and how long the PING may go unanswered before the
// connection is closed. Every JSON call and poll shares one HTTP/2 connection,
// so a connection dropped without a FIN or RST would otherwise stall them all.
const h2PingTimeout = 15 * time.Second

// newTransport is the control plane's transport: Go's default one (dial and
// TLS-handshake timeouts, idle limits, HTTP/2) with ndcli's TLS setting, HTTP/2
// health checks, and no proxy, since ndcli connects directly (proxy support is
// #371).
func newTransport(sslVerify bool) *http.Transport {
	t := defaultTransport(http.DefaultTransport)
	t.Proxy = nil
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: !sslVerify}
	t.HTTP2 = &http.HTTP2Config{SendPingTimeout: h2PingTimeout, PingTimeout: h2PingTimeout}
	return t
}

// defaultTransport clones Go's default transport, or builds one with the same
// settings if something has replaced it.
func defaultTransport(d http.RoundTripper) *http.Transport {
	if d, ok := d.(*http.Transport); ok {
		return d.Clone()
	}
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// networkError is the user-facing text for a failed request, keeping the
// transport error underneath so callers can tell a cancel or a timeout apart.
type networkError struct {
	msg string
	err error
}

func (e *networkError) Error() string { return e.msg }
func (e *networkError) Unwrap() error { return e.err }

// IsNetworkError reports whether err is a request to the control plane that
// failed in transit. A failed token refresh is not one, even when the
// refresh itself failed in transit: it runs on the auth provider's client.
func IsNetworkError(err error) bool {
	var n *networkError
	return errors.As(err, &n)
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, path string, params map[string]string) (*http.Response, error) {
	query := url.Values{}
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	return c.GetValues(ctx, path, query)
}

// GetValues performs a GET request whose query may repeat a key
// (?type=SYNC&type=BACKUP), which Get's one-value-per-key map cannot carry.
// Unlike Get it sends empty values as given.
func (c *Client) GetValues(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	if encoded := params.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	return c.Request(ctx, http.MethodGet, path, nil)
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Request(ctx, http.MethodPost, path, body)
}

// PostWithParams performs a POST request with query parameters
func (c *Client) PostWithParams(ctx context.Context, path string, params map[string]string, body interface{}) (*http.Response, error) {
	if len(params) > 0 {
		query := url.Values{}
		for k, v := range params {
			if v != "" {
				query.Set(k, v)
			}
		}
		if encoded := query.Encode(); encoded != "" {
			path = path + "?" + encoded
		}
	}
	return c.Request(ctx, http.MethodPost, path, body)
}

// Put performs a PUT request
func (c *Client) Put(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Request(ctx, http.MethodPut, path, body)
}

// Patch performs a PATCH request
func (c *Client) Patch(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Request(ctx, http.MethodPatch, path, body)
}

// Delete performs a DELETE request
func (c *Client) Delete(ctx context.Context, path string) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, nil)
}

// ParseResponse parses a JSON response into the given target. Every
// string reachable from target is scrubbed of terminal control bytes
// (sanitize.Struct) so server-supplied names/messages/etc. can never
// inject ANSI/OSC escape sequences into the operator's terminal.
func ParseResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ParseError(resp)
	}

	if target == nil {
		return nil
	}

	return DecodeJSON(resp.Body, target)
}

// ParseResponseWithStatus parses a JSON response and also returns the
// status code. See ParseResponse for the sanitize.Struct pass.
func ParseResponseWithStatus(resp *http.Response, target interface{}) (int, error) {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, ParseError(resp)
	}

	if target == nil {
		return resp.StatusCode, nil
	}

	if err := DecodeJSON(resp.Body, target); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

// --- Raw transfers (attachment upload / download) ---
//
// Ticket attachments are the only part of the API that moves bytes rather
// than JSON: an upload is the file itself as the raw request body (see the
// support-ticket attachment contract — multipart would have to be parsed in
// full before the size cap could reject it), and a download is the stored
// object streamed back. Both need two things the JSON helpers above do not
// provide: a body that is not marshalled/decoded, and a time budget large
// enough for 25 MiB over a slow link.

// rawTransferTimeout bounds a single attachment upload or download. It
// replaces the client's 30 s JSON timeout for these calls; the deadline is
// attached to the request context and released when the response body is
// closed. A var so tests can shorten it.
var rawTransferTimeout = 10 * time.Minute

// MaxAttachmentBytes is the server's per-file attachment cap. Clients check
// it before sending so an oversized file fails locally instead of after
// uploading a request the server rejects on Content-Length.
const MaxAttachmentBytes int64 = 25 << 20 // 25 MiB

// BodyOpener opens the request body for a raw POST. It returns the reader,
// the exact number of bytes it will yield (sent as Content-Length, which
// NDManager checks before reading a byte), and an error. PostRaw calls it
// once per attempt, so it must be able to produce a fresh reader for the
// single 401-retry.
type BodyOpener func() (io.ReadCloser, int64, error)

// cancelOnClose ties a context cancel func to the lifetime of a response
// body, so a streaming caller keeps its deadline until it is done reading
// and nothing leaks when it stops.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// PostRaw POSTs the bytes produced by open as the request body, with an
// explicit Content-Type and Content-Length, and the usual bearer token. The
// response is returned unparsed so the caller can decode the JSON envelope
// (or read the error) itself.
func (c *Client) PostRaw(ctx context.Context, path string, params map[string]string, open BodyOpener, contentType string) (*http.Response, error) {
	return c.doRawPost(ctx, appendParams(path, params), open, contentType, true)
}

func (c *Client) doRawPost(ctx context.Context, path string, open BodyOpener, contentType string, retry bool) (*http.Response, error) {
	body, length, err := open()
	if err != nil {
		return nil, err
	}

	transferCtx, cancel := context.WithTimeout(ctx, rawTransferTimeout)

	req, err := http.NewRequestWithContext(transferCtx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		body.Close()
		cancel()
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// ContentLength must be set explicitly: an io.ReadCloser of unknown
	// size would otherwise be sent chunked, and the server's pre-read size
	// check has nothing to look at.
	req.ContentLength = length
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.authMgr != nil {
		token, err := c.authMgr.GetAccessToken()
		if err != nil {
			body.Close()
			cancel()
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.rawClient.Do(req)
	if err != nil {
		cancel()
		return nil, c.handleNetworkError(err)
	}
	update.ProcessResponseHeaders(resp.Header)

	if resp.StatusCode == http.StatusUnauthorized && retry && c.authMgr != nil {
		rejected := ParseError(resp)
		resp.Body.Close()
		cancel()
		time.Sleep(100 * time.Millisecond)
		if err := c.authMgr.ForceRefresh(); err != nil {
			return nil, refreshFailedError(rejected, err)
		}
		// open() is called again on the retry, which is why it is a
		// factory rather than a reader: the first attempt consumed the
		// previous one.
		return c.doRawPost(ctx, path, open, contentType, false)
	}

	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// GetStream performs a GET and returns the response with its body still
// open, for callers that stream the payload to disk. A status >= 400 is
// parsed through ParseError exactly like a JSON call, and the body is
// closed before returning — so a non-nil response always has bytes worth
// reading, and the caller must close it.
func (c *Client) GetStream(ctx context.Context, path string) (*http.Response, error) {
	return c.doRawGet(ctx, path, true)
}

func (c *Client) doRawGet(ctx context.Context, path string, retry bool) (*http.Response, error) {
	transferCtx, cancel := context.WithTimeout(ctx, rawTransferTimeout)

	req, err := http.NewRequestWithContext(transferCtx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", c.userAgent)

	if c.authMgr != nil {
		token, err := c.authMgr.GetAccessToken()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.rawClient.Do(req)
	if err != nil {
		cancel()
		return nil, c.handleNetworkError(err)
	}
	update.ProcessResponseHeaders(resp.Header)

	if resp.StatusCode == http.StatusUnauthorized && retry && c.authMgr != nil {
		rejected := ParseError(resp)
		resp.Body.Close()
		cancel()
		time.Sleep(100 * time.Millisecond)
		if err := c.authMgr.ForceRefresh(); err != nil {
			return nil, refreshFailedError(rejected, err)
		}
		return c.doRawGet(ctx, path, false)
	}

	if resp.StatusCode >= 400 {
		apiErr := ParseError(resp)
		resp.Body.Close()
		cancel()
		return nil, apiErr
	}

	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// appendParams encodes params onto path as a query string, skipping empty
// values — the same rule Get/PostWithParams apply.
func appendParams(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	query := url.Values{}
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	if encoded := query.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}
