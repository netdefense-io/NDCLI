package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// registerOrgTools registers all organization-related tools
func (s *Server) registerOrgTools() {
	// ndcli.org.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.list",
		Description: "List organizations the current user has access to",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"page":     intProperty("Page number", 1),
				"per_page": intProperty("Items per page", 30),
			},
		},
	}, s.handleOrgList)

	// ndcli.org.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.describe",
		Description: "Get detailed information about a specific organization",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name (uses default from config if not specified)"),
			},
		},
	}, s.handleOrgDescribe)
}

func (s *Server) handleOrgList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[OrgListInput](argsJSON)
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

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, "/api/v1/organizations", params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.OrganizationListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	orgs := result.GetItems()

	// Build response data
	orgList := make([]map[string]interface{}, 0, len(orgs))
	for _, o := range orgs {
		orgList = append(orgList, map[string]interface{}{
			"name":         o.Name,
			"status":       o.Status,
			"default_ou":   o.DefaultOU,
			"device_count": o.DeviceCount,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"organizations": orgList,
	}, page, perPage, result.Total)
}

func (s *Server) handleOrgDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[OrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s", org), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var organization models.Organization
	if err := api.ParseResponse(resp, &organization); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"organization": map[string]interface{}{
			"name":         organization.Name,
			"status":       organization.Status,
			"default_ou":   organization.DefaultOU,
			"device_count": organization.DeviceCount,
			"created_at":   organization.CreatedAt,
		},
	}, "")
}
