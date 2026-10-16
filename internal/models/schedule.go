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

// The kinds of work a ScheduledTask spec describes (NDDataModels'
// ScheduledTaskKind).
const (
	ScheduledTaskKindRun      = "RUN"
	ScheduledTaskKindSync     = "SYNC"
	ScheduledTaskKindBackup   = "BACKUP"
	ScheduledTaskKindFirmware = "FIRMWARE"
)

// ScheduledTask is a registered task spec associated with a schedule. Addressed
// by Code (server-generated 8-char base62); Kind is RUN, SYNC, BACKUP or
// FIRMWARE. A FIRMWARE spec is a firmware track's update window: it belongs to
// the track, which attaches and detaches it ('ndcli firmware track schedule');
// NDManager refuses to enable, disable or remove it here.
// Request is the verbatim request body stored by NDManager (device names,
// OUs, task type, payload, sync filters, …) — kept as raw JSON because the
// shape varies by Kind and the CLI only needs to display it, not parse it.
//
// ScheduleName is present in org-wide list responses
// (GET /organizations/{org}/scheduled-tasks) so each row is self-describing.
//
// FirmwareTrack names the track a FIRMWARE spec is the window of (fast,
// regular or stable) and is null for every other kind. NDManager sends the key
// on every descriptor (the org-wide list, the read by code, the per-schedule
// list and the specs nested in a schedule), so it is a pointer: a descriptor
// decoded and printed again keeps the null instead of an empty string.
type ScheduledTask struct {
	Code          string          `json:"code"`
	ScheduleName  string          `json:"schedule_name"`  // set on org-wide list items
	Kind          string          `json:"kind"`           // RUN | SYNC | BACKUP | FIRMWARE
	FirmwareTrack *string         `json:"firmware_track"` // fast | regular | stable for FIRMWARE, else null
	Request       json.RawMessage `json:"request"`
	Enabled       bool            `json:"enabled"`
	CreatedBy     string          `json:"created_by"`
	LastFiredAt   *FlexibleTime   `json:"last_fired_at"`
	CreatedAt     FlexibleTime    `json:"created_at"`
	UpdatedAt     FlexibleTime    `json:"updated_at"`
}

// Track returns the firmware track the spec is the window of, or "" when it is
// not a firmware window (or the control plane predates the field).
func (t ScheduledTask) Track() string {
	if t.FirmwareTrack == nil {
		return ""
	}
	return *t.FirmwareTrack
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
