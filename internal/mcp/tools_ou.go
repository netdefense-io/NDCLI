package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type ouListInput struct {
	Organization  string `json:"organization,omitempty"`
	Status        string `json:"status,omitempty"`
	Name          string `json:"name,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	SortOrder     string `json:"sort_order,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
}

type ouCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
}

type ouRenameInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
	NewName      string `json:"new_name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type ouMutateInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type ouDeviceLinkInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
	Device       string `json:"device"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type ouTemplateLinkInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
	Template     string `json:"template"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// registerOUTools registers all organizational unit tools.
func (s *Server) registerOUTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.list",
		Description: "List organizational units in an organization with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"status":         stringEnumProperty("Filter by status (default all)", []string{"all", "enabled"}),
				"name":           stringProperty("Filter by name (regex)"),
				"sort_by":        stringEnumProperty("Sort field", []string{"name", "device_count", "created_at", "updated_at"}),
				"sort_order":     stringEnumProperty("Sort order", []string{"asc", "desc"}),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page", 20),
				"created_after":  stringProperty("Filter by created date after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"created_before": stringProperty("Filter by created date before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
			},
		},
	}, s.handleOUList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.describe",
		Description: "Get detailed information about an OU, including its devices and templates.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
			},
			"required": []string{"ou"},
		},
	}, s.handleOUDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.create",
		Description: "Create a new OU in the organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("New OU name"),
				"description":  stringProperty("OU description (optional)"),
			},
			"required": []string{"name"},
		},
	}, s.handleOUCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.delete",
		Description: "Delete an OU. Fails with a 409 listing blocking devices if the OU still has active devices. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"ou"},
		},
	}, s.handleOUDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.rename",
		Description: "Rename an OU. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("Current OU name"),
				"new_name":     stringProperty("New OU name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"ou", "new_name"},
		},
	}, s.handleOURename)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.device_list",
		Description: "List the devices currently assigned to an OU.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
			},
			"required": []string{"ou"},
		},
	}, s.handleOUDeviceList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.add_device",
		Description: "Attach a device to an OU.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"ou", "device"},
		},
	}, s.handleOUAddDevice)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.remove_device",
		Description: "Detach a device from an OU. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
				"device":       stringProperty("Device name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"ou", "device"},
		},
	}, s.handleOURemoveDevice)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.template_list",
		Description: "List templates assigned to an OU.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
			},
			"required": []string{"ou"},
		},
	}, s.handleOUTemplateList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.template_add",
		Description: "Attach a template to an OU.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
				"template":     stringProperty("Template name"),
			},
			"required": []string{"ou", "template"},
		},
	}, s.handleOUTemplateAdd)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.ou.template_remove",
		Description: "Detach a template from an OU. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
				"template":     stringProperty("Template name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"ou", "template"},
		},
	}, s.handleOUTemplateRemove)
}

func (s *Server) handleOUList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OUList(apiCtx, org, service.OUListOpts{
		Status:        input.Status,
		Name:          input.Name,
		SortBy:        input.SortBy,
		SortOrder:     input.SortOrder,
		Page:          input.Page,
		PageSize:      input.PerPage,
		CreatedAfter:  input.CreatedAfter,
		CreatedBefore: input.CreatedBefore,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.OUs))
	for _, ou := range result.OUs {
		items = append(items, ouSummary(&ou))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"organizational_units": items,
	}, result.Page, result.PageSize, result.Total)
}

func (s *Server) handleOUDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouMutateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ou, err := s.svc.OUGet(apiCtx, org, input.OU)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organizational_unit": ouFull(ou),
	}, "")
}

func (s *Server) handleOUCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ou, err := s.svc.OUCreate(apiCtx, org, input.Name, input.Description)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organizational_unit": ouFull(ou),
		"action":              "created",
	}, fmt.Sprintf("OU '%s' created", input.Name))
}

func (s *Server) handleOUDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouMutateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete OU", input.OU)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OUDelete(apiCtx, org, input.OU); err != nil {
		// Surface BlockingResources from a 409 as structured data instead
		// of just "API_ERROR: ...".
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && len(apiErr.BlockingResources) > 0 {
			return s.successResult(map[string]interface{}{
				"action":              "blocked",
				"ou":                  input.OU,
				"blocking_devices":    apiErr.BlockingResources,
				"blocking_count":      len(apiErr.BlockingResources),
			}, fmt.Sprintf("Cannot delete OU '%s' — %d active device(s) must be removed first", input.OU, len(apiErr.BlockingResources)))
		}
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":     input.OU,
		"action": "deleted",
	}, fmt.Sprintf("OU '%s' deleted", input.OU))
}

func (s *Server) handleOURename(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouRenameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("rename OU", fmt.Sprintf("%s → %s", input.OU, input.NewName))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if _, err := s.svc.OURename(apiCtx, org, input.OU, input.NewName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":       input.OU,
		"new_name": input.NewName,
		"action":   "renamed",
	}, fmt.Sprintf("OU renamed: %s → %s", input.OU, input.NewName))
}

func (s *Server) handleOUDeviceList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouMutateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OUDeviceList(apiCtx, org, input.OU)
	if err != nil {
		return s.errorResult(err)
	}
	devices := make([]map[string]interface{}, 0, len(result.Devices))
	for _, d := range result.Devices {
		devices = append(devices, deviceSummary(&d))
	}
	return s.successResult(map[string]interface{}{
		"ou":      input.OU,
		"devices": devices,
		"total":   result.Total,
	}, "")
}

func (s *Server) handleOUAddDevice(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouDeviceLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OUAddDevice(apiCtx, org, input.OU, input.Device); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":     input.OU,
		"device": input.Device,
		"action": "added",
	}, fmt.Sprintf("Device '%s' added to OU '%s'", input.Device, input.OU))
}

func (s *Server) handleOURemoveDevice(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouDeviceLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove device from OU", fmt.Sprintf("%s ← %s", input.OU, input.Device))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OURemoveDevice(apiCtx, org, input.OU, input.Device); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":     input.OU,
		"device": input.Device,
		"action": "removed",
	}, fmt.Sprintf("Device '%s' removed from OU '%s'", input.Device, input.OU))
}

func (s *Server) handleOUTemplateList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouMutateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OUTemplateList(apiCtx, org, input.OU)
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, t := range result.Items {
		items = append(items, map[string]interface{}{
			"name":          t.Name,
			"snippet_count": len(t.Snippets),
		})
	}
	return s.successResult(map[string]interface{}{
		"ou":        input.OU,
		"templates": items,
		"total":     result.Total,
	}, "")
}

func (s *Server) handleOUTemplateAdd(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouTemplateLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OUTemplateAdd(apiCtx, org, input.OU, input.Template); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":       input.OU,
		"template": input.Template,
		"action":   "added",
	}, fmt.Sprintf("Template '%s' added to OU '%s'", input.Template, input.OU))
}

func (s *Server) handleOUTemplateRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[ouTemplateLinkInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove template from OU", fmt.Sprintf("%s ← %s", input.OU, input.Template))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OUTemplateRemove(apiCtx, org, input.OU, input.Template); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"ou":       input.OU,
		"template": input.Template,
		"action":   "removed",
	}, fmt.Sprintf("Template '%s' removed from OU '%s'", input.Template, input.OU))
}

func ouSummary(ou *models.OrganizationalUnit) map[string]interface{} {
	return map[string]interface{}{
		"name":           ou.Name,
		"display_name":   ou.DisplayName,
		"organization":   ou.Organization,
		"status":         ou.Status,
		"description":    ou.Description,
		"device_count":   ou.DeviceCount,
		"template_count": ou.TemplateCount,
		"created_at":     ou.CreatedAt,
		"updated_at":     ou.UpdatedAt,
	}
}

func ouFull(ou *models.OrganizationalUnit) map[string]interface{} {
	full := ouSummary(ou)
	full["devices"] = ou.Devices
	full["templates"] = ou.Templates
	full["parent_ou"] = ou.ParentOU
	return full
}
