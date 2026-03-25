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
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/update"
)

// AuthProvider provides access tokens for API requests
type AuthProvider interface {
	GetAccessToken() (string, error)
	ForceRefresh() error
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
			return nil, &APIError{
				StatusCode: http.StatusUnauthorized,
				Message:    "Authentication failed. Please run 'ndcli auth login' to re-authenticate.",
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

// ParseResponse parses a JSON response into the given target
func ParseResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ParseError(resp)
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// ParseResponseWithStatus parses a JSON response and also returns the status code
func ParseResponseWithStatus(resp *http.Response, target interface{}) (int, error) {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, ParseError(resp)
	}

	if target == nil {
		return resp.StatusCode, nil
	}

	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(target)
}
