package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestCreateToken_RequiresConfirm verifies that createToken returns a
// preview — without hitting the API and without minting a token — when
// confirm is omitted. Fails against pre-fix code, which always POSTs and
// returns the raw token regardless of confirm.
func TestCreateToken_RequiresConfirm(t *testing.T) {
	hit := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token": "ndpat_shouldnotbereturned",
			"name":  "ci-bot",
			"scope": "RW",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &TokenCreateInput{Name: "ci-bot", Scope: "RW"}
	result, err := s.createToken(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if hit {
		t.Fatal("expected the create-token endpoint NOT to be hit without confirm=true")
	}

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected a preview response, got %v", resp.Data)
	}
	if _, hasToken := data["token"]; hasToken {
		t.Error("preview response must not contain a raw token value")
	}
}

// TestCreateToken_RejectsNeverExpiryOverMCP verifies that expires_in="never"
// is rejected client-side even with confirm=true, so MCP callers can never
// mint a permanent credential. Fails against pre-fix code, which accepts
// "never" and mints unconditionally once confirmed.
func TestCreateToken_RejectsNeverExpiryOverMCP(t *testing.T) {
	hit := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &TokenCreateInput{Name: "ci-bot", Scope: "RW", ExpiresIn: "never", Confirm: true}
	result, err := s.createToken(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if hit {
		t.Fatal("expected the create-token endpoint NOT to be hit for expires_in=never")
	}
	if !result.IsError {
		t.Fatal("expected an error result for expires_in=never")
	}

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Error == nil || resp.Error.Code != service.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %+v", resp.Error)
	}
}

// TestCreateToken_ConfirmedCreateSucceeds preserves the happy path: a
// confirmed request with a bounded expiry hits the API and returns the raw
// token.
func TestCreateToken_ConfirmedCreateSucceeds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token": "ndpat_abc123",
			"name":  "ci-bot",
			"scope": "RW",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &TokenCreateInput{Name: "ci-bot", Scope: "RW", ExpiresIn: "90d", Confirm: true}
	result, err := s.createToken(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["token"] != "ndpat_abc123" {
		t.Errorf("expected raw token in confirmed response, got %v", resp.Data)
	}
}

// --- static-PAT (NDCLI_TOKEN) auth gate tests ---
//
// handleTokenCreate/handleTokenRevoke must reject a static-PAT-backed
// Server before ever reaching svc.RequireAuth() (which a static token
// always satisfies) — mirroring cli/root.go's isTokenMutationCommand.
// handleTokenList has no such gate: read access is allowed under a static
// PAT, same as the CLI.

// newStaticAuthTestServerWithAPI is newStaticAuthTestServer (server_test.go)
// but pointed at a real httptest server instead of an unreachable host, for
// tests that need a request to actually reach the (fake) API.
func newStaticAuthTestServerWithAPI(t *testing.T, srv *httptest.Server, org string) *Server {
	t.Helper()
	provider := auth.NewStaticTokenProvider("ndpat_test123")
	client := api.NewClient(srv.URL, false, provider)
	cfg := &config.Config{}
	cfg.Organization.Name = org
	return &Server{
		svc:             service.NewFromProvider(client, provider, cfg),
		config:          cfg,
		consoleSessions: newConsoleSessionManager(),
		// authManager intentionally left nil — matches static-PAT mode.
	}
}

func TestHandleTokenCreate_RejectsStaticAuth(t *testing.T) {
	hit := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleTokenCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"name":"ci-bot","scope":"RW","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if hit {
		t.Fatal("expected the create-token endpoint NOT to be hit under static-PAT auth")
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for token_create under static-PAT auth")
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Error == nil || resp.Error.Code != "INTERACTIVE_AUTH_REQUIRED" {
		t.Errorf("expected INTERACTIVE_AUTH_REQUIRED, got %+v", resp.Error)
	}
}

func TestHandleTokenRevoke_RejectsStaticAuth(t *testing.T) {
	hit := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleTokenRevoke(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"name":"ci-bot","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if hit {
		t.Fatal("expected the revoke-token endpoint NOT to be hit under static-PAT auth")
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for token_revoke under static-PAT auth")
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Error == nil || resp.Error.Code != "INTERACTIVE_AUTH_REQUIRED" {
		t.Errorf("expected INTERACTIVE_AUTH_REQUIRED, got %+v", resp.Error)
	}
}

// TestHandleTokenList_AllowsStaticAuth confirms the interactive-auth gate is
// scoped to create/revoke only — list must still reach the API under a
// static PAT, matching the CLI's "token list works with PAT auth" policy.
func TestHandleTokenList_AllowsStaticAuth(t *testing.T) {
	hit := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleTokenList(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !hit {
		t.Fatal("expected the list-tokens endpoint to be hit under static-PAT auth")
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success (no interactive-auth gate on list), got error: %+v", resp.Error)
	}
}
