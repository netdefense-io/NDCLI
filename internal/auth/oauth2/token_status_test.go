package oauth2

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// clearTrackingStorage records Clear, which mockStorage discards, so a test
// can tell whether a logout actually removed the stored session.
type clearTrackingStorage struct {
	mockStorage
	cleared int
}

func (s *clearTrackingStorage) Clear() error {
	s.cleared++
	s.data = nil
	return nil
}

// revokeRecordingProvider records every RevokeToken call.
type revokeRecordingProvider struct {
	mockProvider
	mu      sync.Mutex
	revoked []string
}

func (p *revokeRecordingProvider) RevokeToken(token, tokenTypeHint string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked = append(p.revoked, tokenTypeHint+":"+token)
	return nil
}

func managerWith(t *testing.T, tokens *models.StoredTokens) *TokenManager {
	t.Helper()
	tm := &TokenManager{storage: &mockStorage{}}
	if tokens != nil {
		if err := tm.saveRaw(tokens); err != nil {
			t.Fatalf("seed tokens: %v", err)
		}
	}
	return tm
}

func TestTokenManagerSessionStatus(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-48 * time.Hour)
	identity := &models.UserInfo{Subject: "sub-1", Email: "alice@example.com", Name: "Alice"}

	tests := []struct {
		name        string
		tokens      *models.StoredTokens
		wantState   models.SessionState
		wantRefresh bool
	}{
		{"nothing stored", nil, models.SessionNone, false},
		{"empty document", &models.StoredTokens{}, models.SessionNone, false},
		{"unexpired access token with refresh token",
			&models.StoredTokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: future}, models.SessionValid, true},
		{"unexpired access token without refresh token",
			&models.StoredTokens{AccessToken: "a", ExpiresAt: future}, models.SessionValid, false},
		{"expired access token with refresh token",
			&models.StoredTokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: past}, models.SessionRefreshable, true},
		{"expired access token without refresh token",
			&models.StoredTokens{AccessToken: "a", ExpiresAt: past}, models.SessionExpired, false},
		{"inside the 60 s refresh buffer counts as expired",
			&models.StoredTokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(30 * time.Second)}, models.SessionRefreshable, true},
		{"refresh token only",
			&models.StoredTokens{RefreshToken: "r", ExpiresAt: future}, models.SessionRefreshable, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.tokens = withIdentity(tt.tokens, identity)
			st := managerWith(t, tt.tokens).SessionStatus()
			if st.State != tt.wantState {
				t.Fatalf("State = %q, want %q", st.State, tt.wantState)
			}
			if st.HasRefresh != tt.wantRefresh {
				t.Errorf("HasRefresh = %v, want %v", st.HasRefresh, tt.wantRefresh)
			}
			if tt.wantState == models.SessionNone {
				return
			}
			if st.Email != "alice@example.com" || st.Name != "Alice" || st.Subject != "sub-1" {
				t.Errorf("identity not carried through: %+v", st)
			}
			if !st.ExpiresAt.Equal(tt.tokens.ExpiresAt) {
				t.Errorf("ExpiresAt = %v, want %v", st.ExpiresAt, tt.tokens.ExpiresAt)
			}
		})
	}
}

func withIdentity(tokens *models.StoredTokens, u *models.UserInfo) *models.StoredTokens {
	if tokens != nil && (tokens.AccessToken != "" || tokens.RefreshToken != "") {
		tokens.UserInfo = u
	}
	return tokens
}

func TestTokenManagerSessionStatus_UnreadableDocumentIsNone(t *testing.T) {
	tm := &TokenManager{storage: &mockStorage{}}
	if err := tm.storage.Save([]byte("{not json"), ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if st := tm.SessionStatus(); st.State != models.SessionNone {
		t.Fatalf("State = %q, want none", st.State)
	}
}

// TestExpiredAccessTokenWithRefreshTokenIsAWorkingSession documents the
// semantics `auth show`, `auth logout` and `auth refresh` once got wrong:
// IsAuthenticated means "unexpired access token", and says nothing against a
// session that GetAccessToken renews on its own.
func TestExpiredAccessTokenWithRefreshTokenIsAWorkingSession(t *testing.T) {
	provider := &mockProvider{}
	client := &Client{
		provider: provider,
		tokenManager: managerWith(t, &models.StoredTokens{
			AccessToken:  "expired-token",
			RefreshToken: "valid-refresh-token",
			TokenType:    "Bearer",
			ExpiresAt:    time.Now().Add(-48 * time.Hour),
		}),
	}

	if client.IsAuthenticated() {
		t.Error("IsAuthenticated() is true for an expired access token; it reports unexpired access tokens only")
	}
	if st := client.SessionStatus(); st.State != models.SessionRefreshable {
		t.Fatalf("SessionStatus().State = %q, want refreshable", st.State)
	}
	if calls := provider.refreshCount.Load(); calls != 0 {
		t.Fatalf("classifying the session refreshed it (%d provider calls); it must be passive", calls)
	}

	token, err := client.GetAccessToken()
	if err != nil || token == "" {
		t.Fatalf("GetAccessToken() = %q, %v; want a renewed token", token, err)
	}
	if calls := provider.refreshCount.Load(); calls != 1 {
		t.Errorf("provider refreshed %d times, want 1", calls)
	}
	if !client.IsAuthenticated() {
		t.Error("IsAuthenticated() is still false after the renewal")
	}
	if st := client.SessionStatus(); st.State != models.SessionValid {
		t.Errorf("SessionStatus().State = %q after the renewal, want valid", st.State)
	}
}

func TestLogoutOfAnExpiredAccessTokenSessionRevokesAndClears(t *testing.T) {
	store := &clearTrackingStorage{}
	tm := &TokenManager{storage: store}
	if err := tm.saveRaw(&models.StoredTokens{
		AccessToken:  "expired-access",
		RefreshToken: "live-refresh",
		ExpiresAt:    time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	provider := &revokeRecordingProvider{}
	client := &Client{provider: provider, tokenManager: tm}

	if err := client.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if store.cleared != 1 {
		t.Errorf("storage cleared %d times, want 1", store.cleared)
	}
	if len(provider.revoked) == 0 || provider.revoked[0] != "refresh_token:live-refresh" {
		t.Errorf("the refresh token must be revoked first, got %v", provider.revoked)
	}
	if st := client.SessionStatus(); st.State != models.SessionNone {
		t.Errorf("SessionStatus().State = %q after logout, want none", st.State)
	}
}

// A stored document with no oauth2_config leaves the client without a provider.
// Logout used to dereference it, and now runs for sessions it never saw before.
func TestLogoutWithoutAProviderStillClears(t *testing.T) {
	store := &clearTrackingStorage{}
	tm := &TokenManager{storage: store}
	if err := tm.saveRaw(&models.StoredTokens{
		AccessToken:  "expired-access",
		RefreshToken: "live-refresh",
		ExpiresAt:    time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	client := &Client{tokenManager: tm}

	if err := client.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if store.cleared != 1 {
		t.Errorf("storage cleared %d times, want 1", store.cleared)
	}
}

func TestRefreshWithoutAProviderFailsCleanly(t *testing.T) {
	client := &Client{tokenManager: managerWith(t, &models.StoredTokens{
		AccessToken:  "expired-access",
		RefreshToken: "live-refresh",
		ExpiresAt:    time.Now().Add(-48 * time.Hour),
	})}

	err := client.ForceRefresh()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "login again") {
		t.Errorf("error should tell the user to log in again, got %q", err.Error())
	}
}

// An expired session with no refresh token is the one state only a login
// recovers. `auth refresh` reaches it without a pre-check now, so the failure
// has to come from here: local, naming the way out, and never a provider call.
func TestRefreshOfAnExpiredSessionWithoutARefreshTokenFailsLocally(t *testing.T) {
	provider := &mockProvider{}
	client := &Client{
		provider: provider,
		tokenManager: managerWith(t, &models.StoredTokens{
			AccessToken: "expired-access",
			ExpiresAt:   time.Now().Add(-48 * time.Hour),
		}),
	}
	if st := client.SessionStatus(); st.State != models.SessionExpired {
		t.Fatalf("SessionStatus().State = %q, want expired", st.State)
	}

	err := client.ForceRefresh()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "no refresh token") || !strings.Contains(err.Error(), "login again") {
		t.Errorf("error should say there is no refresh token and to log in again, got %q", err.Error())
	}
	if calls := provider.refreshCount.Load(); calls != 0 {
		t.Errorf("the provider was called %d times for a session that has no refresh token", calls)
	}
}
