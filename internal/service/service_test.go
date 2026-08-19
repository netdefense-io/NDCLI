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

func (s *stubAuthManager) IsAuthenticated() bool                   { return s.token != "" }
func (s *stubAuthManager) GetAccessToken() (string, error)         { return s.token, s.accessErr }
func (s *stubAuthManager) ForceRefresh() error                     { return s.refreshErr }
func (s *stubAuthManager) GetUserInfo() (*models.UserInfo, error)  { return nil, nil }
func (s *stubAuthManager) GetTokenSummary() map[string]interface{} { return nil }
func (s *stubAuthManager) GetStorageName() string                  { return "stub" }
func (s *stubAuthManager) Logout() error                           { return nil }

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

// --- NewFromProvider tests ---

// TestNewFromProvider_RequireAuthSucceeds guards the exact bug this
// constructor exists to avoid: a Service built with a non-nil auth
// implementation that isn't the concrete *auth.Manager (e.g.
// internal/auth.StaticTokenProvider, used for NDCLI_TOKEN static-PAT auth in
// the MCP server) must pass RequireAuth() — not be indistinguishable from
// the "no auth manager at all" case that New(client, nil, cfg) produces.
func TestNewFromProvider_RequireAuthSucceeds(t *testing.T) {
	svc := NewFromProvider(nil, &stubAuthManager{token: "static-pat-value"}, nil)
	if err := svc.RequireAuth(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if !svc.AuthIsAuthenticated() {
		t.Error("expected AuthIsAuthenticated to be true")
	}
}

func TestNewFromProvider_RequireAuthFailsOnRejectedToken(t *testing.T) {
	rejectErr := errors.New("token rejected")
	svc := NewFromProvider(nil, &stubAuthManager{accessErr: rejectErr}, nil)
	err := svc.RequireAuth()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rejectErr) {
		t.Errorf("expected wrapped rejectErr, got %v", err)
	}
}

// TestNewFromProvider_TypedNilPointerGuard guards the nil-pointer-in-
// interface footgun New() already guards against: a typed-nil *stubAuthManager
// passed as am must not box into a non-nil authManager interface value,
// which would silently defeat every s.auth == nil check downstream (e.g.
// RequireAuth would try to call GetAccessToken on a nil receiver instead of
// cleanly reporting CodeNotAuthenticated).
func TestNewFromProvider_TypedNilPointerGuard(t *testing.T) {
	var nilStub *stubAuthManager // typed nil, not an untyped nil literal
	svc := NewFromProvider(nil, nilStub, nil)
	err := svc.RequireAuth()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	svcErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if svcErr.Code != CodeNotAuthenticated {
		t.Errorf("expected CodeNotAuthenticated (guard should treat typed-nil as no auth manager), got %q", svcErr.Code)
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
