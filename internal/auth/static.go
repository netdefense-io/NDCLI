package auth

import (
	"errors"
	"fmt"
	"os"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// ErrStaticTokenRejected is returned by StaticTokenProvider.ForceRefresh so
// the API client can surface a clear message instead of the generic "re-login"
// prompt, which doesn't apply to static PATs.
var ErrStaticTokenRejected = errors.New("token rejected — verify NDCLI_TOKEN is valid and not expired")

// EnvToken is the environment variable that carries a static personal
// access token. Setting it bypasses the OAuth2 device flow entirely.
const EnvToken = "NDCLI_TOKEN"

// PATPrefix is the required prefix for a valid personal access token.
const PATPrefix = "ndpat_"

// IsValidPATFormat reports whether s has the shape of a valid personal
// access token. Currently just the prefix check — NDManager owns the rest
// of the token's format.
func IsValidPATFormat(s string) bool {
	return len(s) > len(PATPrefix) && s[:len(PATPrefix)] == PATPrefix
}

// StaticProviderFromEnv reads NDCLI_TOKEN and, if set, validates its format
// and returns a ready-to-use StaticTokenProvider.
//
// Returns (nil, nil) when the env var is unset — callers should fall back to
// the OAuth2/keyring path. Returns a non-nil error when the env var is set
// but malformed: callers must surface that loudly (fail startup / reject the
// command) rather than silently falling back to the keyring, which would
// mask a typo'd or truncated token behind a confusing "please log in"
// prompt instead of a clear "your token is bad" one.
//
// Shared by cli/root.go, internal/tui/run.go, and internal/mcp/server.go —
// the three front-ends over internal/service — so the env-var name, prefix
// check, and provider construction live in exactly one place.
func StaticProviderFromEnv() (*StaticTokenProvider, error) {
	token := os.Getenv(EnvToken)
	if token == "" {
		return nil, nil
	}
	if !IsValidPATFormat(token) {
		return nil, fmt.Errorf("%s does not look like a valid personal access token (expected prefix: %s)", EnvToken, PATPrefix)
	}
	return NewStaticTokenProvider(token), nil
}

// StaticTokenProvider implements api.AuthProvider using a fixed PAT value.
// It is used when the NDCLI_TOKEN environment variable is set; the OAuth2
// device flow is bypassed entirely.
//
// It also implements the richer method set internal/service.Service expects
// from an auth manager (IsAuthenticated / GetUserInfo / GetTokenSummary /
// GetStorageName / Logout), so a *Service constructed over a
// StaticTokenProvider behaves correctly under RequireAuth() and friends
// instead of silently acting "not authenticated" — see
// internal/service.NewFromProvider.
type StaticTokenProvider struct {
	token string
}

// NewStaticTokenProvider returns a StaticTokenProvider for the given raw token.
func NewStaticTokenProvider(token string) *StaticTokenProvider {
	return &StaticTokenProvider{token: token}
}

// GetAccessToken returns the static PAT.
func (s *StaticTokenProvider) GetAccessToken() (string, error) {
	return s.token, nil
}

// ForceRefresh is a no-op for static tokens — there is no refresh flow.
// It returns ErrStaticTokenRejected so the API client does not loop on 401.
func (s *StaticTokenProvider) ForceRefresh() error {
	return ErrStaticTokenRejected
}

// IsAuthenticated always reports true — a StaticTokenProvider only ever
// exists after NDCLI_TOKEN has passed format validation.
func (s *StaticTokenProvider) IsAuthenticated() bool {
	return true
}

// GetUserInfo has no locally cached identity to report for a static PAT
// (unlike the OAuth2 flow, there's no ID token to decode claims from).
// Callers that need the identity behind the token should call the
// /api/v1/auth/me endpoint instead (service.Service.AuthMe).
func (s *StaticTokenProvider) GetUserInfo() (*models.UserInfo, error) {
	return nil, errors.New("no cached user info for static-token auth — use 'auth me' instead")
}

// GetTokenSummary reports static-token mode without ever surfacing the raw
// token value.
func (s *StaticTokenProvider) GetTokenSummary() map[string]interface{} {
	return map[string]interface{}{
		"type":   "static_pat",
		"source": EnvToken,
	}
}

// GetStorageName reports the pseudo storage backend name for status/
// diagnostic output — there is no keyring/file storage involved.
func (s *StaticTokenProvider) GetStorageName() string {
	return "static-token (" + EnvToken + ")"
}

// Logout is a no-op — there is no local session to clear for a static PAT.
// Unset NDCLI_TOKEN to stop using it.
func (s *StaticTokenProvider) Logout() error {
	return nil
}
