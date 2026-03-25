package helpers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseTimeFilter parses a time filter string which can be either:
// - Relative: 30m, 2h, 7d, 2w (minutes, hours, days, weeks ago)
// - ISO 8601: 2025-12-05, 2025-12-05T14:30:00, 2025-12-05T14:30:00Z
// Returns the time in ISO 8601 format suitable for API calls.
func ParseTimeFilter(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}

	// Try to parse as relative time (e.g., 30m, 2h, 7d, 2w)
	relativePattern := regexp.MustCompile(`^(\d+)([mhdw])$`)
	if matches := relativePattern.FindStringSubmatch(strings.ToLower(input)); matches != nil {
		value, _ := strconv.Atoi(matches[1])
		unit := matches[2]

		var duration time.Duration
		switch unit {
		case "m":
			duration = time.Duration(value) * time.Minute
		case "h":
			duration = time.Duration(value) * time.Hour
		case "d":
			duration = time.Duration(value) * 24 * time.Hour
		case "w":
			duration = time.Duration(value) * 7 * 24 * time.Hour
		default:
			return "", fmt.Errorf("unknown time unit: %s", unit)
		}

		result := time.Now().Add(-duration)
		return result.Format("2006-01-02T15:04:05"), nil
	}

	// If not relative, assume it's already ISO 8601 format - pass through
	// The API will validate the format
	return input, nil
}
