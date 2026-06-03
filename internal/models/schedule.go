package models

import "encoding/json"

// Schedule is a pure cadence definition (cron + timezone). It no longer
// carries task-type or target information; those live in ScheduledTask specs
// that are registered against a named schedule via `ndcli run --schedule` or
// `ndcli sync apply --schedule`.
//
// GET /organizations/{org}/schedules         → []Schedule (no ScheduledTasks)
// GET /organizations/{org}/schedules/{name}  → Schedule with nested ScheduledTasks
type Schedule struct {
	OrganizationName string          `json:"organization_name"` // set by server on all responses
	Name             string          `json:"name"`
	Enabled          bool            `json:"enabled"`
	Schedule         string          `json:"schedule"` // cron expression
	Timezone         string          `json:"timezone"`
	LastFiredAt      *FlexibleTime   `json:"last_fired_at"`
	NextRunAt        *FlexibleTime   `json:"next_run_at"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        FlexibleTime    `json:"created_at"`
	UpdatedAt        FlexibleTime    `json:"updated_at"`
	ScheduledTasks   []ScheduledTask `json:"scheduled_tasks,omitempty"` // populated only on GET-one
}

// ScheduledTask is a registered task spec associated with a schedule. Addressed
// by Code (server-generated 8-char base62); Kind is "RUN" or "SYNC".
// Request is the verbatim request body stored by NDManager (device names,
// OUs, task type, payload, sync filters, …) — kept as raw JSON because the
// shape varies by Kind and the CLI only needs to display it, not parse it.
//
// ScheduleName is present in org-wide list responses
// (GET /organizations/{org}/scheduled-tasks) so each row is self-describing.
type ScheduledTask struct {
	Code         string          `json:"code"`
	ScheduleName string          `json:"schedule_name"` // set on org-wide list items
	Kind         string          `json:"kind"`          // RUN | SYNC
	Request      json.RawMessage `json:"request"`
	Enabled      bool            `json:"enabled"`
	CreatedBy    string          `json:"created_by"`
	LastFiredAt  *FlexibleTime   `json:"last_fired_at"`
	CreatedAt    FlexibleTime    `json:"created_at"`
	UpdatedAt    FlexibleTime    `json:"updated_at"`
}

// ScheduleListResponse is the envelope returned by
// GET /organizations/{org}/schedules.
type ScheduleListResponse struct {
	Items []Schedule `json:"items"`
	Total int        `json:"total"`
}

// ScheduledTaskListResponse is the envelope returned by
// GET /organizations/{org}/scheduled-tasks[?schedule=<name>].
type ScheduledTaskListResponse struct {
	Items []ScheduledTask `json:"items"`
	Total int             `json:"total"`
}

// ScheduledTaskRegisterResult is the 201 body returned when a run or sync
// request includes a "schedule" field — instead of a task table the server
// returns the registered spec descriptor.
type ScheduledTaskRegisterResult struct {
	Code         string          `json:"code"`
	Kind         string          `json:"kind"`
	Request      json.RawMessage `json:"request"`
	Enabled      bool            `json:"enabled"`
	ScheduleName string          `json:"schedule_name"` // name of the parent schedule
	CreatedBy    string          `json:"created_by"`
	LastFiredAt  *FlexibleTime   `json:"last_fired_at"`
	CreatedAt    FlexibleTime    `json:"created_at"`
	UpdatedAt    FlexibleTime    `json:"updated_at"`
}
