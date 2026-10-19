package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// TestServerClientSendsTheMCPUserAgent: NDManager records a task a tool creates
// as MCP only when the request's User-Agent is NDCLI-Go/<version> with the
// comment item mcp; NDCLI-Go/<version> alone reads as the CLI. The client
// NewServer builds, in both of its auth modes, is newAPIClient's.
func TestServerClientSendsTheMCPUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := config.Get()
	origHost, origVersion := cfg.Controlplane.Host, config.Version
	cfg.Controlplane.Host, config.Version = srv.URL, "1.40.0"
	t.Cleanup(func() { cfg.Controlplane.Host, config.Version = origHost, origVersion })

	resp, err := newAPIClient(&fakeAuthProvider{}).Get(context.Background(), "/api/v1/tasks", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()
	if got != "NDCLI-Go/1.40.0 (mcp)" {
		t.Errorf("User-Agent = %q, want %q", got, "NDCLI-Go/1.40.0 (mcp)")
	}
}
