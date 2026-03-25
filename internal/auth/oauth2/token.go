package oauth2

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/storage"
)

// TokenManager handles token storage and retrieval
type TokenManager struct {
	storage storage.Storage
}

// NewTokenManager creates a new token manager
func NewTokenManager(customPath string) *TokenManager {
	return &TokenManager{storage: storage.GetStorage()}
}

// SaveTokens saves tokens to storage
func (tm *TokenManager) SaveTokens(tokens *models.TokenResponse, userInfo *models.UserInfo, oauth2Config *models.StoredOAuth2Config) error {
	// Calculate expiration time
	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)

	stored := models.StoredTokens{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		IDToken:      tokens.IDToken,
		TokenType:    tokens.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        tokens.Scope,
		UserInfo:     userInfo,
		OAuth2Config: oauth2Config,
		CreatedAt:    time.Now(),
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	// Build composite credential key (email@host) for host-scoped storage
	credentialKey := ""
	if userInfo != nil && userInfo.Email != "" {
		host := config.Get().Controlplane.Host
		credentialKey = config.BuildCredentialKey(userInfo.Email, host)
	}

	if err := tm.storage.Save(data, credentialKey); err != nil {
		return fmt.Errorf("failed to save tokens: %w", err)
	}

	return nil
}

// LoadTokens loads tokens from storage
func (tm *TokenManager) LoadTokens() (*models.StoredTokens, error) {
	data, err := tm.storage.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load tokens: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	var tokens models.StoredTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse tokens: %w", err)
	}

	return &tokens, nil
}

// GetValidAccessToken returns the access token if it's still valid
func (tm *TokenManager) GetValidAccessToken() (string, error) {
	tokens, err := tm.LoadTokens()
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", nil
	}

	if tokens.IsExpired() {
		return "", nil
	}

	return tokens.AccessToken, nil
}

// GetRefreshToken returns the refresh token if available
func (tm *TokenManager) GetRefreshToken() (string, error) {
	tokens, err := tm.LoadTokens()
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", nil
	}

	return tokens.RefreshToken, nil
}

// Clear removes the stored tokens
func (tm *TokenManager) Clear() error {
	return tm.storage.Clear()
}

// GetTokenSummary returns a summary of the stored tokens
func (tm *TokenManager) GetTokenSummary() map[string]interface{} {
	tokens, err := tm.LoadTokens()
	if err != nil || tokens == nil {
		return nil
	}

	summary := map[string]interface{}{
		"expires_at":  tokens.ExpiresAt.Format(time.RFC3339),
		"is_expired":  tokens.IsExpired(),
		"has_refresh": tokens.RefreshToken != "",
		"scope":       tokens.Scope,
	}

	if tokens.UserInfo != nil {
		if tokens.UserInfo.Email != "" {
			summary["email"] = tokens.UserInfo.Email
		}
		if tokens.UserInfo.Name != "" {
			summary["name"] = tokens.UserInfo.Name
		}
		if tokens.UserInfo.Subject != "" {
			summary["subject"] = tokens.UserInfo.Subject
		}
	}

	return summary
}

// UpdateAccessToken updates just the access token and expiration
func (tm *TokenManager) UpdateAccessToken(tokens *models.TokenResponse) error {
	existing, err := tm.LoadTokens()
	if err != nil {
		return err
	}
	if existing == nil {
		// No existing tokens, save as new (without OAuth2 config - shouldn't happen)
		return tm.SaveTokens(tokens, nil, nil)
	}

	// Update access token and expiration
	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	existing.AccessToken = tokens.AccessToken
	existing.ExpiresAt = expiresAt

	// Update refresh token if a new one was provided
	if tokens.RefreshToken != "" {
		existing.RefreshToken = tokens.RefreshToken
	}

	// Update ID token if provided
	if tokens.IDToken != "" {
		existing.IDToken = tokens.IDToken
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	// Build composite credential key (email@host) for host-scoped storage
	credentialKey := ""
	if existing.UserInfo != nil && existing.UserInfo.Email != "" {
		host := config.Get().Controlplane.Host
		credentialKey = config.BuildCredentialKey(existing.UserInfo.Email, host)
	}

	return tm.storage.Save(data, credentialKey)
}

// StorageName returns the name of the storage backend being used
func (tm *TokenManager) StorageName() string {
	return tm.storage.Name()
}
