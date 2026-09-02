package oauth2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/storage"
)

func TestNewTokenManager_HonorsCustomPath(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "tokens.json")

	tm := NewTokenManager(customPath)

	tokens := &models.TokenResponse{
		AccessToken: "tok-custom-path",
		ExpiresIn:   3600,
		TokenType:   "Bearer",
	}
	if err := tm.SaveTokens(tokens, nil, nil); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir(%s): %v", dir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected a token file to be written under %s (custom path %s), found none", dir, customPath)
	}

	loaded, err := tm.LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if loaded == nil || loaded.AccessToken != "tok-custom-path" {
		t.Fatalf("expected round-tripped access token %q from isolated custom-path storage, got %+v", "tok-custom-path", loaded)
	}
}

// TestSaveTokensOmitsUnreadFields locks in the payload diet that keeps a
// session inside the per-secret size cap of the macOS and Windows keyrings.
// The id_token is roughly 40% of an Auth0 bundle and nothing in ndcli or
// netdefense-mcp ever reads it back.
func TestSaveTokensOmitsUnreadFields(t *testing.T) {
	dir := t.TempDir()
	tm := NewTokenManager(filepath.Join(dir, "tokens.json"))

	tokens := &models.TokenResponse{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
		IDToken:      "id-token-value-that-nothing-reads",
		ExpiresIn:    3600,
		TokenType:    "Bearer",
	}
	userInfo := &models.UserInfo{
		Subject:       "auth0|0123456789abcdef01234567",
		Email:         "alice@example.com",
		EmailVerified: true,
		Name:          "Alice Example",
		Nickname:      "alice",
		Picture:       "https://s.gravatar.com/avatar/0123456789abcdef0123456789abcdef?s=480&r=pg&d=https%3A%2F%2Fcdn.auth0.com%2Favatars%2Fal.png",
	}

	if err := tm.SaveTokens(tokens, userInfo, nil); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected a token file under %s: %v", dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}

	for _, unwanted := range []string{"id-token-value-that-nothing-reads", "id_token", "gravatar", "picture", "nickname", "email_verified"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("persisted document still carries %q; it is never read and inflates the keyring secret", unwanted)
		}
	}

	loaded, err := tm.LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if loaded.AccessToken != "access-token-value" || loaded.RefreshToken != "refresh-token-value" {
		t.Errorf("the fields ndcli actually uses must survive the round trip, got %+v", loaded)
	}
	if loaded.UserInfo == nil || loaded.UserInfo.Email != "alice@example.com" || loaded.UserInfo.Name != "Alice Example" {
		t.Errorf("identity fields must survive the round trip, got %+v", loaded.UserInfo)
	}
}

// TestUpdateAccessTokenRenarrowsLegacyProfile covers the upgrade path: a
// document written by an older release still carries the wide profile, and the
// first token refresh after upgrading has to shed it rather than rewrite it.
func TestUpdateAccessTokenRenarrowsLegacyProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	tm := NewTokenManager(path)

	legacy := models.StoredTokens{
		AccessToken:  "old-access",
		RefreshToken: "keep-this-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		UserInfo: &models.UserInfo{
			Email:   "alice@example.com",
			Name:    "Alice Example",
			Picture: "https://cdn.example.com/a-very-long-avatar-url-that-nothing-reads.png",
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Simulate the id_token key an older release would have written.
	data = []byte(strings.Replace(string(data), `{"access_token"`, `{"id_token":"stale-id-token","access_token"`, 1))
	if err := storage.NewFileStorage(path).Save(data, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := tm.UpdateAccessToken(&models.TokenResponse{
		AccessToken: "new-access",
		IDToken:     "new-id-token",
		ExpiresIn:   3600,
		TokenType:   "Bearer",
	}); err != nil {
		t.Fatalf("UpdateAccessToken: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected a token file under %s: %v", dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	for _, unwanted := range []string{"stale-id-token", "new-id-token", "id_token", "picture"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("refresh left %q in the document; the upgrade should shrink it", unwanted)
		}
	}

	loaded, err := tm.LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if loaded.AccessToken != "new-access" || loaded.RefreshToken != "keep-this-refresh" {
		t.Errorf("refresh must keep the session usable, got %+v", loaded)
	}
}
