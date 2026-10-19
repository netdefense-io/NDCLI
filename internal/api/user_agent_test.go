package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// The User-Agent is a contract with NDManager, which records the client of a
// task a login creates from it: CLI for NDCLI-Go/<version>, MCP when the
// header's comment holds the item mcp. The expected strings are written out
// here, not read from the functions under test.

func withVersion(t *testing.T, version string) {
	t.Helper()
	orig := config.Version
	config.Version = version
	t.Cleanup(func() { config.Version = orig })
}

func TestUserAgentsAreTheContractStrings(t *testing.T) {
	withVersion(t, "1.40.0")
	if got := CLIUserAgent(); got != "NDCLI-Go/1.40.0" {
		t.Errorf("CLIUserAgent() = %q, want %q", got, "NDCLI-Go/1.40.0")
	}
	if got := MCPUserAgent(); got != "NDCLI-Go/1.40.0 (mcp)" {
		t.Errorf("MCPUserAgent() = %q, want %q", got, "NDCLI-Go/1.40.0 (mcp)")
	}
}

// TestClientSendsItsUserAgentOnEveryKindOfRequest: a JSON call, a raw upload
// and a raw download each set the header themselves, so each is checked, with
// the default and after SetUserAgent.
func TestClientSendsItsUserAgentOnEveryKindOfRequest(t *testing.T) {
	withVersion(t, "1.40.0")
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	send := func(c *Client) {
		t.Helper()
		ctx := context.Background()
		resp, err := c.Get(ctx, "/api/v1/tasks", nil)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		_ = resp.Body.Close()
		resp, err = c.PostRaw(ctx, "/api/v1/ticket-attachments", nil, func() (io.ReadCloser, int64, error) {
			return io.NopCloser(strings.NewReader("x")), 1, nil
		}, "text/plain")
		if err != nil {
			t.Fatalf("PostRaw: %v", err)
		}
		_ = resp.Body.Close()
		resp, err = c.GetStream(ctx, "/api/v1/ticket-attachments/u")
		if err != nil {
			t.Fatalf("GetStream: %v", err)
		}
		_ = resp.Body.Close()
	}

	c := NewClient(srv.URL, false, nil)
	send(c)
	c.SetUserAgent(MCPUserAgent())
	send(c)

	want := []string{
		"NDCLI-Go/1.40.0", "NDCLI-Go/1.40.0", "NDCLI-Go/1.40.0",
		"NDCLI-Go/1.40.0 (mcp)", "NDCLI-Go/1.40.0 (mcp)", "NDCLI-Go/1.40.0 (mcp)",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("User-Agents sent:\n  %q\nwant\n  %q", seen, want)
	}
}
