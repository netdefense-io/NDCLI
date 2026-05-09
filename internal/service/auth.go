package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// AuthIsAuthenticated reports whether a token is currently stored.
func (s *Service) AuthIsAuthenticated() bool {
	return s.auth != nil && s.auth.IsAuthenticated()
}

// AuthStorageName returns the human-readable name of the storage backend
// (e.g. "keyring", "file").
func (s *Service) AuthStorageName() string {
	if s.auth == nil {
		return ""
	}
	return s.auth.GetStorageName()
}

// AuthTokenSummary returns a redacted snapshot of the locally stored tokens
// (email, expiry, refresh availability, ...). Safe to surface to MCP.
func (s *Service) AuthTokenSummary() map[string]interface{} {
	if s.auth == nil {
		return nil
	}
	return s.auth.GetTokenSummary()
}

// AuthLocalUser returns the locally cached OAuth2 user info (no API call).
func (s *Service) AuthLocalUser() (*models.UserInfo, error) {
	if s.auth == nil {
		return nil, &Error{Code: CodeNotAuthenticated, Message: "no auth manager available"}
	}
	info, err := s.auth.GetUserInfo()
	if err != nil {
		return nil, &Error{Code: CodeAuthFailed, Message: err.Error(), Err: err}
	}
	return info, nil
}

// AuthMe fetches the authenticated user's profile + organization
// memberships from /api/v1/auth/me.
func (s *Service) AuthMe(ctx context.Context) (*models.AuthMe, error) {
	resp, err := s.api.Get(ctx, "/api/v1/auth/me", nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var me models.AuthMe
	if err := api.ParseResponse(resp, &me); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &me, nil
}

// AuthRefresh forces a token refresh.
func (s *Service) AuthRefresh() error {
	if s.auth == nil || !s.auth.IsAuthenticated() {
		return &Error{Code: CodeNotAuthenticated, Message: "not authenticated"}
	}
	if err := s.auth.ForceRefresh(); err != nil {
		return &Error{Code: CodeAuthFailed, Message: fmt.Sprintf("token refresh failed: %v", err), Err: err}
	}
	return nil
}

// AuthDeleteResult mirrors the DELETE /api/v1/auth/me response.
type AuthDeleteResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Email   string `json:"email"`
}

// AuthDelete permanently deletes the calling user's account. The underlying
// 409 response (sole-superuser blocking case) is preserved via the wrapped
// *api.APIError so callers can extract BlockingResources.
//
// CLI exposes this; MCP intentionally does NOT (account deletion behind an
// LLM-driven flow needs much stronger confirmation than a tool call).
func (s *Service) AuthDelete(ctx context.Context) (*AuthDeleteResult, error) {
	resp, err := s.api.Delete(ctx, "/api/v1/auth/me")
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	if resp.StatusCode == 409 {
		// Surface the structured 409 (sole-superuser) so the caller can
		// list blocking organisations.
		apiErr := api.ParseError(resp)
		return nil, &Error{
			Code:    CodeAPIError,
			Message: apiErr.Message,
			Err:     apiErr,
		}
	}
	var result AuthDeleteResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	// Wipe local tokens after successful deletion. Best-effort.
	if s.auth != nil {
		_ = s.auth.Logout()
	}
	return &result, nil
}

// AuthDeleteBlockingOrgs extracts the BlockingResources from the api.APIError
// wrapped inside the service.Error returned by AuthDelete on 409. Returns nil
// if the error isn't a sole-superuser conflict.
func AuthDeleteBlockingOrgs(err error) []string {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return nil
	}
	return apiErr.BlockingResources
}
