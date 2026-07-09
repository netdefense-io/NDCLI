package auth

import (
	"context"
	"fmt"
	"sync"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth/oauth2"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// Manager provides a high-level interface for authentication
type Manager struct {
	client *oauth2.Client
}

// NewManager creates a new authentication manager
func NewManager() *Manager {
	return &Manager{
		client: oauth2.NewClient(),
	}
}

// Login performs authentication using the device flow
func (m *Manager) Login(ctx context.Context, scopes string, forceNew bool) (*models.TokenResponse, error) {
	// Check if already authenticated (unless force new)
	if !forceNew && m.client.IsAuthenticated() {
		// Already authenticated, try to get user info
		userInfo, err := m.client.GetUserInfo()
		if err == nil && userInfo != nil {
			// Return existing token info
			tokens, _ := m.client.GetAccessToken()
			return &models.TokenResponse{
				AccessToken: tokens,
			}, nil
		}
	}

	// Fetch CLI config from NDManager to get OAuth2 settings
	cliConfig, err := api.FetchCLIConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch auth configuration from server: %w", err)
	}
	if err := validateOAuth2Domain(cliConfig.OAuth2.Domain); err != nil {
		return nil, err
	}

	// Create a new client with the fetched OAuth2 config
	loginClient := oauth2.NewClientWithConfig(cliConfig.OAuth2.Domain, cliConfig.OAuth2.ClientID)
	defer loginClient.Close()

	// Perform login
	token, err := loginClient.Login(ctx, scopes, true)
	if err != nil {
		return nil, err
	}

	// Reinitialize the main client to pick up the new tokens
	m.client = oauth2.NewClient()

	return token, nil
}

// Logout revokes tokens and clears the session
func (m *Manager) Logout() error {
	return m.client.Logout()
}

// IsAuthenticated checks if the user is authenticated
func (m *Manager) IsAuthenticated() bool {
	return m.client.IsAuthenticated()
}

// GetAccessToken returns a valid access token
func (m *Manager) GetAccessToken() (string, error) {
	return m.client.GetAccessToken()
}

// ForceRefresh forces a token refresh
func (m *Manager) ForceRefresh() error {
	return m.client.ForceRefresh()
}

// GetUserInfo returns the current user information
func (m *Manager) GetUserInfo() (*models.UserInfo, error) {
	return m.client.GetUserInfo()
}

// GetTokenSummary returns a summary of the current token state
func (m *Manager) GetTokenSummary() map[string]interface{} {
	return m.client.GetTokenSummary()
}

// Close releases resources
func (m *Manager) Close() {
	m.client.Close()
}

// GetStorageName returns the name of the storage backend being used
func (m *Manager) GetStorageName() string {
	return m.client.GetStorageName()
}

// validateOAuth2Domain rejects an OAuth2 domain returned by NDManager that
// doesn't match the locally configured/default identity provider. NDManager
// is untrusted for this purpose: a compromised or rogue control plane could
// otherwise redirect the device-code login flow to a phishing domain.
func validateOAuth2Domain(serverDomain string) error {
	expected := config.Get().OAuth2.Domain
	if expected == "" {
		expected = config.DefaultOAuth2Domain
	}
	if serverDomain != expected {
		return fmt.Errorf("refusing to authenticate: NDManager returned OAuth2 domain %q, expected %q — this may indicate a compromised control plane; if this is an intentional self-hosted deployment, set oauth2.domain in config.yaml to match", serverDomain, expected)
	}
	return nil
}

// Global manager instance
var (
	globalManager     *Manager
	globalManagerOnce sync.Once
)

// GetManager returns the global authentication manager, lazily initialized
// exactly once even if called from multiple goroutines.
func GetManager() *Manager {
	globalManagerOnce.Do(func() {
		globalManager = NewManager()
	})
	return globalManager
}
