package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// TemplateListOpts mirrors the filters for the templates list endpoint.
type TemplateListOpts struct {
	SortBy        string
	Page          int
	PerPage       int
	CreatedAfter  string
	CreatedBefore string
	UpdatedAfter  string
	UpdatedBefore string
}

// TemplateListResult mirrors the paginated template list with resolved
// pagination defaults.
type TemplateListResult struct {
	Templates []models.Template
	Total     int
	Page      int
	PerPage   int
}

// TemplateList returns a paginated list of templates.
func (s *Service) TemplateList(ctx context.Context, org string, opts TemplateListOpts) (*TemplateListResult, error) {
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
	for _, f := range [][2]string{
		{opts.CreatedAfter, "created_after"},
		{opts.CreatedBefore, "created_before"},
		{opts.UpdatedAfter, "updated_after"},
		{opts.UpdatedBefore, "updated_before"},
	} {
		if f[0] == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f[0])
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f[1], err),
				Err:     err,
			}
		}
		params[f[1]] = parsed
	}

	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.TemplateListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &TemplateListResult{
		Templates: result.Items,
		Total:     result.Total,
		Page:      page,
		PerPage:   perPage,
	}, nil
}

// TemplateGet returns a single template (with its snippets).
func (s *Service) TemplateGet(ctx context.Context, org, name string) (*models.Template, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "template name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var t models.Template
	if err := api.ParseResponse(resp, &t); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &t, nil
}

// TemplateCreateOpts holds the fields needed to create a template.
type TemplateCreateOpts struct {
	Name        string
	Description string
	Position    string // PREPEND (default) or APPEND
}

// TemplateCreate creates a new template.
func (s *Service) TemplateCreate(ctx context.Context, org string, opts TemplateCreateOpts) (*models.Template, error) {
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "template name is required"}
	}
	payload := map[string]string{"name": opts.Name}
	if opts.Description != "" {
		payload["description"] = opts.Description
	}
	if opts.Position != "" {
		payload["position"] = opts.Position
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var t models.Template
	if err := api.ParseResponse(resp, &t); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &t, nil
}

// TemplateUpdateOpts collects the optional fields TemplateUpdate may change.
// The server splits rename (NewName) into a separate PUT endpoint from the
// PATCH that handles description/position; the service hides that detail.
type TemplateUpdateOpts struct {
	NewName     string
	Description string
	Position    string
}

// TemplateUpdate applies any combination of rename/description/position
// changes. At least one field must be set; otherwise CodeInvalidInput is
// returned. The returned name is the post-rename name (== input name when
// rename wasn't requested).
func (s *Service) TemplateUpdate(ctx context.Context, org, name string, opts TemplateUpdateOpts) (string, error) {
	if name == "" {
		return "", &Error{Code: CodeInvalidInput, Message: "template name is required"}
	}
	if opts.NewName == "" && opts.Description == "" && opts.Position == "" {
		return "", &Error{Code: CodeInvalidInput, Message: "no updates specified (NewName, Description, or Position must be set)"}
	}

	current := name
	if opts.NewName != "" {
		resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/rename", org, current), map[string]string{"new_name": opts.NewName})
		if err != nil {
			return current, wrapAPI("%v", err)
		}
		if err := api.ParseResponse(resp, nil); err != nil {
			return current, wrapAPI("%v", err)
		}
		current = opts.NewName
	}

	if opts.Description != "" || opts.Position != "" {
		payload := map[string]string{}
		if opts.Description != "" {
			payload["description"] = opts.Description
		}
		if opts.Position != "" {
			payload["position"] = opts.Position
		}
		resp, err := s.api.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, current), payload)
		if err != nil {
			return current, wrapAPI("%v", err)
		}
		if err := api.ParseResponse(resp, nil); err != nil {
			return current, wrapAPI("%v", err)
		}
	}

	return current, nil
}

// TemplateDelete removes a template.
func (s *Service) TemplateDelete(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "template name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// TemplateAddSnippet attaches a snippet to a template.
func (s *Service) TemplateAddSnippet(ctx context.Context, org, templateName, snippetName string) error {
	if templateName == "" || snippetName == "" {
		return &Error{Code: CodeInvalidInput, Message: "template and snippet names are required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/snippets", org, templateName), map[string]string{"snippet_name": snippetName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// TemplateRemoveSnippet detaches a snippet from a template.
func (s *Service) TemplateRemoveSnippet(ctx context.Context, org, templateName, snippetName string) error {
	if templateName == "" || snippetName == "" {
		return &Error{Code: CodeInvalidInput, Message: "template and snippet names are required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/snippets/%s", org, templateName, snippetName))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
