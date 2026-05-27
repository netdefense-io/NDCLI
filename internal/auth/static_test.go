package auth

import (
	"errors"
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
