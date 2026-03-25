package oauth2

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// mockProvider implements providers.Provider for testing
type mockProvider struct {
	refreshCount atomic.Int32
	refreshDelay time.Duration
	refreshErr   error
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) RequestDeviceAuthorization(scopes string) (*models.DeviceAuthResponse, error) {
	return nil, nil
}
func (m *mockProvider) PollForToken(deviceCode string, interval int) (*models.TokenResponse, error) {
	return nil, nil
}
func (m *mockProvider) RefreshToken(refreshToken string) (*models.TokenResponse, error) {
	m.refreshCount.Add(1)
	if m.refreshDelay > 0 {
		time.Sleep(m.refreshDelay)
	}
	if m.refreshErr != nil {
		return nil, m.refreshErr
	}
	return &models.TokenResponse{
		AccessToken: "new-access-token",
		ExpiresIn:   3600,
		TokenType:   "Bearer",
	}, nil
}
func (m *mockProvider) RevokeToken(token, tokenTypeHint string) error { return nil }
func (m *mockProvider) GetUserInfo(accessToken string) (*models.UserInfo, error) {
	return nil, nil
}
func (m *mockProvider) Close() {}

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	data          []byte
	saveCount     atomic.Int32
	credentialKey string
}

// saveRaw is a test helper that serializes StoredTokens directly to mock storage
func (tm *TokenManager) saveRaw(tokens *models.StoredTokens) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return tm.storage.Save(data, "")
}

func (m *mockStorage) Save(data []byte, credentialKey string) error {
	m.saveCount.Add(1)
	m.data = data
	return nil
}
func (m *mockStorage) Load() ([]byte, error)          { return m.data, nil }
func (m *mockStorage) Clear() error                    { return nil }
func (m *mockStorage) Name() string                    { return "mock" }
func (m *mockStorage) GetCurrentCredentialKey() string { return m.credentialKey }

func TestConcurrentRefreshOnlyRefreshesOnce(t *testing.T) {
	mock := &mockProvider{
		refreshDelay: 100 * time.Millisecond,
	}

	store := &mockStorage{}
	tm := &TokenManager{storage: store}

	// Store expired tokens with a refresh token
	expiredTokens := models.StoredTokens{
		AccessToken:  "expired-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour), // expired
	}
	if err := tm.saveRaw(&expiredTokens); err != nil {
		t.Fatalf("failed to save expired tokens: %v", err)
	}

	client := &Client{
		domain:       "test.auth0.com",
		clientID:     "test-client-id",
		provider:     mock,
		tokenManager: tm,
	}

	// Launch 10 concurrent refresh attempts
	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = client.ForceRefresh()
		}(i)
	}
	wg.Wait()

	// All should succeed
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d got error: %v", i, err)
		}
	}

	// The provider should have been called exactly once due to double-check pattern
	count := mock.refreshCount.Load()
	if count != 1 {
		t.Errorf("expected 1 refresh call, got %d", count)
	}
}

func TestSequentialRefreshWorks(t *testing.T) {
	mock := &mockProvider{}
	store := &mockStorage{}
	tm := &TokenManager{storage: store}

	expiredTokens := models.StoredTokens{
		AccessToken:  "expired-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}
	if err := tm.saveRaw(&expiredTokens); err != nil {
		t.Fatalf("failed to save expired tokens: %v", err)
	}

	client := &Client{
		domain:       "test.auth0.com",
		clientID:     "test-client-id",
		provider:     mock,
		tokenManager: tm,
	}

	if err := client.ForceRefresh(); err != nil {
		t.Errorf("ForceRefresh failed: %v", err)
	}

	if count := mock.refreshCount.Load(); count != 1 {
		t.Errorf("expected 1 refresh call, got %d", count)
	}
}

func TestGetAccessTokenRefreshesExpiredToken(t *testing.T) {
	mock := &mockProvider{}
	store := &mockStorage{}
	tm := &TokenManager{storage: store}

	expiredTokens := models.StoredTokens{
		AccessToken:  "expired-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}
	if err := tm.saveRaw(&expiredTokens); err != nil {
		t.Fatalf("failed to save expired tokens: %v", err)
	}

	client := &Client{
		domain:       "test.auth0.com",
		clientID:     "test-client-id",
		provider:     mock,
		tokenManager: tm,
	}

	token, err := client.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}

	if token != "new-access-token" {
		t.Errorf("expected 'new-access-token', got %q", token)
	}
}

func TestGetAccessTokenReturnsValidToken(t *testing.T) {
	mock := &mockProvider{}
	store := &mockStorage{}
	tm := &TokenManager{storage: store}

	validTokens := models.StoredTokens{
		AccessToken:  "valid-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour), // not expired
	}
	if err := tm.saveRaw(&validTokens); err != nil {
		t.Fatalf("failed to save tokens: %v", err)
	}

	client := &Client{
		domain:       "test.auth0.com",
		clientID:     "test-client-id",
		provider:     mock,
		tokenManager: tm,
	}

	token, err := client.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}

	if token != "valid-token" {
		t.Errorf("expected 'valid-token', got %q", token)
	}

	// Provider should NOT have been called since token is valid
	if count := mock.refreshCount.Load(); count != 0 {
		t.Errorf("expected 0 refresh calls for valid token, got %d", count)
	}
}
