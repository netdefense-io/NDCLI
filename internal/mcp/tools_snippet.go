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

// SnippetListInput for snippet list tool
type SnippetListInput struct {
	Organization  string `json:"organization,omitempty"`
	Type          string `json:"type,omitempty"`
	Name          string `json:"name,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
}

// SnippetInput for snippet describe tool
type SnippetInput struct {
	Organization string `json:"organization,omitempty"`
	Snippet      string `json:"snippet"`
}

// registerSnippetTools registers all snippet-related tools
func (s *Server) registerSnippetTools() {
	// ndcli.snippet.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.list",
		Description: "List configuration snippets in an organization",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"type":           stringEnumProperty("Filter by snippet type", []string{"USER", "GROUP", "ALIAS", "RULE", "UNBOUND_HOST_OVERRIDE", "UNBOUND_DOMAIN_FORWARD", "UNBOUND_HOST_ALIAS", "UNBOUND_ACL"}),
				"name":           stringProperty("Filter by name (regex pattern)"),
				"sort_by":        stringProperty("Sort field and direction (e.g., priority:asc, name:desc, created_at:desc)"),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page", 50),
				"created_after":  stringProperty("Filter by created date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"created_before": stringProperty("Filter by created date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"updated_after":  stringProperty("Filter by updated date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"updated_before": stringProperty("Filter by updated date (e.g., 30m, 2h, 7d or ISO 8601)"),
			},
		},
	}, s.handleSnippetList)

	// ndcli.snippet.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.describe",
		Description: "Get detailed information about a specific snippet including its content",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"snippet":      stringProperty("Snippet name"),
			},
			"required": []string{"snippet"},
		},
	}, s.handleSnippetDescribe)
}

func (s *Server) handleSnippetList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[SnippetListInput](argsJSON)
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
		perPage = 50
	}

	// Build query params
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	if input.Type != "" {
		params["type"] = input.Type
	}
	if input.Name != "" {
		params["name"] = input.Name
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

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/snippets", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.SnippetListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	// Build response data
	snippetList := make([]map[string]interface{}, 0, len(result.Items))
	for _, snip := range result.Items {
		snippetList = append(snippetList, map[string]interface{}{
			"name":       snip.Name,
			"type":       snip.Type,
			"priority":   snip.Priority,
			"created_at": snip.CreatedAt,
			"updated_at": snip.UpdatedAt,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"snippets": snippetList,
	}, page, perPage, result.Total)
}

func (s *Server) handleSnippetDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[SnippetInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Snippet == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Snippet name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, input.Snippet), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"snippet": map[string]interface{}{
			"name":         snippet.Name,
			"type":         snippet.Type,
			"priority":     snippet.Priority,
			"content":      snippet.Content,
			"organization": snippet.Organization,
			"created_at":   snippet.CreatedAt,
			"updated_at":   snippet.UpdatedAt,
		},
	}, "")
}
