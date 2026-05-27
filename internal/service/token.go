package service

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// TokenCreateOpts holds the parameters for creating a personal access token.
type TokenCreateOpts struct {
	Name      string
	Scope     string // "RW" or "RO"
	Org       string // optional
	ExpiresIn string // "30d", "60d", "90d", "180d", "365d", "never" — default "90d"
}

// TokenCreateResult carries the newly created token. The raw token value is
// only available once — the caller must surface it immediately to the user.
type TokenCreateResult struct {
	Token models.TokenCreateResponse
}

// TokenCreate creates a new personal access token via POST /api/v1/auth/tokens.
func (s *Service) TokenCreate(ctx context.Context, opts TokenCreateOpts) (*TokenCreateResult, error) {
	switch {
	case opts.Name == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "token name is required"}
	case opts.Scope != "RW" && opts.Scope != "RO":
		return nil, &Error{Code: CodeInvalidInput, Message: "scope must be RW or RO"}
	}
	if opts.ExpiresIn == "" {
		opts.ExpiresIn = "90d"
	}
	body := models.TokenCreateRequest{
		Name:      opts.Name,
		Scope:     opts.Scope,
		Org:       opts.Org,
		ExpiresIn: opts.ExpiresIn,
	}
	resp, err := s.api.Post(ctx, "/api/v1/auth/tokens", body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.TokenCreateResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &TokenCreateResult{Token: result}, nil
}

// TokenList returns all personal access tokens for the authenticated user
// via GET /api/v1/auth/tokens.
func (s *Service) TokenList(ctx context.Context) ([]models.PersonalAccessToken, error) {
	resp, err := s.api.Get(ctx, "/api/v1/auth/tokens", nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result []models.PersonalAccessToken
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return result, nil
}

// TokenRevoke deletes a personal access token by name via
// DELETE /api/v1/auth/tokens/{name}.
func (s *Service) TokenRevoke(ctx context.Context, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "token name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/auth/tokens/%s", name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if resp.StatusCode >= 400 {
		return wrapAPI("%v", api.ParseError(resp))
	}
	resp.Body.Close()
	return nil
}
