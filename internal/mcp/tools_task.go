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
	Organization    string     `json:"organization,omitempty"`
	Status          stringList `json:"status,omitempty"`
	Type            stringList `json:"type,omitempty"`
	Device          string     `json:"device,omitempty"`
	Expired         *bool      `json:"expired,omitempty"`
	CreatedAfter    string     `json:"created_after,omitempty"`
	CreatedBefore   string     `json:"created_before,omitempty"`
	ExcludeType     stringList `json:"exclude_type,omitempty"`
	Origin          stringList `json:"origin,omitempty"`
	Schedule        string     `json:"schedule,omitempty"`
	DeviceExact     stringList `json:"device_exact,omitempty"`
	Search          string     `json:"search,omitempty"`
	ScheduledAfter  string     `json:"scheduled_after,omitempty"`
	ScheduledBefore string     `json:"scheduled_before,omitempty"`
	IncludeActive   bool       `json:"include_active,omitempty"`
	RunID           string     `json:"run_id,omitempty"`
	RequestedBy     string     `json:"requested_by,omitempty"`
	SortBy          string     `json:"sort_by,omitempty"`
	Page            int        `json:"page,omitempty"`
	PerPage         int        `json:"per_page,omitempty"`
}

type taskIDInput struct {
	Task    string `json:"task"`
	Confirm bool   `json:"confirm,omitempty"`
}

// syncResultGuide tells a caller how to read the result items the CLI renders
// with symbols: what a warning and a renumbered local rule mean is otherwise
// left to the caller to guess from the raw message.
const syncResultGuide = "A SYNC task's message is a JSON envelope: message (the summary line), " +
	"results[] (type, name, action, status, code, error) and validation_errors[]. " +
	"A result with status warning is informational; error and blocked results are failures. " +
	"A local rule that rule placement moved has action renumbered, with its old and new sequence in before/after."

// taskFieldsGuide tells a caller what each task says about where it came from
// and how it ran.
const taskFieldsGuide = "Each task has origin (MANUAL, SCHEDULE or FIRMWARE); schedule, the spec that created it " +
	"({name, spec_code, kind, next_run_at, firmware_track}, next_run_at null while disabled) or null; " +
	"device_deleted; on a PENDING or SCHEDULED task device_online and device_last_seen_at, why it may still wait; " +
	"requested_by ({email, name}, both null once the account is deleted; null for a schedule's or a track's task); " +
	"client (WEB, CLI, MCP or API, a personal access token); run_id, shared by the tasks of one request or one " +
	"schedule fire; dispatched_at, completed_at and duration_seconds between them; and last_response_at. " +
	"Times are UTC. A key the control plane does not send is null."

// firmwareTrackResultGuide tells a caller how to read the result of a
// FIRMWARE_TRACK task, which the control plane stores as JSON in the message:
// the parsed block is returned beside it as firmware_track_result.
var firmwareTrackResultGuide = "A FIRMWARE_TRACK task is a step of a firmware track (enroll, advance or unenroll) " +
	"created by the control plane, never by a client. Once it completes, firmware_track_result holds the parsed result: " +
	"action, track, outcome (success, noop, held, released), reason (a closed code; ndcli.firmware.status lists them), " +
	"drift (the pin had been changed outside NetDefense), reinstall_entries, and from_version, to_version, " +
	"target_version and pinned_version when the device reported them. An advance that carried a major upgrade " +
	"(the move to the next OPNsense series) also has kind \"major\" on every answer from the moment the agent decided " +
	"to run it, a hold included, with target_version the series, and notes, what the agent found on the way, each one of: " +
	firmwareNoteGuide + ". " +
	"A task that is still pending or that failed carries a plain-text message instead."

// registerTaskTools registers task management tools.
func (s *Server) registerTaskTools() {
	// ndcli.task.list
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.task.list",
		Description: "List tasks (sync, ping, reboot, plugin-install, ...) across an organization with optional filters. " +
			taskFieldsGuide + " " +
			"A FIRMWARE_TRACK task carries its parsed result as firmware_track_result once it has completed. " +
			"The filters from exclude_type on need a recent control plane: one that does not apply a filter yet " +
			"answers SERVER_TOO_OLD, naming it, instead of a list; call again without it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"status":         stringEnumArrayProperty("Only tasks with any of these statuses (omit for all)", models.TaskStatuses),
				"type":           stringEnumArrayProperty("Only tasks of any of these types (omit for all)", models.TaskTypes),
				"device":         stringProperty("Filter by device name (regex); device_exact takes exact names"),
				"expired":        boolProperty("true = only expired, false = only non-expired (omit for both)"),
				"created_after":  stringProperty("Filter tasks created after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"created_before": stringProperty("Filter tasks created before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"exclude_type":   stringEnumArrayProperty("Leave out tasks of these types", models.TaskTypes),
				"origin":         stringEnumArrayProperty("Only tasks from any of these sources (omit for all)", models.TaskOrigins),
				"schedule":       stringProperty("Only tasks the specs of the schedule with exactly this name created"),
				"device_exact":   stringArrayProperty("Only tasks of the devices with exactly these names"),
				"search":         stringProperty("Only tasks whose code or device name contains this text (plain text, not a pattern)"),
				"scheduled_after": stringProperty("Only tasks scheduled to run at or after this time: 2h (ago), +2h (from now), " +
					"or ISO 8601; a time without an offset is UTC"),
				"scheduled_before": stringProperty("Only tasks scheduled to run at or before this time, in the forms scheduled_after takes"),
				"include_active": boolProperty("With created_after or created_before: also list every PENDING, SCHEDULED " +
					"and IN_PROGRESS task, whenever it was created"),
				"run_id":       stringProperty("Only the tasks of this run (a task's run_id: one request, or one fire of a schedule spec)"),
				"requested_by": stringProperty("Only the tasks the account with this email asked for"),
				"sort_by": stringProperty("Sort as field:direction (default created_at:desc); fields created_at, updated_at, " +
					"expires_at, scheduled_at, status, type, device"),
				"page":     intProperty("Page number", 1),
				"per_page": intProperty("Items per page (max 100)", 30),
			},
		},
	}, s.handleTaskList)

	// ndcli.task.describe
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.task.describe",
		Description: "Get full details for a single task by its task code. " + taskFieldsGuide + " " +
			syncResultGuide + " " + firmwareTrackResultGuide,
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
		Statuses:        input.Status,
		Types:           input.Type,
		Device:          input.Device,
		CreatedAfter:    input.CreatedAfter,
		CreatedBefore:   input.CreatedBefore,
		ExcludeTypes:    input.ExcludeType,
		Origins:         input.Origin,
		Schedule:        input.Schedule,
		Devices:         input.DeviceExact,
		Search:          input.Search,
		ScheduledAfter:  input.ScheduledAfter,
		ScheduledBefore: input.ScheduledBefore,
		IncludeActive:   input.IncludeActive,
		RunID:           input.RunID,
		RequestedBy:     input.RequestedBy,
		SortBy:          input.SortBy,
		Page:            input.Page,
		PerPage:         input.PerPage,
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
	summary := map[string]interface{}{
		"task":        t.ID,
		"type":        t.Type,
		"status":      t.Status,
		"device_uuid": t.DeviceUUID,
		"device":      t.DeviceName,
		"created_at":  t.CreatedAt,
		"expires_at":  t.ExpiresAt,
	}
	addTaskProvenance(summary, t)
	addFirmwareTrackResult(summary, t)
	return summary
}

// addTaskProvenance joins what the control plane reports about where a task
// came from and how it ran: the source, how its device stands while it waits,
// who asked and from which client, its run, and when it was sent and finished,
// with the duration between the two. A key the server sends as null, or does
// not send because it predates it, is null.
func addTaskProvenance(data map[string]interface{}, t *models.Task) {
	var origin interface{}
	if t.Origin != "" {
		origin = t.Origin
	}
	data["device_deleted"] = t.DeviceDeleted
	data["origin"] = origin
	data["schedule"] = t.Schedule
	data["device_online"] = t.DeviceOnline
	data["device_last_seen_at"] = t.DeviceLastSeenAt
	data["last_response_at"] = t.LastResponseAt
	data["requested_by"] = t.RequestedBy
	data["client"] = t.Client
	data["run_id"] = t.RunID
	data["dispatched_at"] = t.DispatchedAt
	data["completed_at"] = t.CompletedAt
	data["duration_seconds"] = t.DurationSeconds()
}

// addFirmwareTrackResult joins the parsed result of a completed FIRMWARE_TRACK
// task to the task's data, so a caller reads outcome and reason as fields
// instead of decoding JSON held in a string. Any other task, and a message
// that is not a result, adds nothing: the key is absent, never null.
func addFirmwareTrackResult(data map[string]interface{}, t *models.Task) {
	if t.Type != models.TaskTypeFirmwareTrack {
		return
	}
	if result := models.ParseFirmwareTrackResult(t.Message); result != nil {
		data["firmware_track_result"] = result
	}
}

func taskFull(t *models.Task) map[string]interface{} {
	full := map[string]interface{}{
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
		"scheduled_at":  t.ScheduledAt,
		"expires_at":    t.ExpiresAt,
	}
	addTaskProvenance(full, t)
	addFirmwareTrackResult(full, t)
	return full
}
