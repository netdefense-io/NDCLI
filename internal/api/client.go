package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
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
// strings that are later printed verbatim.
func DecodeJSON(r io.Reader, target interface{}) error {
	if err := json.NewDecoder(capBody(r)).Decode(target); err != nil {
		return err
	}
	sanitize.Struct(reflect.ValueOf(target))
	return nil
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

// Client is the API client for NDManager
type Client struct {
	baseURL    string
	httpClient *http.Client
	authMgr    AuthProvider
	userAgent  string
}

// NewClient creates a new API client
func NewClient(baseURL string, sslVerify bool, authMgr AuthProvider) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !sslVerify,
		},
	}

	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		authMgr:   authMgr,
		userAgent: fmt.Sprintf("NDCLI-Go/%s", config.Version),
	}
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
		resp.Body.Close()

		// Brief backoff before refresh attempt
		time.Sleep(100 * time.Millisecond)

		if err := c.authMgr.ForceRefresh(); err != nil {
			msg := "Authentication failed. Please run 'ndcli auth login' to re-authenticate."
			// Preserve the provider's own message (e.g. static-token sentinel).
			if err.Error() != "" {
				msg = err.Error()
			}
			return nil, &APIError{
				StatusCode: http.StatusUnauthorized,
				Message:    msg,
			}
		}

		// Retry with new token
		return c.doRequest(ctx, method, path, body, false)
	}

	return resp, nil
}

func (c *Client) handleNetworkError(err error) error {
	cfg := config.Get()

	if urlErr, ok := err.(*url.Error); ok {
		if urlErr.Timeout() {
			return fmt.Errorf("request timed out connecting to %s\nPlease check your network connection", cfg.Controlplane.Host)
		}
	}

	return fmt.Errorf("cannot connect to controlplane at %s\n\nPlease check:\n  - Your network connection\n  - The controlplane host setting\n  - If SSL verification is required", cfg.Controlplane.Host)
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, path string, params map[string]string) (*http.Response, error) {
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
