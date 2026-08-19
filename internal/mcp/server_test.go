package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// newStaticAuthTestServer builds a *Server exactly the way NewServer's
// static-PAT (NDCLI_TOKEN) branch does: authManager left nil, svc wired to a
// real *auth.StaticTokenProvider via service.NewFromProvider. This is the
// object shape every real "netdefense-mcp with NDCLI_TOKEN set" process has
// at runtime — anything that nil-pointer-panics on s.authManager would panic
// here too.
func newStaticAuthTestServer(t *testing.T, token string) *Server {
	t.Helper()
	provider := auth.NewStaticTokenProvider(token)
	client := api.NewClient("https://example.invalid", false, provider)
	cfg := &config.Config{}
	return &Server{
		svc:             service.NewFromProvider(client, provider, cfg),
		config:          cfg,
		consoleSessions: newConsoleSessionManager(),
		// authManager intentionally left nil — matches static-PAT mode.
	}
}

// TestCheckAuth_StaticProvider_Succeeds guards against the nil-pointer panic
// checkAuth() used to hit under static-PAT mode (s.authManager.GetAccessToken()
// on a nil s.authManager) and confirms the gate actually passes for a
// well-formed static token instead of reporting "not authenticated".
func TestCheckAuth_StaticProvider_Succeeds(t *testing.T) {
	s := newStaticAuthTestServer(t, "ndpat_test123")
	if err := s.checkAuth(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestCheckAuth_NoAuthAtAll_Fails is the pre-existing "nobody authenticated"
// baseline (mirrors newTestServer in testutil_test.go) — checkAuth() must
// still reject cleanly, and the message must not tell a static-PAT deployment
// to run 'ndcli auth login' when s.authManager was never set in the first
// place (the general "not authenticated" case, not the static-PAT case).
func TestCheckAuth_NoAuthAtAll_Fails(t *testing.T) {
	client := api.NewClient("https://example.invalid", false, &fakeAuthProvider{})
	s := &Server{svc: service.New(client, nil, &config.Config{})}
	err := s.checkAuth()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("expected *ToolError, got %T", err)
	}
	if toolErr.Code != "AUTH_FAILED" {
		t.Errorf("expected AUTH_FAILED, got %q", toolErr.Code)
	}
}

// TestHandleAuthResource_StaticProvider_NoPanic guards resources.go's direct
// (pre-fix) use of s.authManager, which is nil under static-PAT mode — the
// resource handler must go through s.svc instead and report a sensible
// "authenticated" status rather than panicking or claiming
// not_authenticated for a working static token.
func TestHandleAuthResource_StaticProvider_NoPanic(t *testing.T) {
	s := newStaticAuthTestServer(t, "ndpat_test123")

	result, err := s.handleAuthResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "ndcli://auth/status"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(result.Contents))
	}

	var content map[string]interface{}
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &content); err != nil {
		t.Fatalf("failed to decode resource content: %v", err)
	}

	if authenticated, _ := content["authenticated"].(bool); !authenticated {
		t.Errorf("expected authenticated=true, got %v", content["authenticated"])
	}
	if status, _ := content["status"].(string); status != "authenticated" {
		t.Errorf("expected status=authenticated, got %q", status)
	}
}
