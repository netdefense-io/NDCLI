package providers

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// Auth0Provider implements the Provider interface for Auth0
type Auth0Provider struct {
	domain     string
	clientID   string
	audience   string
	httpClient *http.Client
}

// NewAuth0Provider creates a new Auth0 provider with the given domain and client ID
func NewAuth0Provider(domain, clientID string) *Auth0Provider {
	return &Auth0Provider{
		domain:   domain,
		clientID: clientID,
		audience: config.DefaultOAuth2Audience,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		},
	}
}

// Name returns the provider name
func (p *Auth0Provider) Name() string {
	return "auth0"
}

// RequestDeviceAuthorization initiates the device authorization flow
func (p *Auth0Provider) RequestDeviceAuthorization(scopes string) (*models.DeviceAuthResponse, error) {
	deviceCodeURL := fmt.Sprintf("https://%s/oauth/device/code", p.domain)

	data := url.Values{}
	data.Set("client_id", p.clientID)
	data.Set("scope", scopes)
	if p.audience != "" {
		data.Set("audience", p.audience)
	}

	req, err := http.NewRequest("POST", deviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request device authorization: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device authorization failed: %s", string(body))
	}

	var authResp models.DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to parse device authorization response: %w", err)
	}

	return &authResp, nil
}

// PollForToken polls for the access token during device flow
func (p *Auth0Provider) PollForToken(deviceCode string, interval int) (*models.TokenResponse, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/token", p.domain)

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)
	data.Set("client_id", p.clientID)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to poll for token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for pending/error states
	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		switch errResp.Error {
		case "authorization_pending":
			return nil, ErrAuthorizationPending
		case "slow_down":
			return nil, ErrSlowDown
		case "access_denied":
			return nil, fmt.Errorf("authorization denied by user")
		case "expired_token":
			return nil, fmt.Errorf("device code expired")
		default:
			return nil, fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed: %s", string(body))
	}

	var tokenResp models.TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshToken refreshes an access token using a refresh token
func (p *Auth0Provider) RefreshToken(refreshToken string) (*models.TokenResponse, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/token", p.domain)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", p.clientID)
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tokenResp models.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// RevokeToken revokes an access or refresh token
func (p *Auth0Provider) RevokeToken(token, tokenTypeHint string) error {
	revokeURL := fmt.Sprintf("https://%s/oauth/revoke", p.domain)

	data := url.Values{}
	data.Set("client_id", p.clientID)
	data.Set("token", token)
	if tokenTypeHint != "" {
		data.Set("token_type_hint", tokenTypeHint)
	}

	req, err := http.NewRequest("POST", revokeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	defer resp.Body.Close()

	// Auth0 returns 200 even if token is invalid (by design)
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token revocation failed: %s", string(respBody))
	}

	return nil
}

// GetUserInfo retrieves user information from the provider
func (p *Auth0Provider) GetUserInfo(accessToken string) (*models.UserInfo, error) {
	userInfoURL := fmt.Sprintf("https://%s/userinfo", p.domain)

	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user info: %s", string(body))
	}

	var userInfo models.UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	return &userInfo, nil
}

// Close releases any resources held by the provider
func (p *Auth0Provider) Close() {
	// Nothing to close for Auth0
}

// Polling error types
var (
	ErrAuthorizationPending = fmt.Errorf("authorization_pending")
	ErrSlowDown             = fmt.Errorf("slow_down")
)
