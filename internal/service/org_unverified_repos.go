package service

import (
	"context"
	"fmt"
	"net/url"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// unverifiedReposAnswer is the server's answer for the setting, read and
// write alike.
type unverifiedReposAnswer struct {
	OrganizationName string `json:"organization_name"`
	Allow            *bool  `json:"allow_unverified_repos"`
}

func unverifiedReposPath(org string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/allow-unverified-repos", url.PathEscape(org))
}

// unverifiedReposError turns the bare 404 of a server without the route into
// SERVER_TOO_OLD; every refusal of the route itself carries a code.
func unverifiedReposError(err error) error {
	if api.IsNotFoundError(err) && api.GetErrorCode(err) == "" {
		return &Error{Code: CodeServerTooOld, Message: "this server predates the unverified repositories setting", Err: err}
	}
	return wrapAPI("%v", err)
}

// OrgUnverifiedRepos reads whether the organization's software policies may
// declare unsigned or non-https repositories. Any member may read it.
func (s *Service) OrgUnverifiedRepos(ctx context.Context, org string) (bool, error) {
	if org == "" {
		return false, &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Get(ctx, unverifiedReposPath(org), nil)
	if err != nil {
		return false, wrapAPI("%v", err)
	}
	var answer unverifiedReposAnswer
	if err := api.ParseResponse(resp, &answer); err != nil {
		return false, unverifiedReposError(err)
	}
	if answer.Allow == nil {
		return false, &Error{Code: CodeAPIError, Message: "the server answered without the unverified repositories setting"}
	}
	return *answer.Allow, nil
}

// OrgSetUnverifiedRepos allows or forbids unsigned and non-https repositories
// in the organization's software policies. The server lets only an SU change
// it, and a personal access token never carries that clearance; its refusal is
// returned as sent.
func (s *Service) OrgSetUnverifiedRepos(ctx context.Context, org string, allow bool) error {
	if org == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Put(ctx, unverifiedReposPath(org), map[string]bool{"enabled": allow})
	if err != nil {
		return wrapAPI("%v", err)
	}
	var answer unverifiedReposAnswer
	if err := api.ParseResponse(resp, &answer); err != nil {
		return unverifiedReposError(err)
	}
	if answer.Allow == nil || *answer.Allow != allow {
		return &Error{Code: CodeAPIError, Message: "the server did not confirm the unverified repositories setting"}
	}
	return nil
}

// OrgDescribe is the organization with the settings the describe views show
// beside it. A setting that cannot be read is left unset, never guessed.
func (s *Service) OrgDescribe(ctx context.Context, name string) (*models.Organization, error) {
	org, err := s.OrgGet(ctx, name)
	if err != nil {
		return nil, err
	}
	if allow, err := s.OrgUnverifiedRepos(ctx, name); err == nil {
		org.AllowUnverifiedRepos = &allow
	}
	return org, nil
}
