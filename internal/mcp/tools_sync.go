package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type syncFilterInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device,omitempty"`
	OU           string `json:"ou,omitempty"`
	DriftStatus  string `json:"drift_status,omitempty"`
	Template     string `json:"template,omitempty"`
}

type syncApplyInput struct {
	syncFilterInput
	Force   bool `json:"force,omitempty"`
	Confirm bool `json:"confirm,omitempty"`
}

// registerSyncTools registers all sync-related tools.
func (s *Server) registerSyncTools() {
	// ndcli.sync.status
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.sync.status",
		Description: "Show synchronization status for devices matching the filter. All filters are regex patterns. Organization defaults to the configured org.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name regex (defaults to configured org)"),
				"device":       stringProperty("Device name regex"),
				"ou":           stringProperty("OU name regex"),
				"drift_status": stringEnumProperty("Filter by drift status", []string{"IN_SYNC", "DRIFT", "NEVER_SYNCED", "UNKNOWN", "ERROR"}),
				"template":     stringProperty("Template name regex — restricts to devices whose effective OU→Template chain matches"),
			},
		},
	}, s.handleSyncStatus)

	// ndcli.sync.apply
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.sync.apply",
		Description: "Trigger configuration sync for every device matching the filter. Filters are regex patterns; org defaults to the configured org. Requires confirm=true to execute; without it, returns the affected device list as a preview.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name regex (defaults to configured org)"),
				"device":       stringProperty("Device name regex"),
				"ou":           stringProperty("OU name regex"),
				"drift_status": stringEnumProperty("Only sync devices with this drift status", []string{"IN_SYNC", "DRIFT", "NEVER_SYNCED", "UNKNOWN", "ERROR"}),
				"template":     stringProperty("Template name regex — restricts to devices whose effective OU→Template chain matches"),
				"force":        boolProperty("Force sync even if already in sync"),
				"confirm":      confirmProperty(),
			},
		},
	}, s.handleSyncApply)
}

func (s *Server) handleSyncStatus(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[syncFilterInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	defaultOrg, err := s.svc.ResolveOrg("")
	if err != nil && input.Organization == "" {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.SyncStatus(apiCtx, defaultOrg, service.SyncFilter{
		Organization: input.Organization,
		Device:       input.Device,
		OU:           input.OU,
		DriftStatus:  input.DriftStatus,
		Template:     input.Template,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, syncStatusItem(&it))
	}

	return s.successResult(map[string]interface{}{
		"items": items,
		"total": result.Total,
	}, "")
}

func (s *Server) handleSyncApply(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[syncApplyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	defaultOrg, err := s.svc.ResolveOrg("")
	if err != nil && input.Organization == "" {
		return s.errorResult(err)
	}

	filter := service.SyncFilter{
		Organization: input.Organization,
		Device:       input.Device,
		OU:           input.OU,
		DriftStatus:  input.DriftStatus,
		Template:     input.Template,
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if !input.Confirm {
		status, err := s.svc.SyncStatus(apiCtx, defaultOrg, filter)
		if err != nil {
			return s.errorResult(err)
		}
		names := make([]string, 0, len(status.Items))
		for _, it := range status.Items {
			names = append(names, it.DeviceName)
		}
		return s.previewResult("sync", fmt.Sprintf("%d device(s): %v", len(names), names))
	}

	applied, err := s.svc.SyncApply(apiCtx, defaultOrg, filter, input.Force)
	if err != nil {
		return s.errorResult(err)
	}

	tasks := make([]map[string]interface{}, 0, len(applied.Response.Tasks))
	for _, t := range applied.Response.Tasks {
		tasks = append(tasks, map[string]interface{}{
			"task":              t.Task,
			"device":            t.DeviceName,
			"snippet_count":     t.SnippetCount,
			"vpn_network_count": t.VpnNetworkCount,
			"payload_hash":      t.PayloadHash,
		})
	}
	errs := make([]map[string]interface{}, 0, len(applied.Response.Errors))
	for _, e := range applied.Response.Errors {
		errs = append(errs, map[string]interface{}{
			"device":              e.DeviceName,
			"error":               e.Error,
			"code":                e.Code,
			"conflicts":           e.Conflicts,
			"undefined_variables": e.UndefinedVariables,
		})
	}
	return s.successResult(map[string]interface{}{
		"message":          applied.Response.Message,
		"devices_affected": applied.Response.DevicesAffected,
		"skipped":          applied.Response.Skipped,
		"tasks":            tasks,
		"errors":           errs,
		"status_code":      applied.StatusCode,
	}, applied.Response.Message)
}

func syncStatusItem(it *models.SyncStatusItem) map[string]interface{} {
	return map[string]interface{}{
		"device_name":  it.DeviceName,
		"organization": it.Organization,
		"ous":          it.OUs,
		"auto_sync":    it.AutoSync,
		"synced_at":    it.SyncedAt,
		"synced_hash":  it.SyncedHash,
		"current_hash": it.CurrentHash,
		"in_sync":      it.InSync,
		"error":        it.Error,
	}
}

