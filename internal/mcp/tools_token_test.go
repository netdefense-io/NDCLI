package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
