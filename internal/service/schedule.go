package service

import (
	"context"
	"fmt"
	"net/url"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// scheduleBase returns the base path for the schedule collection.
func scheduleBase(org string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/schedules", url.PathEscape(org))
}

// schedulePath returns the path for a single schedule by name.
func schedulePath(org, name string) string {
	return fmt.Sprintf("%s/%s", scheduleBase(org), url.PathEscape(name))
}

// scheduledTasksBase returns the org-wide scheduled-tasks collection path.
func scheduledTasksBase(org string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/scheduled-tasks", url.PathEscape(org))
}

// scheduledTaskPath returns the path for a single spec addressed by code.
func scheduledTaskPath(org, code string) string {
	return fmt.Sprintf("%s/%s", scheduledTasksBase(org), url.PathEscape(code))
}

// ── Schedule CRUD ─────────────────────────────────────────────────────────────

// ScheduleListResult is the typed result for ScheduleList.
type ScheduleListResult struct {
	Schedules []models.Schedule
	Total     int
}

// ScheduleList returns all cadence schedules for the organization. The items
// do not include the nested scheduled_tasks — call ScheduleGet for that.
func (s *Service) ScheduleList(ctx context.Context, org string) (*ScheduleListResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	resp, err := s.api.Get(ctx, scheduleBase(org), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.ScheduleListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ScheduleListResult{
		Schedules: result.Items,
		Total:     result.Total,
	}, nil
}

// ScheduleGet returns a single schedule by name, including the nested
// scheduled_tasks list.
func (s *Service) ScheduleGet(ctx context.Context, org, name string) (*models.Schedule, error) {
	switch {
	case org == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case name == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule name is required"}
	}
	resp, err := s.api.Get(ctx, schedulePath(org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var sch models.Schedule
	if err := api.ParseResponse(resp, &sch); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &sch, nil
}

// ScheduleCreateOpts collects the cadence-only fields for creating a schedule.
// Target selection and task type live in the ScheduledTask specs registered
// separately via `ndcli run --schedule` / `ndcli sync apply --schedule`.
type ScheduleCreateOpts struct {
	Name     string
	Cron     string // cron expression
	Timezone string // defaults to UTC
	Enabled  bool
}

// ScheduleCreate creates a new cadence schedule.
func (s *Service) ScheduleCreate(ctx context.Context, org string, opts ScheduleCreateOpts) (*models.Schedule, error) {
	switch {
	case org == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case opts.Name == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "--name is required"}
	case opts.Cron == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "--cron is required"}
	}

	tz := opts.Timezone
	if tz == "" {
		tz = "UTC"
	}

	body := map[string]interface{}{
		"name":     opts.Name,
		"schedule": opts.Cron,
		"timezone": tz,
		"enabled":  opts.Enabled,
	}

	resp, err := s.api.Post(ctx, scheduleBase(org), body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var sch models.Schedule
	if err := api.ParseResponse(resp, &sch); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &sch, nil
}

// ScheduleDelete deletes a schedule by name. All registered task specs are
// also deleted.
func (s *Service) ScheduleDelete(ctx context.Context, org, name string) error {
	switch {
	case org == "":
		return &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case name == "":
		return &Error{Code: CodeInvalidInput, Message: "schedule name is required"}
	}
	resp, err := s.api.Delete(ctx, schedulePath(org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// ScheduleSetEnabled calls the /enable or /disable action endpoint on the
// cadence schedule. Returns the updated schedule so callers can render it.
func (s *Service) ScheduleSetEnabled(ctx context.Context, org, name string, enabled bool) (*models.Schedule, error) {
	switch {
	case org == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case name == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule name is required"}
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("%s/%s", schedulePath(org, name), action), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var sch models.Schedule
	if err := api.ParseResponse(resp, &sch); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &sch, nil
}

// ── Org-wide scheduled-task spec operations ───────────────────────────────────

// ScheduleTaskList returns all registered task specs across the organization.
// When scheduleFilter is non-empty, only specs belonging to that named schedule
// are returned (server-side ?schedule= query param). An unknown schedule name
// returns an empty list rather than an error.
func (s *Service) ScheduleTaskList(ctx context.Context, org, scheduleFilter string) ([]models.ScheduledTask, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	var params map[string]string
	if scheduleFilter != "" {
		params = map[string]string{"schedule": scheduleFilter}
	}
	resp, err := s.api.Get(ctx, scheduledTasksBase(org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var envelope models.ScheduledTaskListResponse
	if err := api.ParseResponse(resp, &envelope); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return envelope.Items, nil
}

// ScheduledTaskGet returns a single registered task spec by its org-wide
// code. Returns nil (not an error) when the code is not found so callers
// can treat the spec kind as unknown without hard-failing.
func (s *Service) ScheduledTaskGet(ctx context.Context, org, code string) (*models.ScheduledTask, error) {
	switch {
	case org == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case code == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "task spec code is required"}
	}
	resp, err := s.api.Get(ctx, scheduledTaskPath(org, code), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var spec models.ScheduledTask
	if err := api.ParseResponse(resp, &spec); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &spec, nil
}

// ScheduledTaskSetEnabledByCode enables or disables a registered task spec
// addressed by its org-wide code. Returns the updated spec descriptor.
func (s *Service) ScheduledTaskSetEnabledByCode(ctx context.Context, org, code string, enabled bool) (*models.ScheduledTask, error) {
	switch {
	case org == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case code == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "task spec code is required"}
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	path := fmt.Sprintf("%s/%s", scheduledTaskPath(org, code), action)
	resp, err := s.api.Post(ctx, path, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var spec models.ScheduledTask
	if err := api.ParseResponse(resp, &spec); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &spec, nil
}

// ScheduledTaskRemoveByCode deletes a registered task spec by its org-wide
// code. The server returns {"deleted":true,"code":...,"organization_name":...}.
func (s *Service) ScheduledTaskRemoveByCode(ctx context.Context, org, code string) error {
	switch {
	case org == "":
		return &Error{Code: CodeInvalidInput, Message: "organization is required"}
	case code == "":
		return &Error{Code: CodeInvalidInput, Message: "task spec code is required"}
	}
	resp, err := s.api.Delete(ctx, scheduledTaskPath(org, code))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
