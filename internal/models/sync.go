package models

import "strings"

// SyncStatusItem represents a single device's sync status
type SyncStatusItem struct {
	DeviceName   string        `json:"device_name"`
	Organization string        `json:"organization"`
	OUs          []string      `json:"ous,omitempty"`
	AutoSync     bool          `json:"auto_sync"`
	SyncedAt     *FlexibleTime `json:"synced_at,omitempty"`
	SyncedHash   *string       `json:"synced_hash,omitempty"`
	CurrentHash  *string       `json:"current_hash,omitempty"`
	InSync       bool          `json:"in_sync"`
	Error        *string       `json:"error,omitempty"`
}

// IsSynced returns true if the device is in sync
func (s *SyncStatusItem) IsSynced() bool {
	return s.InSync
}

// GetOUsDisplay returns a comma-separated string of OUs for display
func (s *SyncStatusItem) GetOUsDisplay() string {
	if len(s.OUs) == 0 {
		return "-"
	}
	return strings.Join(s.OUs, ", ")
}

// SyncStatusResponse represents the response from GET /api/v1/sync/status
type SyncStatusResponse struct {
	Items   []SyncStatusItem  `json:"items"`
	Total   int               `json:"total"`
	Filters map[string]string `json:"filters,omitempty"`
}

// SyncTaskResult represents a task created by bulk sync
type SyncTaskResult struct {
	Task            string `json:"task"`
	DeviceName      string `json:"device_name"`
	SnippetCount    int    `json:"snippet_count"`
	VpnNetworkCount int    `json:"vpn_network_count"`
	PayloadHash     string `json:"payload_hash"`
}

// SyncErrorConflict represents a variable conflict detail
type SyncErrorConflict struct {
	Variable string `json:"variable"`
	Message  string `json:"message"`
}

// SyncError represents a detailed error for a device sync operation
type SyncError struct {
	DeviceName         string              `json:"device_name"`
	Error              string              `json:"error"`
	Code               string              `json:"code,omitempty"`
	Conflicts          []SyncErrorConflict `json:"conflicts,omitempty"`
	UndefinedVariables []string            `json:"undefined_variables,omitempty"`
}

// SyncApplyResponse represents the response from POST /api/v1/sync
type SyncApplyResponse struct {
	Message         string           `json:"message"`
	DevicesAffected int              `json:"devices_affected"`
	Skipped         int              `json:"skipped"`
	Tasks           []SyncTaskResult `json:"tasks"`
	Errors          []SyncError      `json:"errors,omitempty"`
}
