package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type templateListInput struct {
	Organization  string `json:"organization,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
}

type templateIDInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type templateCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Position     string `json:"position,omitempty"`
}

type templateUpdateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	NewName      string `json:"new_name,omitempty"`
	Description  string `json:"description,omitempty"`
	Position     string `json:"position,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type templateSnippetLinkInput struct {
	Organization string `json:"organization,omitempty"`
	Template     string `json:"template"`
	Snippet      string `json:"snippet"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// registerTemplateTools registers every template tool.
func (s *Server) registerTemplateTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.list",
		Description: "List templates in an organization with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"sort_by":        stringProperty("Sort field and direction (default name:asc)"),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page", 30),
				"created_after":  stringProperty("Filter by created date after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"created_before": stringProperty("Filter by created date before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"updated_after":  stringProperty("Filter by updated date after"),
				"updated_before": stringProperty("Filter by updated date before"),
			},
		},
	}, s.handleTemplateList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.describe",
		Description: "Get a template's metadata and the snippets it contains.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Template name"),
			},
			"required": []string{"name"},
		},
	}, s.handleTemplateDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.create",
		Description: "Create a new template. Position controls where its snippets are inserted (PREPEND default).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Template name"),
				"description":  stringProperty("Description (optional)"),
				"position":     stringEnumProperty("Snippet position (default PREPEND)", []string{"PREPEND", "APPEND"}),
			},
			"required": []string{"name"},
		},
	}, s.handleTemplateCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.update",
		Description: "Update a template. At least one of new_name/description/position must be set. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Current template name"),
				"new_name":     stringProperty("New name (rename)"),
				"description":  stringProperty("New description"),
				"position":     stringEnumProperty("New position", []string{"PREPEND", "APPEND"}),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleTemplateUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.delete",
		Description: "Delete a template. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Template name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleTemplateDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.add_snippet",
		Description: "Attach a snippet to a template.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"template":     stringProperty("Template name"),
				"snippet":      stringProperty("Snippet name"),
			},
			"required": []string{"template", "snippet"},
		},
	}, s.handleTemplateAddSnippet)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.remove_snippet",
		Description: "Detach a snippet from a template. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"template":     stringProperty("Template name"),
				"snippet":      stringProperty("Snippet name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"template", "snippet"},
		},
	}, s.handleTemplateRemoveSnippet)
}

func (s *Server) handleTemplateList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.TemplateList(apiCtx, org, service.TemplateListOpts{
		SortBy:        input.SortBy,
		Page:          input.Page,
		PerPage:       input.PerPage,
		CreatedAfter:  input.CreatedAfter,
		CreatedBefore: input.CreatedBefore,
		UpdatedAfter:  input.UpdatedAfter,
		UpdatedBefore: input.UpdatedBefore,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.Templates))
	for _, t := range result.Templates {
		items = append(items, templateSummary(&t))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"templates": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleTemplateDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	t, err := s.svc.TemplateGet(apiCtx, org, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template": templateFull(t),
	}, "")
}

func (s *Server) handleTemplateCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	t, err := s.svc.TemplateCreate(apiCtx, org, service.TemplateCreateOpts{
		Name:        input.Name,
		Description: input.Description,
		Position:    input.Position,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template": templateSummary(t),
		"action":   "created",
	}, fmt.Sprintf("Template '%s' created", input.Name))
}

func (s *Server) handleTemplateUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateUpdateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		changes := []string{}
		if input.NewName != "" {
			changes = append(changes, fmt.Sprintf("rename → %s", input.NewName))
		}
		if input.Description != "" {
			changes = append(changes, "description")
		}
		if input.Position != "" {
			changes = append(changes, fmt.Sprintf("position → %s", input.Position))
		}
		return s.previewResult("update template", fmt.Sprintf("%s [%v]", input.Name, changes))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	finalName, err := s.svc.TemplateUpdate(apiCtx, org, input.Name, service.TemplateUpdateOpts{
		NewName:     input.NewName,
		Description: input.Description,
		Position:    input.Position,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":       finalName,
		"renamed":    input.NewName != "",
		"old_name":   input.Name,
		"action":     "updated",
	}, fmt.Sprintf("Template updated: %s", finalName))
}

func (s *Server) handleTemplateDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete template", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TemplateDelete(apiCtx, org, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "deleted",
	}, fmt.Sprintf("Template '%s' deleted", input.Name))
}

func (s *Server) handleTemplateAddSnippet(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateSnippetLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TemplateAddSnippet(apiCtx, org, input.Template, input.Snippet); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template": input.Template,
		"snippet":  input.Snippet,
		"action":   "added",
	}, fmt.Sprintf("Snippet '%s' added to template '%s'", input.Snippet, input.Template))
}

func (s *Server) handleTemplateRemoveSnippet(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateSnippetLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove snippet from template", fmt.Sprintf("%s ← %s", input.Template, input.Snippet))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TemplateRemoveSnippet(apiCtx, org, input.Template, input.Snippet); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template": input.Template,
		"snippet":  input.Snippet,
		"action":   "removed",
	}, fmt.Sprintf("Snippet '%s' removed from template '%s'", input.Snippet, input.Template))
}

func templateSummary(t *models.Template) map[string]interface{} {
	return map[string]interface{}{
		"name":          t.Name,
		"description":   t.Description,
		"position":      t.Position,
		"snippet_count": t.SnippetCount,
		"created_at":    t.CreatedAt,
		"updated_at":    t.UpdatedAt,
		"created_by":    t.CreatedBy,
	}
}

func templateFull(t *models.Template) map[string]interface{} {
	full := templateSummary(t)
	snippets := make([]map[string]interface{}, 0, len(t.Snippets))
	for _, sn := range t.Snippets {
		snippets = append(snippets, map[string]interface{}{
			"name":     sn.Name,
			"type":     sn.Type,
			"priority": sn.Priority,
		})
	}
	full["snippets"] = snippets
	return full
}
