package tui

import (
	"errors"
	"strings"
	"testing"
)

type tokenSourceStub struct {
	err   error
	calls int
}

func (s *tokenSourceStub) GetAccessToken() (string, error) {
	s.calls++
	return "token", s.err
}

// A session whose access token lapsed overnight but can be renewed is a working
// session: asking the manager for a token renews it, where checking for an
// unexpired one refused to start the TUI.
func TestRequireSession_StartsWhenAnAccessTokenIsObtainable(t *testing.T) {
	src := &tokenSourceStub{}
	if err := requireSession(src); err != nil {
		t.Fatalf("requireSession: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("GetAccessToken called %d times, want 1", src.calls)
	}
}

func TestRequireSession_RefusesWhenNoAccessTokenCanBeObtained(t *testing.T) {
	err := requireSession(&tokenSourceStub{err: errors.New("no refresh token available")})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "ndcli auth login") {
		t.Errorf("the error should name the way out, got %q", err.Error())
	}
}
