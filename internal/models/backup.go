package models

// BackupConfig represents an organization's backup configuration.
//
// Effective scheduling state is derived from Status + Scheduled:
//   - Status == DISABLED                  → config is off
//   - Status == ENABLED && !Scheduled     → config on but no schedule attached (will NOT run)
//   - Status == ENABLED && Scheduled      → running on schedule AttachedSchedule
//
// The legacy Schedule cron field is still returned by some server versions
// and is preserved for JSON round-tripping, but display logic should use
// AttachedSchedule and Scheduled instead.
type BackupConfig struct {
	S3Endpoint       string       `json:"s3_endpoint"`
	S3Bucket         string       `json:"s3_bucket"`
	S3KeyID          string       `json:"s3_key_id"`
	S3Prefix         *string      `json:"s3_prefix"`         // Optional folder path within the bucket
	Schedule         string       `json:"schedule"`          // legacy cron field — use AttachedSchedule for display
	AttachedSchedule *string      `json:"attached_schedule"` // name of the attached Schedule, or null
	Scheduled        bool         `json:"scheduled"`         // true only when ENABLED and a spec is attached
	Status           string       `json:"status"`            // ENABLED, DISABLED
	HasEncryptionKey bool         `json:"has_encryption_key"`
	Organization     string       `json:"organization"`
	CreatedAt        FlexibleTime `json:"created_at"`
	UpdatedAt        FlexibleTime `json:"updated_at"`
}

// BackupConfigTestResponse represents the result of an S3 connection test
type BackupConfigTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DeviceBackupStatus represents the backup status for a single device
type DeviceBackupStatus struct {
	DeviceName               string        `json:"device_name"`
	Enabled                  bool          `json:"enabled"`
	HasEncryptionKeyOverride bool          `json:"has_encryption_key_override"`
	LastBackupAt             *FlexibleTime `json:"last_backup_at"`
	LastBackupStatus         string        `json:"last_backup_status"` // SUCCESS, FAILED, IN_PROGRESS
	LastBackupMessage        string        `json:"last_backup_message"`
	Organization             string        `json:"organization"`
}

// DeviceBackupListResponse represents a paginated list of device backup statuses
type DeviceBackupListResponse struct {
	Items        []DeviceBackupStatus `json:"items"`
	Total        int                  `json:"total"`
	EnabledCount int                  `json:"enabled_count"`
	Page         int                  `json:"page"`
	PerPage      int                  `json:"per_page"`
}

// BackupScheduleAttachResult is the body returned by PUT
// /backup-config/schedule when a schedule name is provided. The server
// registers a BACKUP ScheduledTask spec and returns its descriptor.
type BackupScheduleAttachResult struct {
	Code         string       `json:"code"`
	Kind         string       `json:"kind"`          // always "BACKUP"
	ScheduleName string       `json:"schedule_name"` // name of the attached schedule
	Enabled      bool         `json:"enabled"`
	CreatedBy    string       `json:"created_by"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
}

// BackupScheduleDetachResult is the body returned by PUT
// /backup-config/schedule when the schedule field is null/absent.
type BackupScheduleDetachResult struct {
	Detached         bool   `json:"detached"`
	OrganizationName string `json:"organization_name"`
}

// Backup status constants
const (
	BackupStatusSuccess    = "SUCCESS"
	BackupStatusFailed     = "FAILED"
	BackupStatusInProgress = "IN_PROGRESS"
)
