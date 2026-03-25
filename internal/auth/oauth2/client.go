package oauth2

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/netdefense-io/NDCLI/internal/auth/oauth2/providers"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// ErrAuthDisplayed is returned when the auth error was already displayed to the user
var ErrAuthDisplayed = errors.New("authentication failed")

// Client orchestrates the OAuth2 authentication flow
type Client struct {
	domain       string
	clientID     string
	provider     providers.Provider
	tokenManager *TokenManager
	refreshMu    sync.Mutex
}

// NewClient creates a new OAuth2 client.
// It loads OAuth2 config from stored tokens if available.
func NewClient() *Client {
	client := &Client{
		tokenManager: NewTokenManager(""),
	}

	// Try to load OAuth2 config from stored tokens
	tokens, err := client.tokenManager.LoadTokens()
	if err == nil && tokens != nil && tokens.OAuth2Config != nil {
		client.domain = tokens.OAuth2Config.Domain
		client.clientID = tokens.OAuth2Config.ClientID
		client.provider = providers.NewAuth0Provider(client.domain, client.clientID)
	}

	return client
}

// NewClientWithConfig creates a new OAuth2 client with the given domain and client ID.
// Used during login after fetching config from NDManager.
func NewClientWithConfig(domain, clientID string) *Client {
	return &Client{
		domain:       domain,
		clientID:     clientID,
		provider:     providers.NewAuth0Provider(domain, clientID),
		tokenManager: NewTokenManager(""),
	}
}

// Login performs the device authorization flow
func (c *Client) Login(ctx context.Context, scopes string, interactive bool) (*models.TokenResponse, error) {
	if c.provider == nil {
		return nil, fmt.Errorf("OAuth2 provider not configured - call NewClientWithConfig first")
	}

	if scopes == "" {
		scopes = config.DefaultOAuth2Scopes
	}

	// Request device authorization
	authResp, err := c.provider.RequestDeviceAuthorization(scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate device authorization: %w", err)
	}

	// Create poll function
	interval := authResp.Interval
	if interval == 0 {
		interval = 5
	}

	pollFunc := func() (*models.TokenResponse, error) {
		return c.provider.PollForToken(authResp.DeviceCode, interval)
	}

	// Wait for user to authenticate
	var token *models.TokenResponse
	var result AuthResult

	if interactive {
		ia := NewInteractiveAuth(authResp, pollFunc)
		token, result = ia.Wait(ctx)
	} else {
		// Non-interactive mode - just display info and poll
		fmt.Printf("Please visit: %s\n", authResp.VerificationURIComplete)
		fmt.Printf("Or enter code: %s at https://%s/activate\n", authResp.UserCode, c.domain)

		token, result = c.pollNonInteractive(ctx, authResp, pollFunc)
	}

	switch result {
	case AuthSuccess:
		// Get user info (non-critical - auth still succeeds without it)
		userInfo, err := c.provider.GetUserInfo(token.AccessToken)
		if err != nil {
			userInfo = nil
		}

		// Save tokens with OAuth2 config
		oauth2Config := &models.StoredOAuth2Config{
			Domain:   c.domain,
			ClientID: c.clientID,
		}
		if err := c.tokenManager.SaveTokens(token, userInfo, oauth2Config); err != nil {
			return nil, fmt.Errorf("failed to save tokens: %w", err)
		}

		return token, nil

	case AuthTimeout, AuthDenied, AuthCancelled, AuthError:
		// In interactive mode, the error message was already displayed
		if interactive {
			return nil, ErrAuthDisplayed
		}
		// Non-interactive mode needs explicit error messages
		switch result {
		case AuthTimeout:
			return nil, fmt.Errorf("authentication timed out")
		case AuthDenied:
			return nil, fmt.Errorf("authentication denied")
		case AuthCancelled:
			return nil, fmt.Errorf("authentication cancelled")
		default:
			return nil, fmt.Errorf("authentication failed")
		}

	default:
		return nil, fmt.Errorf("unexpected auth result")
	}
}

func (c *Client) pollNonInteractive(ctx context.Context, authResp *models.DeviceAuthResponse, pollFunc func() (*models.TokenResponse, error)) (*models.TokenResponse, AuthResult) {
	// Simple polling loop for non-interactive mode
	ia := NewInteractiveAuth(authResp, pollFunc)
	return ia.Wait(ctx)
}

// Logout revokes tokens and clears storage
func (c *Client) Logout() error {
	tokens, err := c.tokenManager.LoadTokens()
	if err != nil {
		return err
	}

	if tokens != nil {
		// Try to revoke the refresh token first (more important)
		if tokens.RefreshToken != "" {
			_ = c.provider.RevokeToken(tokens.RefreshToken, "refresh_token")
		}

		// Try to revoke the access token
		if tokens.AccessToken != "" {
			_ = c.provider.RevokeToken(tokens.AccessToken, "access_token")
		}
	}

	// Clear local storage
	return c.tokenManager.Clear()
}

// GetAccessToken returns a valid access token, refreshing if necessary
func (c *Client) GetAccessToken() (string, error) {
	// Try to get a valid token from storage (no lock needed for read)
	token, err := c.tokenManager.GetValidAccessToken()
	if err != nil {
		return "", err
	}
	if token != "" {
		return token, nil
	}

	// Token is expired, try to refresh (synchronized)
	if err := c.refresh(); err != nil {
		return "", err
	}

	return c.tokenManager.GetValidAccessToken()
}

// Refresh refreshes the access token using the refresh token
func (c *Client) Refresh() error {
	return c.refresh()
}

// refresh performs the actual token refresh, synchronized with a mutex
// to prevent concurrent refresh requests from racing
func (c *Client) refresh() error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// Double-check: another goroutine may have refreshed while we waited
	token, err := c.tokenManager.GetValidAccessToken()
	if err == nil && token != "" {
		return nil
	}

	refreshToken, err := c.tokenManager.GetRefreshToken()
	if err != nil {
		return err
	}
	if refreshToken == "" {
		return fmt.Errorf("no refresh token available, please login again")
	}

	newTokens, err := c.provider.RefreshToken(refreshToken)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	return c.tokenManager.UpdateAccessToken(newTokens)
}

// ForceRefresh forces a token refresh
func (c *Client) ForceRefresh() error {
	return c.refresh()
}

// IsAuthenticated checks if there are valid tokens stored
func (c *Client) IsAuthenticated() bool {
	token, err := c.tokenManager.GetValidAccessToken()
	return err == nil && token != ""
}

// GetUserInfo returns the stored user information
func (c *Client) GetUserInfo() (*models.UserInfo, error) {
	tokens, err := c.tokenManager.LoadTokens()
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	return tokens.UserInfo, nil
}

// GetTokenSummary returns a summary of the stored tokens
func (c *Client) GetTokenSummary() map[string]interface{} {
	return c.tokenManager.GetTokenSummary()
}

// Close releases resources
func (c *Client) Close() {
	if c.provider != nil {
		c.provider.Close()
	}
}

// GetStorageName returns the name of the storage backend being used
func (c *Client) GetStorageName() string {
	return c.tokenManager.StorageName()
}
