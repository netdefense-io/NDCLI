package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// fakeAuthProvider satisfies api.AuthProvider with a static token.
type fakeAuthProvider struct{}

func (f *fakeAuthProvider) GetAccessToken() (string, error) { return "test-token", nil }
func (f *fakeAuthProvider) ForceRefresh() error             { return nil }

// newTestService creates a Service backed by a real *api.Client pointing at the given test server.
func newTestService(t *testing.T, srv *httptest.Server) *Service {
	t.Helper()
	client := api.NewClient(srv.URL, false, &fakeAuthProvider{})
	return New(client, nil, nil)
}

func TestTokenCreate_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ndpat_abc123",
			"name":       "ci-bot",
			"scope":      "RO",
			"org":        nil,
			"expires_at": nil,
			"created_at": "2026-05-26T12:00:00+00:00",
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	result, err := svc.TokenCreate(context.Background(), TokenCreateOpts{
		Name:      "ci-bot",
		Scope:     "RO",
		ExpiresIn: "90d",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token.Token != "ndpat_abc123" {
		t.Errorf("expected raw token ndpat_abc123, got %q", result.Token.Token)
	}
	if result.Token.Name != "ci-bot" {
		t.Errorf("expected token name ci-bot, got %q", result.Token.Name)
	}
}

func TestTokenCreate_ValidationErrors(t *testing.T) {
	svc := &Service{}

	_, err := svc.TokenCreate(context.Background(), TokenCreateOpts{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	_, err = svc.TokenCreate(context.Background(), TokenCreateOpts{Name: "x", Scope: "ADMIN"})
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %q", svcErr.Code)
	}
}

func TestTokenList_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/auth/tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "ci-bot", "token_prefix": "ndpat_ab", "scope": "RO", "is_expired": false, "is_revoked": false, "created_at": "2026-05-26T12:00:00+00:00"},
			{"name": "deploy", "token_prefix": "ndpat_cd", "scope": "RW", "is_expired": false, "is_revoked": false, "created_at": "2026-05-26T12:00:00+00:00"},
		})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	tokens, err := svc.TokenList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Name != "ci-bot" {
		t.Errorf("expected first token name ci-bot, got %q", tokens[0].Name)
	}
}

func TestTokenRevoke_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/auth/tokens/ci-bot" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	svc := newTestService(t, srv)
	if err := svc.TokenRevoke(context.Background(), "ci-bot"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTokenRevoke_MissingName(t *testing.T) {
	svc := &Service{}
	err := svc.TokenRevoke(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}
