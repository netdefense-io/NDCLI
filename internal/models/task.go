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
	TaskTypeBackup   = "BACKUP"
	TaskTypeConnect  = "CONNECT"
	TaskTypePing     = "PING"
	TaskTypePull     = "PULL"
	TaskTypeReboot   = "REBOOT"
	TaskTypeRestart  = "RESTART"
	TaskTypeShutdown = "SHUTDOWN"
	TaskTypeSync     = "SYNC"
)

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
