package service

import (
	"errors"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// stubAuthManager is a test double for authManager that returns preset values.
type stubAuthManager struct {
	token      string
	accessErr  error
	refreshErr error
}

func (s *stubAuthManager) IsAuthenticated() bool                     { return s.token != "" }
func (s *stubAuthManager) GetAccessToken() (string, error)           { return s.token, s.accessErr }
func (s *stubAuthManager) ForceRefresh() error                       { return s.refreshErr }
func (s *stubAuthManager) GetUserInfo() (*models.UserInfo, error)    { return nil, nil }
func (s *stubAuthManager) GetTokenSummary() map[string]interface{}   { return nil }
func (s *stubAuthManager) GetStorageName() string                    { return "stub" }
func (s *stubAuthManager) Logout() error                             { return nil }

// newServiceWithStubAuth builds a bare *Service wired to the given stub.
func newServiceWithStubAuth(am authManager) *Service {
	return &Service{auth: am}
}

// --- RequireAuth tests ---

func TestRequireAuth_NilManager(t *testing.T) {
	svc := &Service{} // auth is nil
	err := svc.RequireAuth()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeNotAuthenticated {
		t.Errorf("expected CodeNotAuthenticated, got %q", svcErr.Code)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	svc := newServiceWithStubAuth(&stubAuthManager{token: "access-tok"})
	if err := svc.RequireAuth(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestRequireAuth_ExpiredTokenRefreshSucceeds simulates the case where the
// oauth2 client internally refreshes the access token and GetAccessToken
// returns the new token without error. RequireAuth must succeed.
func TestRequireAuth_ExpiredTokenRefreshSucceeds(t *testing.T) {
	// The stub returns a token (simulating what GetAccessToken returns after an
	// internal refresh). From RequireAuth's perspective this is indistinguishable
	// from a still-valid token — both paths result in a non-error return from
	// GetAccessToken, which is the only gate RequireAuth cares about.
	svc := newServiceWithStubAuth(&stubAuthManager{token: "refreshed-tok"})
	if err := svc.RequireAuth(); err != nil {
		t.Fatalf("expected nil after transparent refresh, got %v", err)
	}
}

// TestRequireAuth_ExpiredTokenRefreshFails simulates an expired access token
// whose refresh token is also gone. GetAccessToken returns an error and
// RequireAuth must surface CodeAuthFailed (not CodeNotAuthenticated).
func TestRequireAuth_ExpiredTokenRefreshFails(t *testing.T) {
	refreshErr := errors.New("no refresh token available, please login again")
	svc := newServiceWithStubAuth(&stubAuthManager{accessErr: refreshErr})
	err := svc.RequireAuth()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeAuthFailed {
		t.Errorf("expected CodeAuthFailed, got %q", svcErr.Code)
	}
	if !errors.Is(err, refreshErr) {
		t.Errorf("expected wrapped refreshErr, got %v", err)
	}
}

// --- AuthRefresh tests ---

func TestAuthRefresh_NilManager(t *testing.T) {
	svc := &Service{} // auth is nil
	err := svc.AuthRefresh()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeNotAuthenticated {
		t.Errorf("expected CodeNotAuthenticated, got %q", svcErr.Code)
	}
}

// TestAuthRefresh_ExpiredTokenRefreshSucceeds verifies that AuthRefresh calls
// ForceRefresh directly (no IsAuthenticated gate), so it works even when the
// access token is already expired.
func TestAuthRefresh_ExpiredTokenRefreshSucceeds(t *testing.T) {
	// accessErr set → IsAuthenticated() would return false on the old code,
	// causing it to reject the call. ForceRefresh succeeds → must return nil.
	svc := newServiceWithStubAuth(&stubAuthManager{
		accessErr:  errors.New("token expired"),
		refreshErr: nil,
	})
	if err := svc.AuthRefresh(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestAuthRefresh_ForceRefreshFails(t *testing.T) {
	refreshErr := errors.New("refresh token expired")
	svc := newServiceWithStubAuth(&stubAuthManager{refreshErr: refreshErr})
	err := svc.AuthRefresh()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeAuthFailed {
		t.Errorf("expected CodeAuthFailed, got %q", svcErr.Code)
	}
	if !errors.Is(err, refreshErr) {
		t.Errorf("expected wrapped refreshErr, got %v", err)
	}
}
