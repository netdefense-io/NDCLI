package models

// BackupRunTargets is the target selection of POST
// /organizations/{org}/backups/run: the same shape and rules as a run's
// targets (`all` excludes `devices` and `ous`). An empty list is left out of
// the body, which NDManager reads as no names.
type BackupRunTargets struct {
	Devices []string `json:"devices,omitempty"`
	OUs     []string `json:"ous,omitempty"`
	All     bool     `json:"all,omitempty"`
}

// BackupRunRequest is the whole request body of an on-demand backup. It has
// no payload, schedule or time on purpose: the route refuses them, and the
// encryption key is resolved by the control plane, never sent by a client.
type BackupRunRequest struct {
	Targets BackupRunTargets `json:"targets"`
}

// The codes NDManager gives a device an on-demand backup left out. The list
// is open: a later control plane may add codes, and a client shows the
// Reason beside any code it does not know.
const (
	BackupSkipNotEnabled       = "BACKUP_NOT_ENABLED"
	BackupSkipDeviceNotEnabled = "DEVICE_NOT_ENABLED"
	BackupSkipNoEncryptionKey  = "NO_ENCRYPTION_KEY"
	BackupSkipInFlight         = "BACKUP_IN_FLIGHT"
	BackupSkipCreateFailed     = "TASK_CREATE_FAILED"
)

// BackupRunSkipped is a device an on-demand backup left out, and why. Reason
// is the control plane's sentence, to be shown as it is.
type BackupRunSkipped struct {
	DeviceUUID string `json:"device_uuid"`
	DeviceName string `json:"device_name"`
	Code       string `json:"code"`
	Reason     string `json:"reason"`
}

// BackupRunResult is the 201 answer of an on-demand backup: one PENDING BACKUP
// task per device that was backed up (the row shape of a run), and the devices
// that were left out. Total is at least 1; a request that starts nothing is a
// BACKUP_NO_ELIGIBLE_DEVICES refusal, never an empty result.
type BackupRunResult struct {
	Type         string             `json:"type"`
	Organization string             `json:"organization"`
	Total        int                `json:"total"`
	Tasks        []RunTaskItem      `json:"tasks"`
	Skipped      []BackupRunSkipped `json:"skipped"`
}
