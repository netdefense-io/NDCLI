package auth

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth/oauth2"
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

// Global manager instance
var globalManager *Manager

// GetManager returns the global authentication manager
func GetManager() *Manager {
	if globalManager == nil {
		globalManager = NewManager()
	}
	return globalManager
}
