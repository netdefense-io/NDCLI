package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// sessionStub satisfies the service's auth manager interface with a preset
// classification and call counters, so the auth commands can be exercised
// without a keyring or a token endpoint.
type sessionStub struct {
	status     *models.SessionStatus
	refreshErr error
	logoutErr  error
	refreshes  int
	logouts    int
}

func (s *sessionStub) IsAuthenticated() bool                { return s.status.State == models.SessionValid }
func (s *sessionStub) SessionStatus() *models.SessionStatus { return s.status }
func (s *sessionStub) GetAccessToken() (string, error)      { return "tok", nil }
func (s *sessionStub) ForceRefresh() error {
	s.refreshes++
	return s.refreshErr
}
func (s *sessionStub) GetUserInfo() (*models.UserInfo, error)  { return nil, nil }
func (s *sessionStub) GetTokenSummary() map[string]interface{} { return nil }
func (s *sessionStub) GetStorageName() string                  { return "stub-storage" }
func (s *sessionStub) Logout() error {
	s.logouts++
	return s.logoutErr
}

func serviceWithSession(state models.SessionState) (*service.Service, *sessionStub) {
	stub := &sessionStub{status: &models.SessionStatus{State: state}}
	return service.NewFromProvider(nil, stub, nil), stub
}

// plainColor turns colour escapes off so output can be compared as text.
func plainColor(t *testing.T) {
	t.Helper()
	orig := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = orig })
}

// captureStdout runs fn with os.Stdout and color.Output pointed at a pipe.
// color.Output is captured at package init, so swapping os.Stdout alone would
// leave every color.Green(...) writing to the real terminal.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout, origColorOutput, origNoColor := os.Stdout, color.Output, color.NoColor
	os.Stdout, color.Output, color.NoColor = w, w, true
	defer func() {
		os.Stdout, color.Output, color.NoColor = origStdout, origColorOutput, origNoColor
	}()

	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	return string(out)
}

var authStatusNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func renderState(t *testing.T, st *models.SessionStatus) string {
	t.Helper()
	plainColor(t)
	var buf bytes.Buffer
	renderAuthStatus(&buf, st, "https://api.example.test", "file", authStatusNow)
	return buf.String()
}

func TestRenderAuthStatus_ValidIsUnchanged(t *testing.T) {
	got := renderState(t, &models.SessionStatus{
		State:      models.SessionValid,
		Email:      "alice@example.com",
		Name:       "Alice",
		Subject:    "sub-1",
		ExpiresAt:  authStatusNow.Add(6 * time.Hour),
		HasRefresh: true,
	})
	want := `Authentication Status:

  Email:     alice@example.com
  Name:      Alice
  Subject:   sub-1
  Expires:   2026-09-29T18:00:00Z
  Status:    Valid
  Refresh:   Available
  Host:      https://api.example.test
  Storage:   file
`
	if got != want {
		t.Errorf("valid rendering changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// The reported bug: an expired access token with a refresh token was printed as
// "Not authenticated" although every other command worked.
func TestRenderAuthStatus_RefreshableIsAWorkingSession(t *testing.T) {
	got := renderState(t, &models.SessionStatus{
		State:      models.SessionRefreshable,
		Email:      "alice@example.com",
		Name:       "Alice",
		Subject:    "sub-1",
		ExpiresAt:  authStatusNow.Add(-48 * time.Hour),
		HasRefresh: true,
	})
	want := `Authentication Status:

  Email:     alice@example.com
  Name:      Alice
  Subject:   sub-1
  Expired:   2026-09-27T12:00:00Z (2d ago)
  Status:    Access token expired; it refreshes automatically on the next command
  Refresh:   Available
  Host:      https://api.example.test
  Storage:   file
`
	if got != want {
		t.Errorf("refreshable rendering:\ngot:\n%s\nwant:\n%s", got, want)
	}
	for _, unwanted := range []string{"Not authenticated", "login", "✗"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a refreshable session must not read as signed out, found %q", unwanted)
		}
	}
}

func TestRenderAuthStatus_ExpiredWithoutRefreshTokenNamesLogin(t *testing.T) {
	got := renderState(t, &models.SessionStatus{
		State:     models.SessionExpired,
		Email:     "alice@example.com",
		ExpiresAt: authStatusNow.Add(-90 * time.Minute),
	})
	want := `✗ Session expired
  Email:     alice@example.com
  Expired:   2026-09-29T10:30:00Z (1h ago)
  Host:      https://api.example.test
  Storage:   file

Run 'ndcli auth login' to authenticate
`
	if got != want {
		t.Errorf("expired rendering:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "refreshes automatically") {
		t.Error("an expired session with no refresh token must not promise a refresh")
	}
}

func TestRenderAuthStatus_NoneIsNotAuthenticated(t *testing.T) {
	got := renderState(t, &models.SessionStatus{State: models.SessionNone})
	want := `✗ Not authenticated
  Host:      https://api.example.test
  Storage:   file

Run 'ndcli auth login' to authenticate
`
	if got != want {
		t.Errorf("none rendering:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderAuthStatus_StaticToken(t *testing.T) {
	got := renderState(t, &models.SessionStatus{State: models.SessionStatic})
	want := `Authentication Status:

  Method:    Personal access token (NDCLI_TOKEN)
  Host:      https://api.example.test
  Storage:   file
`
	if got != want {
		t.Errorf("static rendering:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Wording written for the user never names the identity provider.
func TestRenderAuthStatus_NeverNamesTheIdentityProvider(t *testing.T) {
	for _, state := range []models.SessionState{
		models.SessionNone, models.SessionValid, models.SessionRefreshable, models.SessionExpired, models.SessionStatic,
	} {
		got := renderState(t, &models.SessionStatus{State: state, ExpiresAt: authStatusNow, HasRefresh: true})
		if strings.Contains(strings.ToLower(got), "auth0") {
			t.Errorf("state %q names the identity provider:\n%s", state, got)
		}
	}
}

func TestAgoSince(t *testing.T) {
	for _, tt := range []struct {
		age  time.Duration
		want string
	}{
		{-30 * time.Second, "just now"}, // inside the 60 s refresh buffer, not yet past expiry
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{90 * time.Minute, "1h ago"},
		{23 * time.Hour, "23h ago"},
		{48 * time.Hour, "2d ago"},
		{30 * 24 * time.Hour, "30d ago"},
	} {
		if got := agoSince(authStatusNow, authStatusNow.Add(-tt.age)); got != tt.want {
			t.Errorf("agoSince(age=%v) = %q, want %q", tt.age, got, tt.want)
		}
	}
}

// Logout keys off what is stored, not off the access token: an expired access
// token still leaves a refresh token behind, which is the one worth revoking.
func TestLogoutSession(t *testing.T) {
	plainColor(t)
	for _, tt := range []struct {
		state       models.SessionState
		wantLogouts int
		wantOut     string
	}{
		{models.SessionValid, 1, "Successfully logged out"},
		{models.SessionRefreshable, 1, "Successfully logged out"},
		{models.SessionExpired, 1, "Successfully logged out"},
		{models.SessionNone, 0, "Not authenticated"},
		{models.SessionStatic, 0, "NDCLI_TOKEN is set"},
	} {
		t.Run(string(tt.state), func(t *testing.T) {
			s, stub := serviceWithSession(tt.state)
			var buf bytes.Buffer
			if err := logoutSession(&buf, s); err != nil {
				t.Fatalf("logoutSession: %v", err)
			}
			if stub.logouts != tt.wantLogouts {
				t.Errorf("Logout called %d times, want %d", stub.logouts, tt.wantLogouts)
			}
			if !strings.Contains(buf.String(), tt.wantOut) {
				t.Errorf("output %q does not contain %q", buf.String(), tt.wantOut)
			}
		})
	}
}

func TestLogoutSession_ReportsAFailure(t *testing.T) {
	plainColor(t)
	s, stub := serviceWithSession(models.SessionRefreshable)
	stub.logoutErr = errors.New("keyring locked")

	var buf bytes.Buffer
	err := logoutSession(&buf, s)
	if err == nil || !strings.Contains(err.Error(), "logout failed") {
		t.Fatalf("expected a logout failure, got %v", err)
	}
	if strings.Contains(buf.String(), "Successfully") {
		t.Errorf("a failed logout must not claim success: %q", buf.String())
	}
}

// `auth refresh` used to refuse unless the access token was unexpired, which is
// exactly when a refresh is not needed.
func TestRefreshSession(t *testing.T) {
	plainColor(t)
	for _, tt := range []struct {
		state        models.SessionState
		wantRefresh  int
		wantOut      string
		wantNotInOut string
	}{
		{models.SessionRefreshable, 1, "Token refreshed successfully", "Not authenticated"},
		{models.SessionValid, 1, "Token refreshed successfully", ""},
		{models.SessionExpired, 1, "Token refreshed successfully", ""},
		{models.SessionNone, 0, "Not authenticated", ""},
		{models.SessionStatic, 0, "nothing to refresh", "rejected"},
	} {
		t.Run(string(tt.state), func(t *testing.T) {
			s, stub := serviceWithSession(tt.state)
			var buf bytes.Buffer
			if err := refreshSession(&buf, s); err != nil {
				t.Fatalf("refreshSession: %v", err)
			}
			if stub.refreshes != tt.wantRefresh {
				t.Errorf("ForceRefresh called %d times, want %d", stub.refreshes, tt.wantRefresh)
			}
			if !strings.Contains(buf.String(), tt.wantOut) {
				t.Errorf("output %q does not contain %q", buf.String(), tt.wantOut)
			}
			if tt.wantNotInOut != "" && strings.Contains(buf.String(), tt.wantNotInOut) {
				t.Errorf("output %q must not contain %q", buf.String(), tt.wantNotInOut)
			}
		})
	}
}

// A refresh that fails is an error whatever state preceded it, so a script sees
// a non-zero exit. The expired row is only the plumbing: the real "no refresh
// token" failure is asserted where it originates, in the oauth2 package.
func TestRefreshSession_SurfacesTheFailure(t *testing.T) {
	plainColor(t)
	for _, state := range []models.SessionState{models.SessionRefreshable, models.SessionExpired, models.SessionValid} {
		t.Run(string(state), func(t *testing.T) {
			s, stub := serviceWithSession(state)
			stub.refreshErr = errors.New("refresh token revoked")

			var buf bytes.Buffer
			err := refreshSession(&buf, s)
			if err == nil || !strings.Contains(err.Error(), "refresh token revoked") {
				t.Fatalf("expected the refresh failure, got %v", err)
			}
			if strings.Contains(buf.String(), "successfully") {
				t.Errorf("a failed refresh must not claim success: %q", buf.String())
			}
		})
	}
}

// TestStaticTokenModeNeverDereferencesTheAuthManager drives the commands that
// used to panic under NDCLI_TOKEN: the global authManager is nil there, and
// auth show, auth logout, auth login and config show all reached through it.
func TestStaticTokenModeNeverDereferencesTheAuthManager(t *testing.T) {
	origSvc, origClient, origManager := svc, apiClient, authManager
	t.Cleanup(func() { svc, apiClient, authManager = origSvc, origClient, origManager })
	authManager = nil
	initStaticAuth(auth.NewStaticTokenProvider("ndpat_test123"))

	if authManager != nil {
		t.Fatal("static mode must leave the OAuth2 manager nil")
	}
	if got := svc.AuthStatus().State; got != models.SessionStatic {
		t.Fatalf("the CLI service must be built over the static provider, State = %q", got)
	}

	t.Run("auth show", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := runAuthShow(authShowCmd, nil); err != nil {
				t.Errorf("auth show: %v", err)
			}
		})
		for _, want := range []string{"Personal access token (NDCLI_TOKEN)", "static-token (NDCLI_TOKEN)"} {
			if !strings.Contains(out, want) {
				t.Errorf("auth show output lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "ndpat_test123") {
			t.Error("auth show printed the token")
		}
	})

	t.Run("auth logout", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := runAuthLogout(authLogoutCmd, nil); err != nil {
				t.Errorf("auth logout: %v", err)
			}
		})
		if !strings.Contains(out, "NDCLI_TOKEN is set") {
			t.Errorf("auth logout output: %q", out)
		}
	})

	t.Run("auth refresh", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := runAuthRefresh(authRefreshCmd, nil); err != nil {
				t.Errorf("auth refresh: %v", err)
			}
		})
		if !strings.Contains(out, "nothing to refresh") {
			t.Errorf("auth refresh output: %q", out)
		}
	})

	t.Run("auth login", func(t *testing.T) {
		err := runAuthLogin(authLoginCmd, nil)
		if !errors.Is(err, errLoginWithStaticToken) {
			t.Fatalf("auth login under NDCLI_TOKEN = %v, want errLoginWithStaticToken", err)
		}
		if !strings.Contains(err.Error(), "NDCLI_TOKEN") {
			t.Errorf("the error should name the variable to unset: %v", err)
		}
	})

	t.Run("config show", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := runConfigShow(configShowCmd, nil); err != nil {
				t.Errorf("config show: %v", err)
			}
		})
		if !strings.Contains(out, "Auth storage: static-token (NDCLI_TOKEN)") {
			t.Errorf("config show output lacks the static storage name:\n%s", out)
		}
	})
}
