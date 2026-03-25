package output

import (
	"fmt"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Color definitions for consistent styling
var (
	// Status colors
	ColorSuccess = color.New(color.FgGreen)
	ColorError   = color.New(color.FgRed)
	ColorWarning = color.New(color.FgYellow)
	ColorInfo    = color.New(color.FgBlue)

	// Status indicators
	ColorEnabled    = color.New(color.FgGreen)
	ColorDisabled   = color.New(color.FgRed)
	ColorPending    = color.New(color.FgYellow)
	ColorInProgress = color.New(color.FgBlue)
	ColorCompleted  = color.New(color.FgGreen)
	ColorFailed     = color.New(color.FgRed)
	ColorScheduled  = color.New(color.FgCyan)

	// Role colors
	ColorRoleSU = color.New(color.FgMagenta, color.Bold)
	ColorRoleRW = color.New(color.FgBlue)
	ColorRoleRO = color.New(color.FgCyan)

	// Header/label colors
	ColorHeader = color.New(color.FgWhite, color.Bold)
	ColorLabel  = color.New(color.FgCyan)
	ColorValue  = color.New(color.FgWhite)
	ColorDim    = color.New(color.Faint)
)

// StatusColor returns the appropriate color for a status string
func StatusColor(status string) *color.Color {
	switch status {
	case "ENABLED", "COMPLETED", "SUCCESS", "ACTIVE":
		return ColorEnabled
	case "DISABLED", "FAILED", "ERROR":
		return ColorDisabled
	case "PENDING", "INVITED":
		return ColorPending
	case "IN_PROGRESS", "RUNNING":
		return ColorInProgress
	case "SCHEDULED":
		return ColorScheduled
	default:
		return ColorValue
	}
}

// RoleColor returns the appropriate color for a role string
func RoleColor(role string) *color.Color {
	switch role {
	case "SU":
		return ColorRoleSU
	case "RW":
		return ColorRoleRW
	case "RO":
		return ColorRoleRO
	default:
		return ColorValue
	}
}

// ColoredStatus returns a colored status string
func ColoredStatus(status string) string {
	return StatusColor(status).Sprint(status)
}

// RoleName returns a user-friendly role name
func RoleName(role string) string {
	switch role {
	case "SU":
		return "superuser"
	case "RW":
		return "readwrite"
	case "RO":
		return "readonly"
	default:
		return role
	}
}

// ColoredRole returns a colored role string with user-friendly name
func ColoredRole(role string) string {
	return RoleColor(role).Sprint(RoleName(role))
}

// StatusWithIcon returns a status indicator icon followed by colored status text
func StatusWithIcon(status string) string {
	return StatusIndicator(status) + " " + ColoredStatus(status)
}

// BackupStatusWithIcon returns a backup-specific status indicator with icon
func BackupStatusWithIcon(status string) string {
	switch status {
	case "SUCCESS":
		return ColorSuccess.Sprint("●") + " " + ColorSuccess.Sprint("SUCCESS")
	case "FAILED":
		return ColorError.Sprint("●") + " " + ColorError.Sprint("FAILED")
	case "IN_PROGRESS":
		return ColorInProgress.Sprint("◑") + " " + ColorInProgress.Sprint("IN_PROGRESS")
	default:
		return ColorDim.Sprint("○") + " " + ColorDim.Sprint("-")
	}
}

// EnabledDisabledWithIcon returns enabled/disabled status with icon
func EnabledDisabledWithIcon(enabled bool) string {
	if enabled {
		return ColorEnabled.Sprint("●") + " " + ColorEnabled.Sprint("Enabled")
	}
	return ColorDisabled.Sprint("○") + " " + ColorDisabled.Sprint("Disabled")
}

// EncryptionKeyWithIcon returns encryption key status with icon
func EncryptionKeyWithIcon(hasKey bool) string {
	if hasKey {
		return ColorEnabled.Sprint("●") + " " + ColorEnabled.Sprint("Configured")
	}
	return ColorDim.Sprint("○") + " " + ColorDim.Sprint("Not configured")
}

// KeyOverrideWithIcon returns device key override status with icon
func KeyOverrideWithIcon(hasOverride bool) string {
	if hasOverride {
		return ColorInfo.Sprint("●") + " " + ColorInfo.Sprint("Custom key")
	}
	return ColorDim.Sprint("○") + " " + ColorDim.Sprint("Using org default")
}

// VpnActiveStatus returns a colored active/inactive indicator
func VpnActiveStatus(active bool) string {
	if active {
		return ColorEnabled.Sprint("●") + " " + ColorEnabled.Sprint("active")
	}
	return ColorDisabled.Sprint("○") + " " + ColorDisabled.Sprint("inactive")
}

// VpnPairTypeDisplay returns the pair type with ↔ separator (uncolored)
func VpnPairTypeDisplay(pairType string) string {
	switch pairType {
	case "hub-hub":
		return "hub↔hub"
	case "hub-spoke":
		return "hub↔spoke"
	case "spoke-spoke":
		return "spoke↔spoke"
	default:
		return pairType
	}
}

// VpnPairType returns a colored pair type display with ↔ separator
func VpnPairType(pairType string) string {
	display := VpnPairTypeDisplay(pairType)
	switch pairType {
	case "hub-hub":
		return ColorRoleSU.Sprint(display)
	case "hub-spoke":
		return ColorInfo.Sprint(display)
	case "spoke-spoke":
		return ColorDim.Sprint(display)
	default:
		return display
	}
}

// VpnTypeValue returns the combined type label, e.g. "hub↔spoke, automatic"
func VpnTypeValue(pairType, source string) string {
	pt := VpnPairTypeDisplay(pairType)
	if source == "implicit" {
		return pt + ", automatic"
	}
	return pt + ", manual link"
}

// VpnConnectionNote returns a short note for the NOTES column in connection lists
func VpnConnectionNote(c *models.EffectiveConnection) string {
	if c.Source == "explicit" {
		if !c.Active {
			return "manual link, disabled"
		}
		return "manual link"
	}
	// implicit
	if !c.HasOverride {
		return "automatic"
	}
	if !c.Active {
		return "disabled (override)"
	}
	return "override"
}

// VpnConnectionExplanation returns an explanatory note for describe output
func VpnConnectionExplanation(c *models.EffectiveConnection) string {
	if c.Source == "explicit" {
		return "This connection exists because of a manually created link.\nDeleting the link will disconnect these devices."
	}
	// implicit
	if !c.HasOverride {
		return "This connection exists automatically based on member roles.\nCreate a link override to add PSK or disable this connection."
	}
	if !c.Active {
		return "This automatic connection has been disabled by a link override.\nDelete the override to restore the connection."
	}
	return "This automatic connection has a link override.\nDelete the override to restore default settings."
}

// VpnConnectionLine returns the Connection label value, e.g. "ed-209 (HUB) ↔ murphy01 (SPOKE)"
func VpnConnectionLine(c *models.EffectiveConnection) string {
	return fmt.Sprintf("%s (%s) ↔ %s (%s)", c.DeviceA, c.RoleA, c.DeviceB, c.RoleB)
}
