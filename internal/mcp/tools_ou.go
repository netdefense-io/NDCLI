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

// registerOUTools registers all organizational unit-related tools
func (s *Server) registerOUTools() {
	// ndcli.ou.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.list",
		Description: "List organizational units in an organization",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 30),
			},
		},
	}, s.handleOUList)

	// ndcli.ou.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.describe",
		Description: "Get detailed information about a specific organizational unit",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("Organizational unit name"),
			},
			"required": []string{"ou"},
		},
	}, s.handleOUDescribe)
}

func (s *Server) handleOUList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[OUListInput](argsJSON)
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

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/ous", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.OUListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	ous := result.OUs

	// Build response data
	ouList := make([]map[string]interface{}, 0, len(ous))
	for _, ou := range ous {
		ouList = append(ouList, map[string]interface{}{
			"name":           ou.Name,
			"organization":   ou.Organization,
			"device_count":   ou.DeviceCount,
			"template_count": ou.TemplateCount,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"organizational_units": ouList,
	}, page, perPage, result.Total)
}

func (s *Server) handleOUDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[OUInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.OU == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "OU name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, input.OU), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"organizational_unit": map[string]interface{}{
			"name":           ou.Name,
			"organization":   ou.Organization,
			"device_count":   ou.DeviceCount,
			"template_count": ou.TemplateCount,
			"devices":        ou.Devices,
			"templates":      ou.Templates,
		},
	}, "")
}
