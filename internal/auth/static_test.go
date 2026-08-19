package auth

import (
	"errors"
	"os"
	"testing"
)

func TestStaticTokenProvider_GetAccessToken(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_test123")
	tok, err := p.GetAccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "ndpat_test123" {
		t.Errorf("expected ndpat_test123, got %q", tok)
	}
}

func TestStaticTokenProvider_ForceRefresh_ReturnsSentinel(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_test123")
	err := p.ForceRefresh()
	if err == nil {
		t.Fatal("expected non-nil error from ForceRefresh")
	}
	if !errors.Is(err, ErrStaticTokenRejected) {
		t.Errorf("expected ErrStaticTokenRejected, got %v", err)
	}
}

func TestStaticTokenProvider_IsAuthenticated(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_test123")
	if !p.IsAuthenticated() {
		t.Error("expected IsAuthenticated to always be true for a static provider")
	}
}

func TestStaticTokenProvider_GetUserInfo_ReturnsError(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_test123")
	info, err := p.GetUserInfo()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if info != nil {
		t.Errorf("expected nil user info, got %+v", info)
	}
}

func TestStaticTokenProvider_GetTokenSummary_NeverLeaksRawToken(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_super-secret-value")
	summary := p.GetTokenSummary()
	for k, v := range summary {
		if s, ok := v.(string); ok && s == "ndpat_super-secret-value" {
			t.Errorf("raw token leaked in summary key %q", k)
		}
	}
	if summary["type"] != "static_pat" {
		t.Errorf("expected type=static_pat, got %v", summary["type"])
	}
}

func TestStaticTokenProvider_Logout_NoOp(t *testing.T) {
	p := NewStaticTokenProvider("ndpat_test123")
	if err := p.Logout(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- StaticProviderFromEnv tests ---

func withEnvToken(t *testing.T, value string) {
	t.Helper()
	if value == "" {
		os.Unsetenv(EnvToken)
		return
	}
	os.Setenv(EnvToken, value)
	t.Cleanup(func() { os.Unsetenv(EnvToken) })
}

func TestStaticProviderFromEnv_Unset(t *testing.T) {
	withEnvToken(t, "")
	provider, err := StaticProviderFromEnv()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if provider != nil {
		t.Fatalf("expected nil provider when NDCLI_TOKEN is unset, got %+v", provider)
	}
}

func TestStaticProviderFromEnv_Valid(t *testing.T) {
	withEnvToken(t, "ndpat_valid123")
	provider, err := StaticProviderFromEnv()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider for a valid NDCLI_TOKEN")
	}
	tok, tokErr := provider.GetAccessToken()
	if tokErr != nil {
		t.Fatalf("unexpected error: %v", tokErr)
	}
	if tok != "ndpat_valid123" {
		t.Errorf("expected provider wired to the env value, got %q", tok)
	}
}

func TestStaticProviderFromEnv_Malformed(t *testing.T) {
	cases := []string{"not-a-pat", "ndpat", "bearer_abc123", "ndpat_"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			withEnvToken(t, v)
			provider, err := StaticProviderFromEnv()
			if provider != nil {
				t.Errorf("expected nil provider for malformed token %q, got %+v", v, provider)
			}
			if err == nil {
				t.Fatalf("expected error for malformed token %q, got nil", v)
			}
		})
	}
}

func TestIsValidPATFormat(t *testing.T) {
	cases := map[string]bool{
		"ndpat_abc123": true,
		"ndpat_":       false, // prefix only, no value
		"ndpat":        false,
		"":             false,
		"bearer_xyz":   false,
	}
	for input, want := range cases {
		if got := IsValidPATFormat(input); got != want {
			t.Errorf("IsValidPATFormat(%q) = %v, want %v", input, got, want)
		}
	}
}
