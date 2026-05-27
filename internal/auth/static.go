package auth

import "errors"

// ErrStaticTokenRejected is returned by StaticTokenProvider.ForceRefresh so
// the API client can surface a clear message instead of the generic "re-login"
// prompt, which doesn't apply to static PATs.
var ErrStaticTokenRejected = errors.New("token rejected — verify NDCLI_TOKEN is valid and not expired")

// StaticTokenProvider implements api.AuthProvider using a fixed PAT value.
// It is used when the NDCLI_TOKEN environment variable is set; the OAuth2
// device flow is bypassed entirely.
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
