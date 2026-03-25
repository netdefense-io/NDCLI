package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// registerResources registers all MCP resources
func (s *Server) registerResources() {
	// Static resources
	s.mcpServer.AddResource(&mcp.Resource{
		URI:         "ndcli://config",
		Name:        "NDCLI Configuration",
		Description: "Current NDCLI configuration settings",
		MIMEType:    "application/json",
	}, s.handleConfigResource)

	s.mcpServer.AddResource(&mcp.Resource{
		URI:         "ndcli://auth/status",
		Name:        "Authentication Status",
		Description: "Current authentication status and user information",
		MIMEType:    "application/json",
	}, s.handleAuthResource)

	// Dynamic resources using templates
	s.mcpServer.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "ndcli://org/{name}",
		Name:        "Organization Details",
		Description: "Get details for a specific organization",
		MIMEType:    "application/json",
	}, s.handleOrgResource)

	s.mcpServer.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "ndcli://org/{name}/devices",
		Name:        "Organization Devices",
		Description: "List devices in a specific organization",
		MIMEType:    "application/json",
	}, s.handleOrgDevicesResource)
}

func (s *Server) handleConfigResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	cfg := config.Get()

	content := map[string]interface{}{
		"controlplane": map[string]interface{}{
			"host":       cfg.Controlplane.Host,
			"ssl_verify": cfg.Controlplane.SSLVerify,
		},
		"organization": map[string]interface{}{
			"name": cfg.Organization.Name,
		},
		"output": map[string]interface{}{
			"format": cfg.Output.Format,
		},
		"version": config.Version,
	}

	jsonContent, err := marshalJSON(content)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "ndcli://config",
				MIMEType: "application/json",
				Text:     jsonContent,
			},
		},
	}, nil
}

func (s *Server) handleAuthResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	isAuthenticated := s.authManager.IsAuthenticated()

	status := "not_authenticated"
	var userInfo *models.UserInfo

	if isAuthenticated {
		_, err := s.authManager.GetAccessToken()
		if err != nil {
			status = "token_expired"
		} else {
			status = "authenticated"
			userInfo, _ = s.authManager.GetUserInfo()
		}
	}

	content := map[string]interface{}{
		"authenticated": isAuthenticated,
		"status":        status,
		"storage":       s.authManager.GetStorageName(),
	}

	if userInfo != nil {
		content["user"] = map[string]interface{}{
			"email": userInfo.Email,
			"name":  userInfo.Name,
		}
	}

	jsonContent, err := marshalJSON(content)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "ndcli://auth/status",
				MIMEType: "application/json",
				Text:     jsonContent,
			},
		},
	}, nil
}

func (s *Server) handleOrgResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return nil, err
	}

	// Extract org name from URI: ndcli://org/{name}
	orgName := extractOrgFromURI(req.Params.URI)
	if orgName == "" {
		return nil, &ToolError{Code: "INVALID_URI", Message: "Organization name not found in URI"}
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s", orgName), nil)
	if err != nil {
		return nil, err
	}

	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		return nil, err
	}

	content := map[string]interface{}{
		"name":         org.Name,
		"status":       org.Status,
		"default_ou":   org.DefaultOU,
		"device_count": org.DeviceCount,
		"created_at":   org.CreatedAt,
	}

	jsonContent, err := marshalJSON(content)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     jsonContent,
			},
		},
	}, nil
}

func (s *Server) handleOrgDevicesResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return nil, err
	}

	// Extract org name from URI: ndcli://org/{name}/devices
	orgName := extractOrgFromURI(req.Params.URI)
	if orgName == "" {
		return nil, &ToolError{Code: "INVALID_URI", Message: "Organization name not found in URI"}
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	params := map[string]string{
		"per_page": "100",
	}

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices", orgName), params)
	if err != nil {
		return nil, err
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	devices := result.GetItems()

	deviceList := make([]map[string]interface{}, 0, len(devices))
	for _, d := range devices {
		deviceList = append(deviceList, map[string]interface{}{
			"name":                 d.Name,
			"uuid":                 d.UUID,
			"status":               d.Status,
			"organizational_units": d.OrganizationalUnits,
			"heartbeat":            d.Heartbeat,
		})
	}

	content := map[string]interface{}{
		"organization": orgName,
		"devices":      deviceList,
		"total":        result.Total,
	}

	jsonContent, err := marshalJSON(content)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     jsonContent,
			},
		},
	}, nil
}

// extractOrgFromURI extracts the organization name from a resource URI
// Handles URIs like:
// - ndcli://org/{name}
// - ndcli://org/{name}/devices
func extractOrgFromURI(uri string) string {
	// Remove the prefix
	trimmed := strings.TrimPrefix(uri, "ndcli://org/")
	if trimmed == uri {
		return ""
	}

	// Split by / and take the first part
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
