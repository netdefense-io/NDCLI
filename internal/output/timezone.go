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

// GetTimezone returns the current display timezone name.
func GetTimezone() string {
	if displayLocation == time.Local {
		return "Local"
	}
	return displayLocation.String()
}

// inDisplayZone converts a time to the display timezone.
func inDisplayZone(t time.Time) time.Time {
	return t.In(displayLocation)
}
