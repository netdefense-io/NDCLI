package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// CLIConfigResponse represents the response from /api/v1/cli/config
type CLIConfigResponse struct {
	OAuth2 struct {
		Domain   string `json:"domain"`
		ClientID string `json:"client_id"`
	} `json:"oauth2"`
}

// FetchCLIConfig fetches the CLI configuration from NDManager.
// This is an unauthenticated call used before login to get OAuth2 settings.
func FetchCLIConfig(ctx context.Context) (*CLIConfigResponse, error) {
	cfg := config.Get()

	// Build URL with version parameter
	url := fmt.Sprintf("%s/api/v1/cli/config?version=%s", cfg.Controlplane.Host, config.Version)

	// Create HTTP client (respects SSL verify setting)
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !cfg.Controlplane.SSLVerify,
			},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf("NDCLI-Go/%s", config.Version))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach NDManager at %s: %w", cfg.Controlplane.Host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch CLI config (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var cliConfig CLIConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cliConfig); err != nil {
		return nil, fmt.Errorf("failed to parse CLI config response: %w", err)
	}

	// Validate required fields
	if cliConfig.OAuth2.Domain == "" {
		return nil, fmt.Errorf("NDManager returned empty oauth2.domain")
	}
	if cliConfig.OAuth2.ClientID == "" {
		return nil, fmt.Errorf("NDManager returned empty oauth2.client_id")
	}

	return &cliConfig, nil
}
