package output

import (
	"fmt"
	"time"
)

var displayLocation *time.Location = time.Local

// SetTimezone sets the timezone for displaying timestamps.
// Accepts IANA names ("America/New_York"), "UTC", or "Local".
func SetTimezone(tzName string) error {
	if tzName == "" || tzName == "Local" {
		displayLocation = time.Local
		return nil
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	displayLocation = loc
	return nil
}

// ApplyConfiguredTimezone sets the display timezone from a config value,
// falling back to the system local zone when the value is invalid. An empty
// value means Local. The invalid-value error is returned rather than rendered:
// each front-end words its own warning (stderr in the CLI, the server log in
// netdefense-mcp), but the fallback must not differ between them — a surface
// that silently kept a different zone would resolve the same `--at`/`at` input
// to a different instant (issue #217).
func ApplyConfiguredTimezone(tzName string) error {
	if err := SetTimezone(tzName); err != nil {
		_ = SetTimezone("Local")
		return err
	}
	return nil
}

// GetTimezone returns the current display timezone name.
func GetTimezone() string {
	if displayLocation == time.Local {
		return "Local"
	}
	return displayLocation.String()
}

// Location returns the active display timezone as a *time.Location, for use
// when parsing bare-tz timestamps the user typed in (e.g. `--at`). Returns
// time.Local when the config is unset or set to "Local".
func Location() *time.Location {
	return displayLocation
}

// inDisplayZone converts a time to the display timezone.
func inDisplayZone(t time.Time) time.Time {
	return t.In(displayLocation)
}
