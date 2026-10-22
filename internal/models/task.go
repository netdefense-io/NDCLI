package models

import (
	"regexp"
	"strings"
	"time"
)

// RunIDPattern is NDDataModels' TASK_RUN_ID_PATTERN, the shape of a run's code.
var RunIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

// Task represents a device task.
//
// NDManager 1.99 added where a task came from and how its device stands
// (device_deleted through last_response_at), and 1.100 who asked for it, from
// which client, in which run, and when it was sent and finished (requested_by
// through completed_at). An older control plane omits them and they read as
// unset. The nullable ones are pointers without omitempty, so -f json and a
// re-encode keep the server's null. Every time a current NDManager sends is UTC
// with its Z; the newer ones are time.Time, the older ones keep FlexibleTime.
type Task struct {
	ID         string `json:"task"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	DeviceUUID string `json:"device_uuid"`
	// DeviceName is a removed device's name as it was given, with
	// DeviceDeleted set.
	DeviceName    string       `json:"device_name,omitempty"`
	DeviceDeleted bool         `json:"device_deleted"`
	Organization  string       `json:"organization"`
	Payload       string       `json:"payload,omitempty"`
	Message       string       `json:"message,omitempty"`
	ErrorMessage  string       `json:"error_message,omitempty"`
	CreatedAt     FlexibleTime `json:"created_at"`
	UpdatedAt     FlexibleTime `json:"updated_at,omitempty"`
	ExpiresAt     FlexibleTime `json:"expires_at,omitempty"`
	ScheduledAt   FlexibleTime `json:"scheduled_at,omitempty"`
	// Origin is one of TaskOrigins.
	Origin string `json:"origin,omitempty"`
	// Schedule is the schedule spec that created the task, or nil.
	Schedule *TaskSchedule `json:"schedule"`
	// DeviceOnline and DeviceLastSeenAt are set on a PENDING or SCHEDULED task
	// only: whether its device is connected now (nil also when that cannot be
	// told) and its last heartbeat, which lags the device by about two minutes.
	DeviceOnline     *bool      `json:"device_online"`
	DeviceLastSeenAt *time.Time `json:"device_last_seen_at"`
	// LastResponseAt is when the device's signed answer was recorded.
	LastResponseAt *time.Time `json:"last_response_at"`
	// RequestedBy is the account that asked for a MANUAL task. nil for a
	// schedule's or a firmware track's task, and for a task recorded before
	// NDManager kept it.
	RequestedBy *TaskRequester `json:"requested_by"`
	// Client is how the request reached NetDefense: WEB, CLI, MCP or API (a
	// personal access token). MANUAL tasks only.
	Client *string `json:"client"`
	// RunID is shared by the tasks of one create request or one fire of a
	// schedule spec.
	RunID *string `json:"run_id"`
	// DispatchedAt is when the task was sent to the device, and CompletedAt
	// when it reached COMPLETED, FAILED, EXPIRED or CANCELED.
	DispatchedAt *time.Time `json:"dispatched_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// TaskSchedule is the schedule spec that created a task.
type TaskSchedule struct {
	Name     string  `json:"name"`
	SpecCode *string `json:"spec_code"`
	// Kind is the spec's kind: RUN, SYNC, BACKUP or FIRMWARE.
	Kind string `json:"kind"`
	// NextRunAt is null while the schedule or the spec is disabled.
	NextRunAt *time.Time `json:"next_run_at"`
	// FirmwareTrack names the track whose window created the task (fast,
	// regular or stable), and is null for every other kind.
	FirmwareTrack *string `json:"firmware_track"`
}

// TaskRequester is the account that asked for a task, read when the task is.
// Both fields are null when the account no longer exists.
type TaskRequester struct {
	Email *string `json:"email"`
	Name  *string `json:"name"`
}

// Duration is how long the task took, from its dispatch to its completion. ok
// is false unless both are known and in that order.
func (t *Task) Duration() (d time.Duration, ok bool) {
	if t.DispatchedAt == nil || t.CompletedAt == nil {
		return 0, false
	}
	d = t.CompletedAt.Sub(*t.DispatchedAt)
	if d < 0 {
		return 0, false
	}
	return d, true
}

// DurationSeconds is Duration in whole seconds, as -f json and the MCP task
// tools give it, or nil when it is not known.
func (t *Task) DurationSeconds() *int64 {
	d, ok := t.Duration()
	if !ok {
		return nil
	}
	s := int64(d.Round(time.Second) / time.Second)
	return &s
}

// TaskListResponse represents a paginated list of tasks. Filters echoes the
// filters the server applied; a filter it does not know is ignored and has no
// key there.
type TaskListResponse struct {
	Items      []Task         `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	TotalPages int            `json:"pages"`
	Filters    map[string]any `json:"filters,omitempty"`
}

// Where a task came from (NDDataModels' TASK_ORIGIN_*): a person's request, a
// schedule spec's fire, or a firmware track's step.
const (
	TaskOriginManual   = "MANUAL"
	TaskOriginSchedule = "SCHEDULE"
	TaskOriginFirmware = "FIRMWARE"
)

// TaskOrigins is NDDataModels' TASK_ORIGINS in its order, the values of the
// task list's origin filter. Read-only.
var TaskOrigins = []string{TaskOriginManual, TaskOriginSchedule, TaskOriginFirmware}

// TaskStatus constants. NDManager writes CANCELED (one L); CANCELLED is only
// accepted as an input filter.
const (
	TaskStatusPending    = "PENDING"
	TaskStatusScheduled  = "SCHEDULED"
	TaskStatusInProgress = "IN_PROGRESS"
	TaskStatusCompleted  = "COMPLETED"
	TaskStatusFailed     = "FAILED"
	TaskStatusCanceled   = "CANCELED"
	TaskStatusExpired    = "EXPIRED"
)

// TaskType constants
const (
	TaskTypeBackup          = "BACKUP"
	TaskTypeConnect         = "CONNECT"
	TaskTypeFirmwareTrack   = "FIRMWARE_TRACK"
	TaskTypeFirmwareUpgrade = "FIRMWARE_UPGRADE"
	TaskTypePing            = "PING"
	TaskTypePull            = "PULL"
	TaskTypeReboot          = "REBOOT"
	TaskTypeRestart         = "RESTART"
	TaskTypeShutdown        = "SHUTDOWN"
	TaskTypeSync            = "SYNC"
	TaskTypePluginInstall   = "PLUGIN_INSTALL"
)

// TaskTypes is every task type NDManager can return and accept as a list
// filter (NDDataModels' TaskType enum). It is the single source for `task list
// --type` help, completion and validation and for the ndcli.task.list enum.
// Treat it as read-only: it is shared by reference with all of them. It is the
// listable set, not the creatable one (see service.validRunTypes):
// FIRMWARE_TRACK is listed because the firmware tracks create those tasks, and
// no client can.
var TaskTypes = []string{
	TaskTypeBackup, TaskTypeConnect, TaskTypeFirmwareTrack, TaskTypeFirmwareUpgrade, TaskTypePing,
	TaskTypePluginInstall, TaskTypePull, TaskTypeReboot, TaskTypeRestart, TaskTypeShutdown, TaskTypeSync,
}

// TaskStatuses is NDDataModels' TaskStatus enum in lifecycle order, the single
// source for `task list --status` the way TaskTypes is for --type. Read-only.
var TaskStatuses = []string{
	TaskStatusPending, TaskStatusScheduled, TaskStatusInProgress, TaskStatusCompleted,
	TaskStatusFailed, TaskStatusCanceled, TaskStatusExpired,
}

// TaskStatusAliases maps a spelling NDManager accepts as a status filter onto
// the value it writes. Read-only, like TaskTypes.
var TaskStatusAliases = map[string]string{"CANCELLED": TaskStatusCanceled}

// TaskValuesHelp renders a value list for flag help and error text: a
// comma-separated list in declaration order.
func TaskValuesHelp(values []string) string {
	return strings.Join(values, ", ")
}

// FirmwareUpgradeData is the result data block reported by NDAgent for
// FIRMWARE_UPGRADE tasks. The agent stores it serialised as JSON inside
// Task.Message. Field names mirror the NDDataModels / NDAgent wire contract
// exactly — do not rename without updating those modules too.
type FirmwareUpgradeData struct {
	// ResolvedMode is the mode the agent actually executed ("minor" or "major").
	ResolvedMode string `json:"resolved_mode,omitempty"`
	// FromVersion is the OPNsense product version read at execution time.
	FromVersion string `json:"from_version,omitempty"`
	// ToVersion is the version reached after a successful upgrade, or the
	// available target if the run was a dry-run.
	ToVersion string `json:"to_version,omitempty"`
	// RebootPerformed is true when the agent triggered a reboot as part of
	// the upgrade (minor reboot=true or major).
	RebootPerformed bool `json:"reboot_performed"`
	// Applied is true when at least one package or component was installed.
	Applied bool `json:"applied"`
	// NoUpdate is true when the firmware check found nothing to apply (no-op
	// COMPLETED). Distinct from Applied=false because dry_run also has
	// Applied=false.
	NoUpdate bool `json:"no_update"`
	// PackagesApplied is the count of non-base/non-kernel packages applied
	// when reboot=false (split-and-apply path).
	PackagesApplied int `json:"packages_applied"`
	// MixedState is true when the split-and-apply path was used and base/kernel
	// updates are still pending (device is running new packages against the old
	// base/kernel — normal for the no-reboot path, but worth tracking).
	MixedState bool `json:"mixed_state"`
	// DryRun is true when the upgrade ran in preview/simulation mode — packages
	// were resolved but nothing was installed (Applied is also false).
	DryRun bool `json:"dry_run"`
	// NeedsReboot is true when a reboot is required to complete a pending
	// upgrade (e.g. after the no-reboot/split-apply path leaves base/kernel
	// updates deferred).
	NeedsReboot bool `json:"needs_reboot"`
}

// RunResult is the response from POST /organizations/{org}/tasks — the
// `ndcli run` server-side fan-out endpoint. One row per resolved device.
type RunResult struct {
	Type         string `json:"type"`
	Organization string `json:"organization"`
	ScheduledAt  string `json:"scheduled_at,omitempty"`
	// RunID is the code of the run this call created, the run_id of each of
	// its tasks. A control plane that predates it does not send it.
	RunID string        `json:"run_id,omitempty"`
	Total int           `json:"total"`
	Tasks []RunTaskItem `json:"tasks"`
}

// RunTaskItem is a single device's task row inside a RunResult.
type RunTaskItem struct {
	Task       string `json:"task"`
	DeviceUUID string `json:"device_uuid"`
	DeviceName string `json:"device_name"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expires_at"`
}

// ConnectInitResponse is returned by POST /devices/{device}/connect
type ConnectInitResponse struct {
	Task    string `json:"task"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ConnectStatusResponse is returned by GET /tasks/{task}/connect-status
type ConnectStatusResponse struct {
	Task    string `json:"task"`
	Status  string `json:"status"`
	Payload string `json:"payload,omitempty"`
	Message string `json:"message,omitempty"`
	// ReadOnly is the authoritative read-only flag NDManager derives from the
	// caller's role on the organization. A pointer so an older NDManager that
	// omits the field (nil) is distinguishable from an explicit false: nil means
	// "unknown, proceed as before"; *true means terminal/exec is not allowed.
	ReadOnly *bool `json:"read_only,omitempty"`
}

// ConnectPayload is the parsed payload from a completed connect task
type ConnectPayload struct {
	JTI               string `json:"jti"`
	PathfinderSession string `json:"pathfinder_session"`
}
