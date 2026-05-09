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

// TaskCreateOpts carries the task type and any type-specific payload fields.
// Only one PingTarget+PingCount or InstallVersion is read, depending on Type.
type TaskCreateOpts struct {
	Type           string // PING, SHUTDOWN, REBOOT, RESTART, PLUGIN_INSTALL
	PingTarget     string // required for PING
	PingCount      int    // optional for PING; <=0 means use server default
	InstallVersion string // optional for PLUGIN_INSTALL
}

// validCreateTypes enumerates the task types create accepts. SYNC, PULL,
// BACKUP, CONNECT have dedicated endpoints elsewhere.
var validCreateTypes = map[string]bool{
	models.TaskTypePing:     true,
	models.TaskTypeShutdown: true,
	models.TaskTypeReboot:   true,
	models.TaskTypeRestart:  true,
	"PLUGIN_INSTALL":        true,
}

// TaskCreate creates an on-demand task for a device.
func (s *Service) TaskCreate(ctx context.Context, org, deviceName string, opts TaskCreateOpts) (*models.Task, error) {
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	taskType := strings.ToUpper(opts.Type)
	if !validCreateTypes[taskType] {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid task type: %s. Valid types: PING, SHUTDOWN, REBOOT, RESTART, PLUGIN_INSTALL", opts.Type),
		}
	}

	var body interface{}
	switch taskType {
	case models.TaskTypePing:
		if opts.PingTarget == "" {
			return nil, &Error{Code: CodeInvalidInput, Message: "PING requires a target IP or host"}
		}
		payload := map[string]interface{}{"target": opts.PingTarget}
		if opts.PingCount > 0 {
			payload["count"] = opts.PingCount
		}
		body = map[string]interface{}{"payload": payload}
	case "PLUGIN_INSTALL":
		payload := map[string]interface{}{}
		if opts.InstallVersion != "" {
			payload["target_version"] = opts.InstallVersion
		}
		body = map[string]interface{}{"payload": payload}
	}

	q := url.Values{}
	q.Set("task_type", taskType)
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/task?%s", org, deviceName, q.Encode())

	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var task models.Task
	if err := api.ParseResponse(resp, &task); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &task, nil
}
