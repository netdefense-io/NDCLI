// Package service provides the shared application layer for ndcli and the
// MCP server. Each domain (device, org, ou, ...) exposes typed methods that
// take resolved arguments and return typed results — no cobra, no stdout, no
// JSON wrapping. Both front-ends are thin wrappers over this package.
package service

import (
	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
)

// Service is the entry point for all domain operations.
type Service struct {
	api  *api.Client
	auth *auth.Manager
	cfg  *config.Config
}

// New constructs a Service from already-initialised dependencies. Callers own
// the lifetime of the auth manager.
func New(apiClient *api.Client, authMgr *auth.Manager, cfg *config.Config) *Service {
	return &Service{
		api:  apiClient,
		auth: authMgr,
		cfg:  cfg,
	}
}

// API returns the underlying API client. Exposed for callers that still need
// raw access during the migration; new code should prefer typed service
// methods.
func (s *Service) API() *api.Client { return s.api }

// Config returns the loaded configuration.
func (s *Service) Config() *config.Config { return s.cfg }

// RequireAuth verifies that a valid access token is available, refreshing it
// if necessary. Returns a typed *Error so callers can map it to their own
// surface (CLI message, MCP error code).
func (s *Service) RequireAuth() error {
	if s.auth == nil || !s.auth.IsAuthenticated() {
		return &Error{
			Code:    CodeNotAuthenticated,
			Message: "Not authenticated. Please run 'ndcli auth login' first.",
		}
	}
	if _, err := s.auth.GetAccessToken(); err != nil {
		return &Error{
			Code:    CodeAuthFailed,
			Message: "Authentication failed. Please run 'ndcli auth login' to re-authenticate.",
			Err:     err,
		}
	}
	return nil
}

// ResolveOrg returns the organization to use, falling back to the configured
// default when input is empty.
func (s *Service) ResolveOrg(input string) (string, error) {
	if input != "" {
		return input, nil
	}
	if s.cfg != nil && s.cfg.Organization.Name != "" {
		return s.cfg.Organization.Name, nil
	}
	return "", &Error{
		Code:    CodeOrgRequired,
		Message: "Organization is required. Provide --org or set organization.name in config.",
	}
}
