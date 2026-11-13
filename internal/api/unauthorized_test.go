package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rejectedProvider is a token the server refuses and that cannot be
// refreshed, as a static personal access token cannot.
type rejectedProvider struct{ refreshes int }

func (p *rejectedProvider) GetAccessToken() (string, error) { return "ndpat_rejected", nil }
func (p *rejectedProvider) ForceRefresh() error {
	p.refreshes++
	return errors.New("token rejected — verify NDCLI_TOKEN is valid and not expired")
}

// A 401 the token cannot recover from keeps the code the server answered
// with (AUTH_INVALID), so [CODE] and -f json's code show it, beside the
// provider's own words. Before, the body was closed unread and the code lost.
func TestARefusedTokenKeepsTheServersCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"Invalid or expired token","code":"AUTH_INVALID"}`)
	}))
	defer srv.Close()
	open := func() (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader([]byte("x"))), 1, nil
	}
	for name, call := range map[string]func(*Client) error{
		"a JSON request": func(c *Client) error {
			_, err := c.Get(context.Background(), "/api/v1/devices", nil)
			return err
		},
		"a raw upload": func(c *Client) error {
			_, err := c.PostRaw(context.Background(), "/api/v1/upload", nil, open, "application/octet-stream")
			return err
		},
		"a raw download": func(c *Client) error {
			_, err := c.GetStream(context.Background(), "/api/v1/download")
			return err
		},
	} {
		provider := &rejectedProvider{}
		err := call(NewClient(srv.URL, false, provider))
		if provider.refreshes != 1 {
			t.Errorf("%s: %d refreshes, want 1", name, provider.refreshes)
		}
		if got := GetErrorCode(err); got != "AUTH_INVALID" {
			t.Errorf("%s: code %q, want AUTH_INVALID (err %v)", name, got, err)
		}
		if !IsAuthError(err) || err == nil || err.Error() != "token rejected — verify NDCLI_TOKEN is valid and not expired" {
			t.Errorf("%s: err %v, want the provider's words on a 401", name, err)
		}
	}
}
