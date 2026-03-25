package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// TemplateListInput for template list tool
type TemplateListInput struct {
	Organization  string `json:"organization,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
}

// TemplateInput for template describe tool
type TemplateInput struct {
	Organization string `json:"organization,omitempty"`
	Template     string `json:"template"`
}

// registerTemplateTools registers all template-related tools
func (s *Server) registerTemplateTools() {
	// ndcli.template.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.list",
		Description: "List configuration templates in an organization. Templates are collections of snippets assigned to OUs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"sort_by":        stringProperty("Sort field and direction (e.g., name:asc, created_at:desc)"),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page", 30),
				"created_after":  stringProperty("Filter by created date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"created_before": stringProperty("Filter by created date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"updated_after":  stringProperty("Filter by updated date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"updated_before": stringProperty("Filter by updated date (e.g., 30m, 2h, 7d or ISO 8601)"),
			},
		},
	}, s.handleTemplateList)

	// ndcli.template.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.describe",
		Description: "Get detailed information about a specific template including its snippets",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"template":     stringProperty("Template name"),
			},
			"required": []string{"template"},
		},
	}, s.handleTemplateDescribe)
}

func (s *Server) handleTemplateList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[TemplateListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Set defaults
	page := input.Page
	if page < 1 {
		page = 1
	}
	perPage := input.PerPage
	if perPage < 1 {
		perPage = 30
	}

	// Build query params
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	if input.SortBy != "" {
		params["sort_by"] = input.SortBy
	}
	if input.CreatedAfter != "" {
		parsed, err := helpers.ParseTimeFilter(input.CreatedAfter)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid created_after: " + err.Error()})
		}
		params["created_after"] = parsed
	}
	if input.CreatedBefore != "" {
		parsed, err := helpers.ParseTimeFilter(input.CreatedBefore)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid created_before: " + err.Error()})
		}
		params["created_before"] = parsed
	}
	if input.UpdatedAfter != "" {
		parsed, err := helpers.ParseTimeFilter(input.UpdatedAfter)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid updated_after: " + err.Error()})
		}
		params["updated_after"] = parsed
	}
	if input.UpdatedBefore != "" {
		parsed, err := helpers.ParseTimeFilter(input.UpdatedBefore)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid updated_before: " + err.Error()})
		}
		params["updated_before"] = parsed
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/templates", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.TemplateListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	// Build response data
	templateList := make([]map[string]interface{}, 0, len(result.Items))
	for _, tmpl := range result.Items {
		templateList = append(templateList, map[string]interface{}{
			"name":          tmpl.Name,
			"description":   tmpl.Description,
			"snippet_count": tmpl.SnippetCount,
			"created_at":    tmpl.CreatedAt,
			"updated_at":    tmpl.UpdatedAt,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"templates": templateList,
	}, page, perPage, result.Total)
}

func (s *Server) handleTemplateDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[TemplateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Template == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Template name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, input.Template), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var template models.Template
	if err := api.ParseResponse(resp, &template); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	// Build snippet list
	snippetList := make([]map[string]interface{}, 0, len(template.Snippets))
	for _, snip := range template.Snippets {
		snippetList = append(snippetList, map[string]interface{}{
			"name":     snip.Name,
			"type":     snip.Type,
			"priority": snip.Priority,
		})
	}

	return s.successResult(map[string]interface{}{
		"template": map[string]interface{}{
			"name":          template.Name,
			"description":   template.Description,
			"position":      template.Position,
			"snippet_count": template.SnippetCount,
			"snippets":      snippetList,
			"created_at":    template.CreatedAt,
			"updated_at":    template.UpdatedAt,
		},
	}, "")
}
