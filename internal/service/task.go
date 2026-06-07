package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// TaskListOpts collects every filter NDManager's /api/v1/tasks endpoint
// accepts. Empty fields are omitted; ExpiredSet must be true for Expired to
// be sent (so callers can distinguish "no filter" from "explicitly false").
type TaskListOpts struct {
	Status        string
	Type          string
	Device        string
	Expired       bool
	ExpiredSet    bool
	CreatedAfter  string
	CreatedBefore string
	SortBy        string
	Page          int
	PerPage       int
}

// TaskListResult mirrors the paginated task list response with resolved
// pagination defaults.
type TaskListResult struct {
	Tasks   []models.Task
	Total   int
	Page    int
	PerPage int
}

// TaskList returns a paginated, filtered list of tasks for the organization.
func (s *Service) TaskList(ctx context.Context, org string, opts TaskListOpts) (*TaskListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 30
	}

	params := map[string]string{
		"organization": org,
		"page":         strconv.Itoa(page),
		"per_page":     strconv.Itoa(perPage),
	}
	if opts.Status != "" {
		params["status"] = opts.Status
	}
	if opts.Type != "" {
		params["type"] = opts.Type
	}
	if opts.Device != "" {
		params["device_name"] = opts.Device
	}
	if opts.ExpiredSet {
		params["expired"] = strconv.FormatBool(opts.Expired)
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}

	for _, f := range [][2]string{
		{opts.CreatedAfter, "created_after"},
		{opts.CreatedBefore, "created_before"},
	} {
		if f[0] == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f[0])
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f[1], err),
				Err:     err,
			}
		}
		params[f[1]] = parsed
	}

	resp, err := s.api.Get(ctx, "/api/v1/tasks", params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.TaskListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &TaskListResult{
		Tasks:   result.Items,
		Total:   result.Total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

// TaskGet returns a single task by ID. Tasks live at the platform level (not
// org-scoped on this endpoint), so org is not required.
func (s *Service) TaskGet(ctx context.Context, taskID string) (*models.Task, error) {
	if taskID == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "task id is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var task models.Task
	if err := api.ParseResponse(resp, &task); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &task, nil
}

// TaskCancel cancels a pending or scheduled task.
func (s *Service) TaskCancel(ctx context.Context, taskID string) error {
	if taskID == "" {
		return &Error{Code: CodeInvalidInput, Message: "task id is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/tasks/%s/cancel", taskID), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// RunOpts is the typed input for Service.Run. The Type uses NDDataModels
// enum strings (PING/SHUTDOWN/...). At least one of Devices/OUs/AllDevices
// must be set; the friendly-name mapping (`ping` → PING, etc.) lives in
// cli/run.go so it stays out of MCP-facing surface area.
//
// Schedule and ScheduledAt are mutually exclusive. When Schedule is set, the
// server registers a ScheduledTask spec instead of creating tasks immediately;
// use RunRegisterSpec for that path so the two response types stay separate.
type RunOpts struct {
	Type        string                 // PING, SHUTDOWN, REBOOT, RESTART, PLUGIN_INSTALL, FIRMWARE_UPGRADE
	Payload     map[string]interface{} // type-specific; PING: target+count, PLUGIN_INSTALL: target_version, FIRMWARE_UPGRADE: mode/reboot/check_first/dry_run
	Devices     []string               // repeatable
	OUs         []string               // repeatable
	AllDevices  bool                   // mutually exclusive with Devices/OUs
	ScheduledAt string                 // RFC3339; empty = run immediately; mutually exclusive with Schedule
	Schedule    string                 // schedule name; when set, call RunRegisterSpec instead
}

var validRunTypes = map[string]bool{
	models.TaskTypePing:            true,
	models.TaskTypeShutdown:        true,
	models.TaskTypeReboot:          true,
	models.TaskTypeRestart:         true,
	models.TaskTypePluginInstall:   true,
	models.TaskTypeFirmwareUpgrade: true,
}

// Run posts to POST /api/v1/organizations/{org}/tasks — the server-side
// fan-out endpoint. NDManager resolves devices/OUs/all, creates one task
// per resolved device, and returns the list. SCHEDULED tasks come back
// with status=SCHEDULED; immediate tasks come back PENDING.
func (s *Service) Run(ctx context.Context, org string, opts RunOpts) (*models.RunResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	taskType := strings.ToUpper(opts.Type)
	if !validRunTypes[taskType] {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid task type: %s", opts.Type),
		}
	}
	if !opts.AllDevices && len(opts.Devices) == 0 && len(opts.OUs) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "at least one of --device, --ou, or --org is required"}
	}
	if opts.AllDevices && (len(opts.Devices) > 0 || len(opts.OUs) > 0) {
		return nil, &Error{Code: CodeInvalidInput, Message: "--org cannot be combined with --device or --ou"}
	}

	body := map[string]interface{}{
		"type": taskType,
		"targets": map[string]interface{}{
			"devices": nonNilStrings(opts.Devices),
			"ous":     nonNilStrings(opts.OUs),
			"all":     opts.AllDevices,
		},
	}
	if opts.Payload != nil && len(opts.Payload) > 0 {
		body["payload"] = opts.Payload
	}
	if opts.ScheduledAt != "" {
		body["scheduled_at"] = opts.ScheduledAt
	}

	endpoint := fmt.Sprintf("/api/v1/organizations/%s/tasks", url.PathEscape(org))
	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.RunResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// RunRegisterSpec posts to POST /api/v1/organizations/{org}/tasks with a
// "schedule" field, which instructs NDManager to register a recurring
// ScheduledTask spec instead of creating tasks immediately. The server returns
// a 201 spec descriptor rather than a task table.
func (s *Service) RunRegisterSpec(ctx context.Context, org string, opts RunOpts) (*models.ScheduledTaskRegisterResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if opts.Schedule == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule name is required for spec registration"}
	}
	taskType := strings.ToUpper(opts.Type)
	if !validRunTypes[taskType] {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid task type: %s", opts.Type),
		}
	}
	if !opts.AllDevices && len(opts.Devices) == 0 && len(opts.OUs) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "at least one of --device, --ou, or --org is required"}
	}
	if opts.AllDevices && (len(opts.Devices) > 0 || len(opts.OUs) > 0) {
		return nil, &Error{Code: CodeInvalidInput, Message: "--org cannot be combined with --device or --ou"}
	}

	body := map[string]interface{}{
		"type": taskType,
		"targets": map[string]interface{}{
			"devices": nonNilStrings(opts.Devices),
			"ous":     nonNilStrings(opts.OUs),
			"all":     opts.AllDevices,
		},
		"schedule": opts.Schedule,
	}
	if opts.Payload != nil && len(opts.Payload) > 0 {
		body["payload"] = opts.Payload
	}

	endpoint := fmt.Sprintf("/api/v1/organizations/%s/tasks", url.PathEscape(org))
	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.ScheduledTaskRegisterResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// nonNilStrings returns an empty (non-nil) slice for a nil input so JSON-encoded
// task targets serialize as [] rather than null. NDManager validates
// targets.devices / targets.ous as lists and rejects null with
// "Input should be a valid list" — this bit callers (e.g. the TUI device
// actions) that target by device only and leave OUs nil.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
