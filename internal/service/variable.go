package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// VariableScope identifies which scope a variable belongs to. Use the
// constants below — the strings match the CLI surface.
type VariableScope string

const (
	VarScopeOrg      VariableScope = "org"
	VarScopeOU       VariableScope = "ou"
	VarScopeTemplate VariableScope = "template"
	VarScopeDevice   VariableScope = "device"
)

// IsValid reports whether the scope is a recognised value.
func (sc VariableScope) IsValid() bool {
	switch sc {
	case VarScopeOrg, VarScopeOU, VarScopeTemplate, VarScopeDevice:
		return true
	}
	return false
}

// RequiresEntity is true for OU/Template/Device scopes, which need an entity
// name (e.g. an OU name) in addition to the variable name.
func (sc VariableScope) RequiresEntity() bool {
	return sc != VarScopeOrg
}

// scopeBase returns the URL prefix for the variable collection at this
// scope+entity. Returns CodeInvalidInput if scope requires an entity but
// none was provided.
func variableScopeURL(scope VariableScope, org, entity string) (string, error) {
	switch scope {
	case VarScopeOrg:
		return fmt.Sprintf("/api/v1/organizations/%s/variables", url.PathEscape(org)), nil
	case VarScopeOU:
		if entity == "" {
			return "", &Error{Code: CodeInvalidInput, Message: "OU name is required for ou-scope variables"}
		}
		return fmt.Sprintf("/api/v1/organizations/%s/ous/%s/variables", url.PathEscape(org), url.PathEscape(entity)), nil
	case VarScopeTemplate:
		if entity == "" {
			return "", &Error{Code: CodeInvalidInput, Message: "template name is required for template-scope variables"}
		}
		return fmt.Sprintf("/api/v1/organizations/%s/templates/%s/variables", url.PathEscape(org), url.PathEscape(entity)), nil
	case VarScopeDevice:
		if entity == "" {
			return "", &Error{Code: CodeInvalidInput, Message: "device name is required for device-scope variables"}
		}
		return fmt.Sprintf("/api/v1/organizations/%s/devices/%s/variables", url.PathEscape(org), url.PathEscape(entity)), nil
	default:
		return "", &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("unknown variable scope: %s", scope)}
	}
}

// VariableListOpts collects optional filters for VariableList.
type VariableListOpts struct {
	NameFilter string
	Page       int
	PerPage    int
}

// VariableListResult mirrors a paginated variable list with resolved defaults.
type VariableListResult struct {
	Variables []models.Variable
	Total     int
	Page      int
	PerPage   int
}

// VariableList returns variables defined at a given scope/entity.
func (s *Service) VariableList(ctx context.Context, scope VariableScope, org, entity string, opts VariableListOpts) (*VariableListResult, error) {
	base, err := variableScopeURL(scope, org, entity)
	if err != nil {
		return nil, err
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.NameFilter != "" {
		params["name_filter"] = opts.NameFilter
	}
	resp, err := s.api.Get(ctx, base, params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VariableListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &VariableListResult{
		Variables: result.Items,
		Total:     result.Total,
		Page:      page,
		PerPage:   perPage,
	}, nil
}

// VariableGet returns a single variable definition.
func (s *Service) VariableGet(ctx context.Context, scope VariableScope, org, entity, name string) (*models.Variable, error) {
	base, err := variableScopeURL(scope, org, entity)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "variable name is required"}
	}
	resp, err := s.api.Get(ctx, base+"/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var v models.Variable
	if err := api.ParseResponse(resp, &v); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &v, nil
}

// VariableCreateOpts holds the fields for creating a variable. Secret is
// only honoured by the org scope; the server rejects it elsewhere.
type VariableCreateOpts struct {
	Name        string
	Value       string
	Description string
	Secret      bool
}

// VariableCreate creates a new variable at the given scope.
func (s *Service) VariableCreate(ctx context.Context, scope VariableScope, org, entity string, opts VariableCreateOpts) (*models.Variable, error) {
	base, err := variableScopeURL(scope, org, entity)
	if err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "variable name is required"}
	}
	payload := map[string]interface{}{
		"name":  opts.Name,
		"value": opts.Value,
	}
	if opts.Description != "" {
		payload["description"] = opts.Description
	}
	if opts.Secret && scope == VarScopeOrg {
		payload["secret"] = true
	}
	resp, err := s.api.Post(ctx, base, payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var v models.Variable
	if err := api.ParseResponse(resp, &v); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &v, nil
}

// VariableSetOpts holds optional fields for VariableSet. Either Value or
// Description (or both) must be non-nil.
type VariableSetOpts struct {
	Value       *string
	Description *string
}

// VariableSet patches an existing variable.
func (s *Service) VariableSet(ctx context.Context, scope VariableScope, org, entity, name string, opts VariableSetOpts) (*models.Variable, error) {
	base, err := variableScopeURL(scope, org, entity)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "variable name is required"}
	}
	if opts.Value == nil && opts.Description == nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "at least one of value or description must be provided"}
	}
	payload := map[string]interface{}{}
	if opts.Value != nil {
		payload["value"] = *opts.Value
	}
	if opts.Description != nil {
		payload["description"] = *opts.Description
	}
	resp, err := s.api.Patch(ctx, base+"/"+url.PathEscape(name), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var v models.Variable
	if err := api.ParseResponse(resp, &v); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &v, nil
}

// VariableDelete removes a variable definition. Caller is responsible for
// any user-facing confirmation.
func (s *Service) VariableDelete(ctx context.Context, scope VariableScope, org, entity, name string) error {
	base, err := variableScopeURL(scope, org, entity)
	if err != nil {
		return err
	}
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "variable name is required"}
	}
	resp, err := s.api.Delete(ctx, base+"/"+url.PathEscape(name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// VariableOverviewOpts collects filters for the cross-scope overview.
type VariableOverviewOpts struct {
	NameFilter string
	Page       int
	PerPage    int
}

// VariableOverviewResult mirrors the paginated overview response.
type VariableOverviewResult struct {
	Variables []models.VariableOverview
	Total     int
	Page      int
	PerPage   int
}

// VariableOverview returns the cross-scope view of every variable in an org
// (one entry per name, with all its definitions across scopes).
func (s *Service) VariableOverview(ctx context.Context, org string, opts VariableOverviewOpts) (*VariableOverviewResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}
	params := map[string]string{
		"scope":    "all",
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.NameFilter != "" {
		params["name"] = opts.NameFilter
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/variables", url.PathEscape(org)), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VariableOverviewResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &VariableOverviewResult{
		Variables: result.Items,
		Total:     result.Total,
		Page:      page,
		PerPage:   perPage,
	}, nil
}

// VariableCountNonOrgDefinitions returns how many non-org-scope definitions
// exist for a single variable name. Used by the CLI to warn before
// cascade-deleting an org variable.
func (s *Service) VariableCountNonOrgDefinitions(ctx context.Context, org, name string) int {
	if name == "" {
		return 0
	}
	overview, err := s.VariableOverview(ctx, org, VariableOverviewOpts{
		NameFilter: fmt.Sprintf("^%s$", name),
	})
	if err != nil {
		return 0
	}
	for _, v := range overview.Variables {
		if v.Name != name {
			continue
		}
		count := 0
		for _, def := range v.Definitions {
			if def.Scope != "organization" {
				count++
			}
		}
		return count
	}
	return 0
}
