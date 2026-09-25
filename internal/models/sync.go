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

// AuthIssue is one AUTH_SERVER/AUTH_ORDER build-time failure reported by
// NDManager for a device (AUTH_BUILD_INVALID). Every field is an
// identifier — snippet, template, server, group and facility names —
// never a value, so an issue can never carry a secret (like a resolved
// LDAP bind password) back to the caller.
//
// Snippet and Template are always a list, never a bare string, even for a
// single-snippet issue — NDManager's auth_build._as_list normalizes every
// issue to this shape server-side. Server, Group and Facility stay single
// strings.
type AuthIssue struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Snippet  []string `json:"snippet,omitempty"`
	Template []string `json:"template,omitempty"`
	Server   string   `json:"server,omitempty"`
	Group    string   `json:"group,omitempty"`
	Facility string   `json:"facility,omitempty"`
}

// SyncError represents a detailed error for a device sync operation
type SyncError struct {
	DeviceName                  string              `json:"device_name"`
	Error                       string              `json:"error"`
	Code                        string              `json:"code,omitempty"`
	Conflicts                   []SyncErrorConflict `json:"conflicts,omitempty"`
	UndefinedVariables          []string            `json:"undefined_variables,omitempty"`
	UndefinedVariablesBySnippet map[string][]string `json:"undefined_variables_by_snippet,omitempty"`
	// AuthIssues carries every AUTH_SERVER/AUTH_ORDER build-time issue for
	// this device, present when Code is AUTH_BUILD_INVALID.
	AuthIssues []AuthIssue `json:"auth_issues,omitempty"`
}

// SyncApplyResponse represents the response from POST /api/v1/sync
type SyncApplyResponse struct {
	Message         string           `json:"message"`
	DevicesAffected int              `json:"devices_affected"`
	Skipped         int              `json:"skipped"`
	Tasks           []SyncTaskResult `json:"tasks"`
	Errors          []SyncError      `json:"errors,omitempty"`
}
