package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// OrgListOpts collects the filters NDManager's GET /api/v1/organizations
// accepts. Empty fields are omitted.
type OrgListOpts struct {
	SortBy  string
	Name    string
	Role    string // RO, RW, SU
	Status  string // ENABLED, DISABLED, INVITED, DECLINED
	Page    int
	PerPage int
}

// OrgListResult mirrors the paginated org list with resolved pagination.
type OrgListResult struct {
	Orgs    []models.Organization
	Total   int
	Page    int
	PerPage int
}

// OrgList returns a paginated, filtered list of organizations the caller can
// see.
func (s *Service) OrgList(ctx context.Context, opts OrgListOpts) (*OrgListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 30
	}

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}
	if opts.Name != "" {
		params["name"] = opts.Name
	}
	if opts.Role != "" {
		params["role"] = strings.ToUpper(opts.Role)
	}
	if opts.Status != "" {
		params["status"] = strings.ToUpper(opts.Status)
	}

	resp, err := s.api.Get(ctx, "/api/v1/organizations", params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.OrganizationListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &OrgListResult{
		Orgs:    result.GetItems(),
		Total:   result.GetTotal(),
		Page:    page,
		PerPage: perPage,
	}, nil
}

// OrgGet returns the full record for a single organization.
func (s *Service) OrgGet(ctx context.Context, name string) (*models.Organization, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s", name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &org, nil
}

// OrgCreate creates a new organization. Display name and description are
// optional; the caller decides whether to update the local default-org
// config after a successful create.
func (s *Service) OrgCreate(ctx context.Context, name, displayName, description string) (*models.Organization, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	payload := map[string]string{"name": name}
	if displayName != "" {
		payload["display_name"] = displayName
	}
	if description != "" {
		payload["description"] = description
	}
	resp, err := s.api.Post(ctx, "/api/v1/organizations", payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &org, nil
}

// OrgDelete removes an organization. Caller is responsible for any user-
// facing confirmation.
func (s *Service) OrgDelete(ctx context.Context, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s", name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgQuota returns the plan limits + current usage for an organization.
func (s *Service) OrgQuota(ctx context.Context, name string) (*models.OrgQuota, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/quota", name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var quota models.OrgQuota
	if err := api.ParseResponse(resp, &quota); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &quota, nil
}

// OrgSetDefaultOU sets the default OU for new devices in the organization.
func (s *Service) OrgSetDefaultOU(ctx context.Context, org, ouName string) error {
	if org == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	if ouName == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/default-ou", org), map[string]string{"ou_name": ouName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// NormalizeRole maps any of the long/short role aliases to the canonical
// SU/RW/RO. Returns CodeInvalidInput when the spelling isn't recognised.
func NormalizeRole(input string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "su", "superuser":
		return "SU", nil
	case "rw", "readwrite":
		return "RW", nil
	case "ro", "readonly":
		return "RO", nil
	default:
		return "", &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid role: %s. Valid roles: SU/superuser, RW/readwrite, RO/readonly", input),
		}
	}
}

// OrgInviteSend sends an invitation to the given email address. role is
// normalised via NormalizeRole.
func (s *Service) OrgInviteSend(ctx context.Context, org, email, role string) error {
	if org == "" || email == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization and email are required"}
	}
	normalized, err := NormalizeRole(role)
	if err != nil {
		return err
	}
	payload := map[string]string{"email": email, "role": normalized}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites", org), payload)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgInviteList returns both the invites the caller has received and (when
// they are a superuser) the ones they have sent.
func (s *Service) OrgInviteList(ctx context.Context) (*models.InvitesResponse, error) {
	resp, err := s.api.Get(ctx, "/api/v1/invites", map[string]string{"direction": "all"})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.InvitesResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// OrgInviteAccept accepts a pending invitation to join an organization.
func (s *Service) OrgInviteAccept(ctx context.Context, orgName string) error {
	if orgName == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/accept", orgName), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgInviteDecline declines a pending invitation.
func (s *Service) OrgInviteDecline(ctx context.Context, orgName string) error {
	if orgName == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/decline", orgName), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgInviteRevoke revokes a pending invitation the caller previously sent.
func (s *Service) OrgInviteRevoke(ctx context.Context, org, email string) error {
	if org == "" || email == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization and email are required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/%s", org, email))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgAccountListResult bundles the organization's accounts with the optional
// usage quota the API returns alongside.
type OrgAccountListResult struct {
	Accounts []models.Account
	Quota    *models.Quota
}

// OrgAccountList lists every account in the org.
func (s *Service) OrgAccountList(ctx context.Context, org string) (*OrgAccountListResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts", org), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.AccountListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &OrgAccountListResult{
		Accounts: result.Accounts,
		Quota:    result.Quota,
	}, nil
}

// OrgAccountDisable disables an account, optionally removing it permanently.
// Underlying api errors (NotFound, conflict) are preserved via Unwrap so
// callers can refine their UX.
func (s *Service) OrgAccountDisable(ctx context.Context, org, email string, remove bool) error {
	if org == "" || email == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization and email are required"}
	}
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/disable", org, email)
	if remove {
		endpoint += "?remove=true"
	}
	resp, err := s.api.Put(ctx, endpoint, nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgAccountEnable re-enables a previously disabled account.
func (s *Service) OrgAccountEnable(ctx context.Context, org, email string) error {
	if org == "" || email == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization and email are required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/enable", org, email), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OrgAccountSetRole sets the role for an existing account. role is
// normalised via NormalizeRole.
func (s *Service) OrgAccountSetRole(ctx context.Context, org, email, role string) error {
	if org == "" || email == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization and email are required"}
	}
	normalized, err := NormalizeRole(role)
	if err != nil {
		return err
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/role", org, email), map[string]string{"role": normalized})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
