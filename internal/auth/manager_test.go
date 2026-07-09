package auth

import (
	"strings"
	"sync"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

func TestValidateOAuth2Domain(t *testing.T) {
	tests := []struct {
		name         string
		serverDomain string
		wantErr      bool
	}{
		{"matches default domain", config.DefaultOAuth2Domain, false},
		{"mismatched domain rejected", "attacker.example.com", true},
		{"empty domain rejected", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOAuth2Domain(tt.serverDomain)
			if tt.wantErr && err == nil {
				t.Fatalf("validateOAuth2Domain(%q): expected error, got nil", tt.serverDomain)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateOAuth2Domain(%q): expected no error, got %v", tt.serverDomain, err)
			}
			if tt.wantErr {
				msg := err.Error()
				if tt.serverDomain != "" && !strings.Contains(msg, tt.serverDomain) {
					t.Errorf("error %q does not mention server domain %q", msg, tt.serverDomain)
				}
				if !strings.Contains(msg, config.DefaultOAuth2Domain) {
					t.Errorf("error %q does not mention expected domain %q", msg, config.DefaultOAuth2Domain)
				}
			}
		})
	}
}

func TestValidateOAuth2Domain_HonorsConfiguredDomain(t *testing.T) {
	original := config.Get().OAuth2.Domain
	defer func() { config.Get().OAuth2.Domain = original }()

	config.Get().OAuth2.Domain = "auth.selfhosted.example.com"

	if err := validateOAuth2Domain("auth.selfhosted.example.com"); err != nil {
		t.Fatalf("expected explicitly configured domain to be honored, got error: %v", err)
	}
	if err := validateOAuth2Domain(config.DefaultOAuth2Domain); err == nil {
		t.Fatal("expected default domain to be rejected once a custom oauth2.domain is configured")
	}
}

func TestGetManager_ConcurrentSingleton(t *testing.T) {
	const n = 50
	results := make([]*Manager, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = GetManager()
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("GetManager() returned nil")
	}
	for i, m := range results {
		if m != first {
			t.Fatalf("GetManager() returned a distinct instance at index %d: %p != %p", i, m, first)
		}
	}
}
