package models

// Task represents a device task
type Task struct {
	ID           string       `json:"task"`
	Type         string       `json:"type"`
	Status       string       `json:"status"`
	DeviceUUID   string       `json:"device_uuid"`
	DeviceName   string       `json:"device_name,omitempty"`
	Organization string       `json:"organization"`
	Payload      string       `json:"payload,omitempty"`
	Message      string       `json:"message,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at,omitempty"`
	ExpiresAt    FlexibleTime `json:"expires_at,omitempty"`
	ScheduledAt  FlexibleTime `json:"scheduled_at,omitempty"`
	StartedAt    FlexibleTime `json:"started_at,omitempty"`
	CompletedAt  FlexibleTime `json:"completed_at,omitempty"`
}

// TaskListResponse represents a paginated list of tasks
type TaskListResponse struct {
	Items      []Task `json:"items"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"pages"`
}

// TaskStatus constants
const (
	TaskStatusPending    = "PENDING"
	TaskStatusScheduled  = "SCHEDULED"
	TaskStatusInProgress = "IN_PROGRESS"
	TaskStatusCompleted  = "COMPLETED"
	TaskStatusFailed     = "FAILED"
	TaskStatusCancelled  = "CANCELLED"
	TaskStatusExpired    = "EXPIRED"
)

// TaskType constants
const (
	TaskTypeBackup          = "BACKUP"
	TaskTypeConnect         = "CONNECT"
	TaskTypeFirmwareUpgrade = "FIRMWARE_UPGRADE"
	TaskTypePing            = "PING"
	TaskTypePull            = "PULL"
	TaskTypeReboot          = "REBOOT"
	TaskTypeRestart         = "RESTART"
	TaskTypeShutdown        = "SHUTDOWN"
	TaskTypeSync            = "SYNC"
	TaskTypePluginInstall   = "PLUGIN_INSTALL"
)

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
}

// RunResult is the response from POST /organizations/{org}/tasks — the
// `ndcli run` server-side fan-out endpoint. One row per resolved device.
type RunResult struct {
	Type         string        `json:"type"`
	Organization string        `json:"organization"`
	ScheduledAt  string        `json:"scheduled_at,omitempty"`
	Total        int           `json:"total"`
	Tasks        []RunTaskItem `json:"tasks"`
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
}

// ConnectPayload is the parsed payload from a completed connect task
type ConnectPayload struct {
	JTI               string `json:"jti"`
	PathfinderSession string `json:"pathfinder_session"`
}
