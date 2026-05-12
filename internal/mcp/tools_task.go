package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type taskListInput struct {
	Organization  string `json:"organization,omitempty"`
	Status        string `json:"status,omitempty"`
	Type          string `json:"type,omitempty"`
	Device        string `json:"device,omitempty"`
	Expired       *bool  `json:"expired,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
}

type taskIDInput struct {
	Task    string `json:"task"`
	Confirm bool   `json:"confirm,omitempty"`
}

// registerTaskTools registers task management tools.
func (s *Server) registerTaskTools() {
	// ndcli.task.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.task.list",
		Description: "List tasks (sync, ping, reboot, plugin-install, ...) across an organization with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"status":         stringEnumProperty("Filter by status", []string{"PENDING", "SCHEDULED", "IN_PROGRESS", "COMPLETED", "FAILED", "CANCELLED", "EXPIRED"}),
				"type":           stringEnumProperty("Filter by task type", []string{"BACKUP", "PING", "PLUGIN_INSTALL", "PULL", "REBOOT", "RESTART", "SHUTDOWN", "SYNC"}),
				"device":         stringProperty("Filter by device name (regex)"),
				"expired":        boolProperty("true = only expired, false = only non-expired (omit for both)"),
				"created_after":  stringProperty("Filter tasks created after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"created_before": stringProperty("Filter tasks created before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"sort_by":        stringProperty("Sort field and direction (default created_at:desc)"),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page (max 100)", 30),
			},
		},
	}, s.handleTaskList)

	// ndcli.task.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.task.describe",
		Description: "Get full details for a single task by its task code.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task": stringProperty("Task code (8-char base62)"),
			},
			"required": []string{"task"},
		},
	}, s.handleTaskDescribe)

	// ndcli.task.cancel
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.task.cancel",
		Description: "Cancel a pending or scheduled task. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task":    stringProperty("Task code to cancel"),
				"confirm": confirmProperty(),
			},
			"required": []string{"task"},
		},
	}, s.handleTaskCancel)

}

func (s *Server) handleTaskList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[taskListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	opts := service.TaskListOpts{
		Status:        input.Status,
		Type:          input.Type,
		Device:        input.Device,
		CreatedAfter:  input.CreatedAfter,
		CreatedBefore: input.CreatedBefore,
		SortBy:        input.SortBy,
		Page:          input.Page,
		PerPage:       input.PerPage,
	}
	if input.Expired != nil {
		opts.Expired = *input.Expired
		opts.ExpiredSet = true
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.TaskList(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}

	tasks := make([]map[string]interface{}, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		tasks = append(tasks, taskSummary(&t))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"tasks": tasks,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleTaskDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[taskIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	task, err := s.svc.TaskGet(apiCtx, input.Task)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"task": taskFull(task),
	}, "")
}

func (s *Server) handleTaskCancel(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[taskIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("cancel", input.Task)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TaskCancel(apiCtx, input.Task); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"task":   input.Task,
		"action": "cancelled",
	}, fmt.Sprintf("Task %s cancelled", input.Task))
}

func taskSummary(t *models.Task) map[string]interface{} {
	return map[string]interface{}{
		"task":        t.ID,
		"type":        t.Type,
		"status":      t.Status,
		"device_uuid": t.DeviceUUID,
		"device":      t.DeviceName,
		"created_at":  t.CreatedAt,
		"expires_at":  t.ExpiresAt,
	}
}

func taskFull(t *models.Task) map[string]interface{} {
	return map[string]interface{}{
		"task":          t.ID,
		"type":          t.Type,
		"status":        t.Status,
		"organization":  t.Organization,
		"device":        t.DeviceName,
		"device_uuid":   t.DeviceUUID,
		"payload":       t.Payload,
		"message":       t.Message,
		"error_message": t.ErrorMessage,
		"created_at":    t.CreatedAt,
		"updated_at":    t.UpdatedAt,
		"expires_at":    t.ExpiresAt,
		"started_at":    t.StartedAt,
		"completed_at":  t.CompletedAt,
	}
}
