package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// stubAuthManager is a test double for authManager that returns preset values.
type stubAuthManager struct {
	token      string
	accessErr  error
	refreshErr error
	status     *models.SessionStatus
	logoutErr  error
	logouts    int
}

func (s *stubAuthManager) IsAuthenticated() bool                   { return s.token != "" }
func (s *stubAuthManager) SessionStatus() *models.SessionStatus    { return s.status }
func (s *stubAuthManager) GetAccessToken() (string, error)         { return s.token, s.accessErr }
func (s *stubAuthManager) ForceRefresh() error                     { return s.refreshErr }
func (s *stubAuthManager) GetUserInfo() (*models.UserInfo, error)  { return nil, nil }
func (s *stubAuthManager) GetTokenSummary() map[string]interface{} { return nil }
func (s *stubAuthManager) GetStorageName() string                  { return "stub" }
func (s *stubAuthManager) Logout() error {
	s.logouts++
	return s.logoutErr
}

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

// --- AuthStatus / AuthLogout tests ---

func TestAuthStatus_NilManagerIsNone(t *testing.T) {
	svc := &Service{} // auth is nil
	st := svc.AuthStatus()
	if st == nil || st.State != models.SessionNone {
		t.Fatalf("expected a non-nil SessionNone status, got %+v", st)
	}
}

// A manager that has nothing to say must not hand callers a nil to dereference.
func TestAuthStatus_ManagerReturningNilIsNone(t *testing.T) {
	svc := newServiceWithStubAuth(&stubAuthManager{})
	if st := svc.AuthStatus(); st == nil || st.State != models.SessionNone {
		t.Fatalf("expected a non-nil SessionNone status, got %+v", st)
	}
}

func TestAuthStatus_ReturnsTheManagersClassification(t *testing.T) {
	want := &models.SessionStatus{State: models.SessionRefreshable, Email: "a@example.com", HasRefresh: true}
	svc := newServiceWithStubAuth(&stubAuthManager{status: want})
	if got := svc.AuthStatus(); got != want {
		t.Fatalf("expected the manager's status to pass through, got %+v", got)
	}
}

func TestAuthLogout_NilManager(t *testing.T) {
	svc := &Service{}
	err := svc.AuthLogout()
	svcErr, ok := err.(*Error)
	if !ok || svcErr.Code != CodeNotAuthenticated {
		t.Fatalf("expected *Error with CodeNotAuthenticated, got %#v", err)
	}
}

func TestAuthLogout_ClearsTheSession(t *testing.T) {
	am := &stubAuthManager{}
	svc := newServiceWithStubAuth(am)
	if err := svc.AuthLogout(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if am.logouts != 1 {
		t.Errorf("expected exactly one Logout call, got %d", am.logouts)
	}
}

func TestAuthLogout_WrapsAFailure(t *testing.T) {
	cause := errors.New("keyring locked")
	svc := newServiceWithStubAuth(&stubAuthManager{logoutErr: cause})
	err := svc.AuthLogout()
	svcErr, ok := err.(*Error)
	if !ok || svcErr.Code != CodeAuthFailed {
		t.Fatalf("expected *Error with CodeAuthFailed, got %#v", err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected the cause to stay reachable through Unwrap, got %v", err)
	}
	if !strings.Contains(err.Error(), "logout failed") {
		t.Errorf("message should say what failed, got %q", err.Error())
	}
}
