package models

// BackupConfig represents an organization's backup configuration
type BackupConfig struct {
	S3Endpoint       string       `json:"s3_endpoint"`
	S3Bucket         string       `json:"s3_bucket"`
	S3KeyID          string       `json:"s3_key_id"`
	S3Prefix         *string      `json:"s3_prefix"` // Optional folder path within the bucket
	Schedule         string       `json:"schedule"`
	Status           string       `json:"status"` // ENABLED, DISABLED
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

// Backup status constants
const (
	BackupStatusSuccess    = "SUCCESS"
	BackupStatusFailed     = "FAILED"
	BackupStatusInProgress = "IN_PROGRESS"
)
