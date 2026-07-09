package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/sanitize"
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
		// This call runs unauthenticated, before login, so a hostile or
		// misbehaving NDManager can respond with an arbitrary body here.
		// Cobra doesn't SilenceErrors, so this string reaches the
		// operator's terminal verbatim — scrub terminal control bytes
		// before splicing the body into the error message, matching
		// ParseError's handling of APIError fields.
		body, _ := ReadBody(resp.Body)
		return nil, fmt.Errorf("failed to fetch CLI config (HTTP %d): %s", resp.StatusCode, sanitize.String(string(body)))
	}

	var cliConfig CLIConfigResponse
	if err := DecodeJSON(resp.Body, &cliConfig); err != nil {
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
