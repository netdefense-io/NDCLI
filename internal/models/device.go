package models

import "strings"

// Device represents a managed firewall device
type Device struct {
	UUID                string        `json:"uuid"`
	Name                string        `json:"name"`
	Status              string        `json:"status"`
	Organization        string        `json:"organization"`
	OrganizationalUnits []string      `json:"organizational_units,omitempty"`
	Version             string        `json:"version,omitempty"`
	Heartbeat           FlexibleTime  `json:"heartbeat,omitempty"`
	AutoSync            bool          `json:"auto_sync"`
	SyncedAt            *FlexibleTime `json:"synced_at,omitempty"`
	SyncedHash          *string       `json:"synced_hash,omitempty"`
	CreatedAt           FlexibleTime  `json:"created_at"`
	UpdatedAt           FlexibleTime  `json:"updated_at"`
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
