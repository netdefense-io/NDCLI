package oauth2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/internal/auth/oauth2/providers"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// ErrAuthDisplayed is returned when the auth error was already displayed to the user
var ErrAuthDisplayed = errors.New("authentication failed")

// Poll-loop durations, all expressed in the provider's own unit (seconds) and
// multiplied by pollIntervalUnit. Keeping the fallbacks in units rather than
// as absolute Durations means every branch of the loop — including the ones
// that only run when the provider omits a value — scales with the test hook,
// so a future test cannot accidentally wait a real 5 s or 15 min.
const (
	// defaultPollIntervalUnits is used when the provider does not supply an
	// interval.
	defaultPollIntervalUnits = 5
	// defaultDeviceCodeLifetimeUnits bounds a non-interactive login when the
	// provider does not supply expires_in (15 minutes).
	defaultDeviceCodeLifetimeUnits = 900
	// slowDownBackoffUnits is how much is added to the poll interval each
	// time the provider says slow_down, matching the interactive renderer.
	slowDownBackoffUnits = 5
)

// pollIntervalUnit converts the provider's integer seconds into a Duration.
// A test overrides it so the poll loop does not take real seconds.
var pollIntervalUnit = time.Second

// SupportsInteractive reports whether the full-screen device-flow renderer can
// be used. It repaints with ANSI escapes and an in-place countdown, so it is
// only meaningful when stdout is a terminal — piped into a file, a log or a CI
// job it is pure noise.
//
// NB: this is deliberately stdout, not stdin. The IsTerminal checks in
// interactive.go guard raw-mode keyboard reads and are a different question.
func SupportsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

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
	var pollErr error

	if interactive {
		ia := NewInteractiveAuth(authResp, pollFunc)
		token, result = ia.Wait(ctx)
	} else {
		// Non-interactive mode - just display info and poll
		fmt.Printf("Please visit: %s\n", authResp.VerificationURIComplete)
		fmt.Printf("Or enter code: %s at https://%s/activate\n", authResp.UserCode, c.domain)

		token, result, pollErr = c.pollNonInteractive(ctx, authResp, pollFunc)
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
		// SaveTokens already prefixes its own errors; wrapping again here
		// produced "failed to save tokens: failed to save tokens: ...".
		if err := c.tokenManager.SaveTokens(token, userInfo, oauth2Config); err != nil {
			return nil, err
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
			// Nothing rendered the polling error, so it has to be carried
			// out of here or the user is told only "authentication failed".
			if pollErr != nil {
				return nil, pollErr
			}
			return nil, fmt.Errorf("authentication failed")
		}

	default:
		return nil, fmt.Errorf("unexpected auth result")
	}
}

// pollNonInteractive waits for the device flow to complete without rendering
// anything: no screen clears, no in-place countdown, no raw-mode key reader.
// This used to delegate to InteractiveAuth.Wait despite its own comment, so a
// redirected stdout collected an ANSI repaint per second.
//
// The caller has already printed the verification URL and user code once; from
// here the only output is whatever the caller makes of the returned result.
func (c *Client) pollNonInteractive(ctx context.Context, authResp *models.DeviceAuthResponse, pollFunc func() (*models.TokenResponse, error)) (*models.TokenResponse, AuthResult, error) {
	interval := time.Duration(authResp.Interval) * pollIntervalUnit
	if interval <= 0 {
		interval = defaultPollIntervalUnits * pollIntervalUnit
	}
	expiresIn := time.Duration(authResp.ExpiresIn) * pollIntervalUnit
	if expiresIn <= 0 {
		expiresIn = defaultDeviceCodeLifetimeUnits * pollIntervalUnit
	}

	// Handle Ctrl-C ourselves so an interrupted login reports "cancelled"
	// rather than dying mid-poll.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	expiry := time.NewTimer(expiresIn)
	defer expiry.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, AuthCancelled, nil

		case <-sigChan:
			return nil, AuthCancelled, nil

		case <-expiry.C:
			return nil, AuthTimeout, nil

		case <-ticker.C:
			token, err := pollFunc()
			if err == nil {
				return token, AuthSuccess, nil
			}
			switch {
			case errors.Is(err, providers.ErrAuthorizationPending):
				continue
			case errors.Is(err, providers.ErrSlowDown):
				interval += slowDownBackoffUnits * pollIntervalUnit
				ticker.Reset(interval)
				continue
			}
			return nil, AuthError, err
		}
	}
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
