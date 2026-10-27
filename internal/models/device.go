package models

import (
	"encoding/json"
	"strings"
)

// Device represents a managed firewall device
type Device struct {
	UUID                string       `json:"uuid"`
	Name                string       `json:"name"`
	Status              string       `json:"status"`
	Organization        string       `json:"organization"`
	OrganizationalUnits []string     `json:"organizational_units,omitempty"`
	Version             string       `json:"version,omitempty"`
	Heartbeat           FlexibleTime `json:"heartbeat,omitempty"`
	// Online is tri-state from NDManager's online-device registry:
	// true = a broker replica owns the device's WS; false = registry up,
	// no replica reports ownership (authoritative offline); nil = lookup
	// failed (Redis down / disabled) — callers must not infer offline.
	Online         *bool         `json:"online,omitempty"`
	AutoSync       bool          `json:"auto_sync"`
	SyncedAt       *FlexibleTime `json:"synced_at,omitempty"`
	SyncedHash     *string       `json:"synced_hash,omitempty"`
	DriftStatus    string        `json:"drift_status,omitempty"`
	DriftCheckedAt *FlexibleTime `json:"drift_checked_at,omitempty"`
	CreatedAt      FlexibleTime  `json:"created_at"`
	UpdatedAt      FlexibleTime  `json:"updated_at"`
	// Facts are agent-reported, optional and informational. Unknown keys
	// are cargo: the map is carried through to JSON output untouched so a
	// newer agent's facts are never dropped by an older CLI. See
	// device_facts.go for the typed accessors and the rules.
	Facts map[string]interface{} `json:"facts,omitempty"`
	// KeyState is "mismatch" when the device's agent presented a key other
	// than the one bound to the device, and absent otherwise (also from an
	// NDManager that predates the field). Only a known mismatch is reported.
	KeyState       string          `json:"key_state,omitempty"`
	KeyStateDetail *KeyStateDetail `json:"key_state_detail,omitempty"`
}

// KeyStateDetail is the evidence behind a key mismatch.
type KeyStateDetail struct {
	BoundKID     string `json:"bound_kid,omitempty"`
	PresentedKID string `json:"presented_kid,omitempty"`
	Source       string `json:"source,omitempty"`
	Since        string `json:"since,omitempty"`
}

// KeyStateMismatch is the only key_state value NDManager reports.
const KeyStateMismatch = "mismatch"

// HasKeyMismatch reports whether the control plane flags the device's key.
func (d *Device) HasKeyMismatch() bool {
	return d.KeyState == KeyStateMismatch
}

// RebindCommand is the command that recovers a device in key mismatch.
func (d *Device) RebindCommand() string {
	return "ndcli device rebind-token " + d.Name
}

// MarshalJSON adds rebind_command to a device in key mismatch so the JSON
// output names the recovery command like the human formats do.
func (d Device) MarshalJSON() ([]byte, error) {
	type plain Device
	if !d.HasKeyMismatch() {
		return json.Marshal(plain(d))
	}
	return json.Marshal(struct {
		plain
		RebindCommand string `json:"rebind_command"`
	}{plain(d), d.RebindCommand()})
}

// IsSynced returns true if the device has a synced hash
func (d *Device) IsSynced() bool {
	return d.SyncedHash != nil && *d.SyncedHash != ""
}

// GetOUsDisplay returns a comma-separated string of OUs for display
func (d *Device) GetOUsDisplay() string {
	if len(d.OrganizationalUnits) == 0 {
		return "-"
	}
	return strings.Join(d.OrganizationalUnits, ", ")
}

// DeviceListResponse represents a paginated list of devices
type DeviceListResponse struct {
	Items   []Device `json:"items"`
	Devices []Device `json:"devices"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
	Pages   int      `json:"pages"`
	Quota   *Quota   `json:"quota,omitempty"`
}

// GetItems returns devices from whichever field is populated
func (r *DeviceListResponse) GetItems() []Device {
	if len(r.Items) > 0 {
		return r.Items
	}
	return r.Devices
}

// DeviceStatus constants
const (
	DeviceStatusPending  = "PENDING"
	DeviceStatusEnabled  = "ENABLED"
	DeviceStatusDisabled = "DISABLED"
)

// DeviceDriftStatus constants
const (
	DeviceDriftStatusInSync      = "IN_SYNC"
	DeviceDriftStatusDrift       = "DRIFT"
	DeviceDriftStatusNeverSynced = "NEVER_SYNCED"
	DeviceDriftStatusUnknown     = "UNKNOWN"
	DeviceDriftStatusError       = "ERROR"
)
