package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// registerDeviceTools registers all device-related tools.
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
				"synced_after":     stringProperty("Filter by synced-at after date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"synced_before":    stringProperty("Filter by synced-at before date (e.g., 30m, 2h, 7d or ISO 8601)"),
				"created_after":    stringProperty("Filter by created date after (e.g., 30m, 2h, 7d or ISO 8601)"),
				"created_before":   stringProperty("Filter by created date before (e.g., 30m, 2h, 7d or ISO 8601)"),
				"drift_status":     stringEnumProperty("Filter by drift status", []string{"IN_SYNC", "DRIFT", "NEVER_SYNCED", "UNKNOWN", "ERROR"}),
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

	// ndcli.device.approve_all
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.device.approve_all",
		Description: "Approve every PENDING device in the organization. Requires confirm=true to execute; without it, returns a count preview.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"confirm":      confirmProperty(),
			},
		},
	}, s.handleDeviceApproveAll)

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

	// ndcli.device.rebind_token
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.device.rebind_token",
		Description: "Issue a one-time signing-key re-bind token for a device. The raw token is returned once and never echoed again. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
				"ttl_seconds":  intProperty("Token validity window in seconds (default 86400, max 604800)", 86400),
				"confirm":      confirmProperty(),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceRebindToken)

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

// deviceListInput mirrors the new (post-service) device list schema. Kept
// local to this file because no other tools_*.go consumes it.
type deviceListInput struct {
	Organization    string `json:"organization,omitempty"`
	Status          string `json:"status,omitempty"`
	OU              string `json:"ou,omitempty"`
	Name            string `json:"name,omitempty"`
	SortBy          string `json:"sort_by,omitempty"`
	Page            int    `json:"page,omitempty"`
	PerPage         int    `json:"per_page,omitempty"`
	HeartbeatAfter  string `json:"heartbeat_after,omitempty"`
	HeartbeatBefore string `json:"heartbeat_before,omitempty"`
	SyncedAfter     string `json:"synced_after,omitempty"`
	SyncedBefore    string `json:"synced_before,omitempty"`
	CreatedAfter    string `json:"created_after,omitempty"`
	CreatedBefore   string `json:"created_before,omitempty"`
	DriftStatus     string `json:"drift_status,omitempty"`
}

type deviceApproveAllInput struct {
	Organization string `json:"organization,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type deviceRebindTokenInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	TTLSeconds   int    `json:"ttl_seconds,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

func (s *Server) handleDeviceList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[deviceListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.DeviceList(apiCtx, org, service.DeviceListOpts{
		Status:          input.Status,
		OU:              input.OU,
		Name:            input.Name,
		SortBy:          input.SortBy,
		Page:            input.Page,
		PerPage:         input.PerPage,
		HeartbeatAfter:  input.HeartbeatAfter,
		HeartbeatBefore: input.HeartbeatBefore,
		SyncedAfter:     input.SyncedAfter,
		SyncedBefore:    input.SyncedBefore,
		CreatedAfter:    input.CreatedAfter,
		CreatedBefore:   input.CreatedBefore,
		DriftStatus:     input.DriftStatus,
	})
	if err != nil {
		return s.errorResult(err)
	}

	deviceList := make([]map[string]interface{}, 0, len(result.Devices))
	for _, d := range result.Devices {
		deviceList = append(deviceList, deviceSummary(&d))
	}

	return s.successResultWithPagination(map[string]interface{}{
		"devices": deviceList,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleDeviceDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	device, err := s.svc.DeviceGet(apiCtx, org, input.Device)
	if err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"device": deviceFull(device),
	}, "")
}

func (s *Server) handleDeviceApprove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.DeviceApprove(apiCtx, org, input.Device); err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"device": input.Device,
		"action": "approved",
		"status": "ENABLED",
	}, fmt.Sprintf("Device '%s' approved successfully", input.Device))
}

func (s *Server) handleDeviceApproveAll(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[deviceApproveAllInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// Pre-flight: count pending devices for the preview / final report.
	listing, err := s.svc.DeviceList(apiCtx, org, service.DeviceListOpts{
		Status:  models.DeviceStatusPending,
		PerPage: 500,
	})
	if err != nil {
		return s.errorResult(err)
	}
	if len(listing.Devices) == 0 {
		return s.successResult(map[string]interface{}{
			"approved": 0,
			"failed":   0,
			"devices":  []string{},
		}, "No pending devices found")
	}
	if !input.Confirm {
		names := make([]string, 0, len(listing.Devices))
		for _, d := range listing.Devices {
			names = append(names, d.Name)
		}
		return s.previewResult("approve", fmt.Sprintf("%d pending devices: %v", len(names), names))
	}

	results, err := s.svc.DeviceApproveAll(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}

	approved := 0
	failed := []map[string]string{}
	approvedNames := []string{}
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, map[string]string{
				"device": r.Name,
				"error":  r.Err.Error(),
			})
			continue
		}
		approved++
		approvedNames = append(approvedNames, r.Name)
	}

	return s.successResult(map[string]interface{}{
		"approved":       approved,
		"approved_names": approvedNames,
		"failed_count":   len(failed),
		"failed":         failed,
	}, fmt.Sprintf("Approved %d, failed %d", approved, len(failed)))
}

func (s *Server) handleDeviceRename(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceRenameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("rename", fmt.Sprintf("%s → %s", input.Device, input.NewName))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.DeviceRename(apiCtx, org, input.Device, input.NewName); err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"device":   input.Device,
		"new_name": input.NewName,
		"action":   "renamed",
	}, fmt.Sprintf("Device renamed: %s → %s", input.Device, input.NewName))
}

func (s *Server) handleDeviceRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove", input.Device)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.DeviceRemove(apiCtx, org, input.Device); err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"device": input.Device,
		"action": "removed",
	}, fmt.Sprintf("Device '%s' removed successfully", input.Device))
}

func (s *Server) handleDeviceRebindToken(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[deviceRebindTokenInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("issue rebind token for", input.Device)
	}

	ttl := time.Duration(input.TTLSeconds) * time.Second
	if input.TTLSeconds == 0 {
		ttl = 24 * time.Hour
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	parsed, err := s.svc.DeviceRebindToken(apiCtx, org, input.Device, ttl)
	if err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"device":          input.Device,
		"bootstrap_token": parsed.BootstrapToken,
		"expires_at":      parsed.ExpiresAt,
		"message":         parsed.Message,
	}, "Rebind token issued. Token is single-use; store securely — it will not be returned again.")
}

func (s *Server) handleDeviceSnippets(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[DeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	device, err := s.svc.DeviceGet(apiCtx, org, input.Device)
	if err != nil {
		return s.errorResult(err)
	}

	if len(device.OrganizationalUnits) == 0 {
		return s.successResult(map[string]interface{}{
			"device":   input.Device,
			"ous":      []string{},
			"snippets": []map[string]interface{}{},
			"message":  "Device has no organizational units assigned",
		}, "")
	}

	// OU + template traversal still uses the raw API client until OU/template
	// services land in the next phase. Once they do, this becomes a service-
	// level composite call.
	type snippetInfo struct {
		Name     string
		Type     string
		Priority int
		Template string
		OU       string
	}
	snippetMap := make(map[string]snippetInfo)
	ouTemplates := make(map[string][]string)

	for _, ouName := range device.OrganizationalUnits {
		resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, ouName), nil)
		if err != nil {
			continue
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

// deviceSummary is the compact device representation used in list responses.
func deviceSummary(d *models.Device) map[string]interface{} {
	return map[string]interface{}{
		"name":                 d.Name,
		"uuid":                 d.UUID,
		"status":               d.Status,
		"organizational_units": d.OrganizationalUnits,
		"heartbeat":            d.Heartbeat,
		"synced_at":            d.SyncedAt,
		"drift_status":         d.DriftStatus,
		"drift_checked_at":     d.DriftCheckedAt,
		"created_at":           d.CreatedAt,
	}
}

// deviceFull is the detailed device representation used in describe responses.
func deviceFull(d *models.Device) map[string]interface{} {
	return map[string]interface{}{
		"name":                 d.Name,
		"uuid":                 d.UUID,
		"status":               d.Status,
		"organization":         d.Organization,
		"organizational_units": d.OrganizationalUnits,
		"heartbeat":            d.Heartbeat,
		"synced_at":            d.SyncedAt,
		"synced_hash":          d.SyncedHash,
		"drift_status":         d.DriftStatus,
		"drift_checked_at":     d.DriftCheckedAt,
		"created_at":           d.CreatedAt,
	}
}
