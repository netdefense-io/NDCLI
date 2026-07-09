package oauth2

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
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
