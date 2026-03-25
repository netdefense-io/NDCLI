package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// registerSyncTools registers all sync-related tools
func (s *Server) registerSyncTools() {
	// ndcli.sync.status
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.sync.status",
		Description: "Show synchronization status of devices. Lists devices with pending config changes.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Filter by specific device name (optional)"),
			},
		},
	}, s.handleSyncStatus)

	// ndcli.sync.apply
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.sync.apply",
		Description: "Trigger configuration sync for a specific device",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name to sync"),
			},
			"required": []string{"device"},
		},
	}, s.handleSyncApply)
}

func (s *Server) handleSyncStatus(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[SyncStatusInput](argsJSON)
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

	var params map[string]string
	if input.Device != "" {
		params = map[string]string{
			"name": input.Device,
		}
	} else {
		params = map[string]string{
			"status":   "ENABLED",
			"per_page": "100",
		}
	}

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	devices := result.GetItems()

	// Categorize devices by sync status
	synced := make([]map[string]interface{}, 0)
	pendingSync := make([]map[string]interface{}, 0)
	neverSynced := make([]map[string]interface{}, 0)

	for _, d := range devices {
		deviceInfo := map[string]interface{}{
			"name":        d.Name,
			"synced_at":   d.SyncedAt,
			"synced_hash": d.SyncedHash,
			"heartbeat":   d.Heartbeat,
		}

		var syncedAt time.Time
		if d.SyncedAt != nil {
			syncedAt = d.SyncedAt.Time
		}
		if syncedAt.IsZero() {
			neverSynced = append(neverSynced, deviceInfo)
		} else if d.SyncedHash == nil || *d.SyncedHash == "" {
			pendingSync = append(pendingSync, deviceInfo)
		} else {
			synced = append(synced, deviceInfo)
		}
	}

	return s.successResult(map[string]interface{}{
		"summary": map[string]interface{}{
			"total":        len(devices),
			"synced":       len(synced),
			"pending_sync": len(pendingSync),
			"never_synced": len(neverSynced),
		},
		"synced":       synced,
		"pending_sync": pendingSync,
		"never_synced": neverSynced,
	}, "")
}

func (s *Server) handleSyncApply(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[SyncApplyInput](argsJSON)
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

	// Make API call to trigger sync
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	payload := map[string]string{
		"type": "SYNC",
	}

	resp, err := s.apiClient.Post(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/tasks", org, input.Device), payload)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var task models.Task
	statusCode, err := api.ParseResponseWithStatus(resp, &task)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	// 201 means task created, 200 means task already exists
	if statusCode == 201 {
		return s.successResult(map[string]interface{}{
			"device": input.Device,
			"action": "sync_triggered",
			"task":   task.ID,
			"status": task.Status,
		}, fmt.Sprintf("Sync task created for device '%s'", input.Device))
	}

	return s.successResult(map[string]interface{}{
		"device": input.Device,
		"action": "sync_pending",
		"task":   task.ID,
		"status": task.Status,
	}, fmt.Sprintf("Sync task already pending for device '%s'", input.Device))
}

// taskListInput for task list tool
type taskListInput struct {
	Organization string `json:"organization,omitempty"`
	Status       string `json:"status,omitempty"`
	Device       string `json:"device,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

// registerTaskTools registers all task-related tools
func (s *Server) registerTaskTools() {
	// ndcli.task.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.task.list",
		Description: "List tasks (sync operations, config pushes) across devices",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"status":       stringEnumProperty("Filter by task status", []string{"PENDING", "RUNNING", "COMPLETED", "FAILED"}),
				"device":       stringProperty("Filter by device name"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 30),
			},
		},
	}, s.handleTaskList)
}

func (s *Server) handleTaskList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Parse input
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[taskListInput](argsJSON)
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
	if input.Device != "" {
		params["device"] = input.Device
	}

	// Make API call
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	resp, err := s.apiClient.Get(apiCtx, fmt.Sprintf("/api/v1/organizations/%s/tasks", org), params)
	if err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	var result models.TaskListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return s.errorResult(&ToolError{Code: "API_ERROR", Message: err.Error()})
	}

	tasks := result.Items

	// Build response data
	taskList := make([]map[string]interface{}, 0, len(tasks))
	for _, t := range tasks {
		taskList = append(taskList, map[string]interface{}{
			"task":        t.ID,
			"device_uuid": t.DeviceUUID,
			"type":        t.Type,
			"status":      t.Status,
			"created_at":  t.CreatedAt,
			"expires_at":  t.ExpiresAt,
		})
	}

	return s.successResultWithPagination(map[string]interface{}{
		"tasks": taskList,
	}, page, perPage, result.Total)
}
