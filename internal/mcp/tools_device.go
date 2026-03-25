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

// registerDeviceTools registers all device-related tools
func (s *Server) registerDeviceTools() {
	// ndcli.device.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.list",
		Description: "List managed firewall devices in an organization with optional filtering",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":     organizationProperty(),
				"status":           stringEnumProperty("Filter by device status", []string{"PENDING", "ENABLED", "DISABLED"}),
				"ou":               stringProperty("Filter by organizational unit name"),
				"name":             stringProperty("Filter by name (regex pattern)"),
				"sort_by":          stringProperty("Sort field and direction (e.g., name:asc, created_at:desc)"),
				"page":             intProperty("Page number", 1),
				"per_page":         intProperty("Items per page", 30),
				"heartbeat_after":  stringProperty("Filter by heartbeat after date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"heartbeat_before": stringProperty("Filter by heartbeat before date (e.g., 30m, 2h, 7d or ISO 8601)"),
			},
		},
	}, s.handleDeviceList)

	// ndcli.device.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.describe",
		Description: "Get detailed information about a specific device",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceDescribe)

	// ndcli.device.approve
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.approve",
		Description: "Approve a pending device to enable management",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name to approve"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceApprove)

	// ndcli.device.rename
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.rename",
		Description: "Rename a device. Requires confirm=true to execute.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Current device name"),
				"new_name":     stringProperty("New device name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"device", "new_name"},
		},
	}, s.handleDeviceRename)

	// ndcli.device.remove
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.remove",
		Description: "Remove a device from management. Requires confirm=true to execute.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name to remove"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceRemove)

	// ndcli.device.snippets
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.snippets",
		Description: "Get all configuration snippets that apply to a device. Traverses device → OUs → templates → snippets hierarchy.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceSnippets)
}

func (s *Server) handleDeviceList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceListInput](argsJSON)
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

	if input.Status != "" {
		params["status"] = input.Status
	}
	if input.OU != "" {
		params["ou"] = input.OU
	}
	if input.Name != "" {
		params["name"] = input.Name
	}
	if input.SortBy != "" {
		params["sort_by"] = input.SortBy
	}
	if input.HeartbeatAfter != "" {
		parsed, err := helpers.ParseTimeFilter(input.HeartbeatAfter)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid heartbeat_after: " + err.Error()})
		}
		params["heartbeat_after"] = parsed
	}
	if input.HeartbeatBefore != "" {
		parsed, err := helpers.ParseTimeFilter(input.HeartbeatBefore)
		if err != nil {
			return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Invalid heartbeat_before: " + err.Error()})
		}
		params["heartbeat_before"] = parsed
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	devices := result.GetItems()

	// Build response data
	deviceList := make([]map[string]interface{}, 0, len(devices))
	for _, d := range devices {
		deviceList = append(deviceList, map[string]interface{}{
			"name":                d.Name,
			"uuid":                d.UUID,
			"status":              d.Status,
			"organizational_units": d.OrganizationalUnits,
			"heartbeat":           d.Heartbeat,
			"synced_at":           d.SyncedAt,
			"created_at":          d.CreatedAt,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"devices": deviceList,
	}, page, perPage, result.Total)
}

func (s *Server) handleDeviceDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Device == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Device name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, input.Device), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var device models.Device
	if err := api.ParseResponse(resp, &device); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"device": map[string]interface{}{
			"name":                 device.Name,
			"uuid":                 device.UUID,
			"status":               device.Status,
			"organization":         device.Organization,
			"organizational_units": device.OrganizationalUnits,
			"heartbeat":            device.Heartbeat,
			"synced_at":            device.SyncedAt,
			"synced_hash":          device.SyncedHash,
			"created_at":           device.CreatedAt,
		},
	}, "")
}

func (s *Server) handleDeviceApprove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Device == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Device name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Post(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/approve", org, input.Device), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"device": input.Device,
		"action": "approved",
		"status": "ENABLED",
	}, fmt.Sprintf("Device '%s' approved successfully", input.Device))
}

func (s *Server) handleDeviceRename(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceRenameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Device == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Device name is required"})
	}
	if input.NewName == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "New name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Check for confirmation - destructive operation
	if !input.Confirm {
		return s.previewResult("rename", fmt.Sprintf("%s → %s", input.Device, input.NewName))
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	payload := map[string]string{"new_name": input.NewName}
	resp, err := s.apiClient.Put(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/rename", org, input.Device), payload)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"device":   input.Device,
		"new_name": input.NewName,
		"action":   "renamed",
	}, fmt.Sprintf("Device renamed: %s → %s", input.Device, input.NewName))
}

func (s *Server) handleDeviceRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Device == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Device name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Check for confirmation - destructive operation
	if !input.Confirm {
		return s.previewResult("remove", input.Device)
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Delete(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, input.Device))
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	return s.successResult(map[string]interface{}{
		"device": input.Device,
		"action": "removed",
	}, fmt.Sprintf("Device '%s' removed successfully", input.Device))
}

func (s *Server) handleDeviceSnippets(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	if input.Device == "" {
		return s.errorResult(&ToolError{Code: "INVALID_INPUT", Message: "Device name is required"})
	}

	// Get organization
	org, err := s.getOrganization(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// Step 1: Get device to find its OUs
	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, input.Device), nil)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var device models.Device
	if err := api.ParseResponse(resp, &device); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	if len(device.OrganizationalUnits) == 0 {
		return s.successResult(map[string]interface{}{
			"device":   input.Device,
			"ous":      []string{},
			"snippets": []map[string]interface{}{},
			"message":  "Device has no organizational units assigned",
		}, "")
	}

	// Step 2: For each OU, get its templates
	type snippetInfo struct {
		Name       string
		Type       string
		Priority   int
		Template   string
		OU         string
	}

	snippetMap := make(map[string]snippetInfo) // Use map to dedupe by snippet name
	ouTemplates := make(map[string][]string)   // Track templates per OU

	for _, ouName := range device.OrganizationalUnits {
		resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, ouName), nil)
		if err != nil {
			continue // Skip OUs we can't access
		}

		var ou models.OrganizationalUnit
		if err := api.ParseResponse(resp, &ou); err != nil {
			continue
		}

		templateNames := make([]string, 0, len(ou.Templates))
		for _, tmpl := range ou.Templates {
			templateNames = append(templateNames, tmpl.Name)
		}
		ouTemplates[ouName] = templateNames

		// Step 3: For each template, get its snippets
		for _, tmpl := range ou.Templates {
			resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, tmpl.Name), nil)
			if err != nil {
				continue
			}

			var template models.Template
			if err := api.ParseResponse(resp, &template); err != nil {
				continue
			}

			for _, snip := range template.Snippets {
				// Keep the snippet with highest priority (lower number = higher priority)
				if existing, ok := snippetMap[snip.Name]; !ok || snip.Priority < existing.Priority {
					snippetMap[snip.Name] = snippetInfo{
						Name:     snip.Name,
						Type:     snip.Type,
						Priority: snip.Priority,
						Template: tmpl.Name,
						OU:       ouName,
					}
				}
			}
		}
	}

	// Build response
	snippetList := make([]map[string]interface{}, 0, len(snippetMap))
	for _, snip := range snippetMap {
		snippetList = append(snippetList, map[string]interface{}{
			"name":     snip.Name,
			"type":     snip.Type,
			"priority": snip.Priority,
			"template": snip.Template,
			"ou":       snip.OU,
		})
	}

	return s.successResult(map[string]interface{}{
		"device":       input.Device,
		"ous":          device.OrganizationalUnits,
		"ou_templates": ouTemplates,
		"snippets":     snippetList,
		"total":        len(snippetList),
	}, "")
}
