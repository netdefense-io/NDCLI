package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// ── input structs ─────────────────────────────────────────────────────────────

type scheduleListInput struct {
	Organization string `json:"organization,omitempty"`
}

type scheduleNameInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
}

type scheduleDeleteInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// scheduleCreateInput is cadence-only. Task type and targets are registered
// separately via ndcli.run.* or ndcli.sync.apply with a schedule field.
type scheduleCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Cron         string `json:"cron"`
	Timezone     string `json:"timezone,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
}

// scheduleTaskListInput: schedule is optional (org-wide when omitted).
type scheduleTaskListInput struct {
	Organization string `json:"organization,omitempty"`
	Schedule     string `json:"schedule,omitempty"` // optional filter
}

type scheduleTaskCodeInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
}

type scheduleTaskRemoveInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// ── registration ──────────────────────────────────────────────────────────────

// registerScheduleTools registers all ndcli.schedule.* MCP tools.
func (s *Server) registerScheduleTools() {
	// ndcli.schedule.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.list",
		Description: "List all cadence schedules for an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
			},
		},
	}, s.handleScheduleList)

	// ndcli.schedule.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.describe",
		Description: "Get full details for a named schedule, including its registered task specs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Schedule name"),
			},
			"required": []string{"name"},
		},
	}, s.handleScheduleDescribe)

	// ndcli.schedule.create
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.create",
		Description: "Create a new cadence schedule (cron + timezone). Task specs are registered separately via ndcli.run.* or ndcli.sync.apply with a schedule field.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Unique schedule name within the organization"),
				"cron":         stringProperty("Cron expression (e.g. \"0 2 * * 0\" for 02:00 on Sundays)"),
				"timezone":     stringProperty("IANA timezone for the cron expression (default: UTC)"),
				"enabled":      boolProperty("Whether the schedule is active on creation (default: true)"),
			},
			"required": []string{"name", "cron"},
		},
	}, s.handleScheduleCreate)

	// ndcli.schedule.enable
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.enable",
		Description: "Enable a cadence schedule so all its specs fire on cron.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Schedule name"),
			},
			"required": []string{"name"},
		},
	}, s.handleScheduleEnable)

	// ndcli.schedule.disable
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.disable",
		Description: "Disable a cadence schedule so no specs fire.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Schedule name"),
			},
			"required": []string{"name"},
		},
	}, s.handleScheduleDisable)

	// ndcli.schedule.delete
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.delete",
		Description: "Delete a cadence schedule and all its registered specs. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Schedule name to delete"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleScheduleDelete)

	// ndcli.schedule.tasks.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.tasks.list",
		Description: "List registered task specs org-wide. Omit schedule for all specs; provide schedule to filter to one cadence.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"schedule":     stringProperty("Optional: limit to specs belonging to this schedule name"),
			},
		},
	}, s.handleScheduleTasksList)

	// ndcli.schedule.tasks.enable
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.tasks.enable",
		Description: "Enable a registered task spec by its code (org-wide, no schedule name needed).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty("Task spec code (8-char base62)"),
			},
			"required": []string{"code"},
		},
	}, s.handleScheduleTasksEnable)

	// ndcli.schedule.tasks.disable
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.tasks.disable",
		Description: "Disable a registered task spec by its code (org-wide, no schedule name needed).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty("Task spec code (8-char base62)"),
			},
			"required": []string{"code"},
		},
	}, s.handleScheduleTasksDisable)

	// ndcli.schedule.tasks.remove
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.schedule.tasks.remove",
		Description: "Remove a task spec by its code (org-wide, no schedule name needed). Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty("Task spec code to remove"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"code"},
		},
	}, s.handleScheduleTasksRemove)
}

// ── schedule handlers ─────────────────────────────────────────────────────────

func (s *Server) handleScheduleList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.ScheduleList(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"schedules": result.Schedules,
		"total":     result.Total,
	}, "")
}

func (s *Server) handleScheduleDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	sch, err := s.svc.ScheduleGet(apiCtx, org, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(sch, "")
}

func (s *Server) handleScheduleCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	isEnabled := true
	if input.Enabled != nil {
		isEnabled = *input.Enabled
	}
	tz := input.Timezone
	if tz == "" {
		tz = "UTC"
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	sch, err := s.svc.ScheduleCreate(apiCtx, org, service.ScheduleCreateOpts{
		Name:     input.Name,
		Cron:     input.Cron,
		Timezone: tz,
		Enabled:  isEnabled,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(sch, "Schedule created")
}

func (s *Server) handleScheduleEnable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	sch, err := s.svc.ScheduleSetEnabled(apiCtx, org, input.Name, true)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(sch, "Schedule enabled")
}

func (s *Server) handleScheduleDisable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	sch, err := s.svc.ScheduleSetEnabled(apiCtx, org, input.Name, false)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(sch, "Schedule disabled")
}

func (s *Server) handleScheduleDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleDeleteInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete schedule", input.Name)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.ScheduleDelete(apiCtx, org, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"deleted": true,
		"name":    input.Name,
	}, "Schedule deleted")
}

// ── task spec handlers ────────────────────────────────────────────────────────

func (s *Server) handleScheduleTasksList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleTaskListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	tasks, err := s.svc.ScheduleTaskList(apiCtx, org, input.Schedule)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"tasks": tasks,
		"total": len(tasks),
	}, "")
}

func (s *Server) handleScheduleTasksEnable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleTaskCodeInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	spec, err := s.svc.ScheduledTaskSetEnabledByCode(apiCtx, org, input.Code, true)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(spec, "Spec enabled")
}

func (s *Server) handleScheduleTasksDisable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleTaskCodeInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	spec, err := s.svc.ScheduledTaskSetEnabledByCode(apiCtx, org, input.Code, false)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(spec, "Spec disabled")
}

func (s *Server) handleScheduleTasksRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[scheduleTaskRemoveInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove spec", input.Code)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.ScheduledTaskRemoveByCode(apiCtx, org, input.Code); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"removed": true,
		"code":    input.Code,
	}, "Spec removed")
}
