package helpers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// relativeFuturePattern accepts `45s`, `30m`, `2h`, `3d`, `1w` for `--at`-style
// inputs. Case-insensitive. Seconds (s) added for short-range scheduling tests;
// the rest match ParseTimeFilter's accepted units.
var relativeFuturePattern = regexp.MustCompile(`^(\d+)([smhdw])$`)

// bareTimestampLayouts are tried in order against any input that wasn't matched
// by the relative pattern and that lacks an explicit timezone. They're parsed
// in the caller-supplied *time.Location (typically NDCLI's configured display
// timezone, so "2026-05-12 03:00" means 3 AM in the user's working tz).
var bareTimestampLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02", // date-only = midnight in the configured tz
}

// tzSuffix matches the trailing tz designator on an RFC3339-style string:
// "Z", "+HH:MM", or "-HH:MM". When present we use time.Parse (which respects
// the embedded zone); when absent we use ParseInLocation with the user's
// configured timezone.
var tzSuffix = regexp.MustCompile(`(?:Z|[+\-]\d{2}:\d{2})$`)

// ParseFutureTime parses an --at-style input into an absolute UTC time.
// Accepted forms:
//
//   - Relative offset: `45s`, `30m`, `2h`, `3d`, `1w`  → now + offset (UTC)
//   - With explicit timezone: `2026-05-12T03:00:00Z`,
//     `2026-05-12T03:00:00-03:00`                       → that exact instant
//   - Bare timestamp (no tz): `2026-05-12 03:00`,
//     `2026-05-12T03:00:00`, `2026-05-12`               → interpreted in `loc`
//
// `loc` is the timezone to apply to bare timestamps — typically NDCLI's
// configured display timezone via `output.Location()`. Date-only inputs land
// at midnight in `loc`.
//
// Returns the parsed instant as UTC (always). Past-time checks are the
// caller's responsibility.
func ParseFutureTime(input string, loc *time.Location) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if loc == nil {
		loc = time.Local
	}

	// 1. Relative offset
	if m := relativeFuturePattern.FindStringSubmatch(strings.ToLower(input)); m != nil {
		n, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch m[2] {
		case "s":
			d = time.Duration(n) * time.Second
		case "m":
			d = time.Duration(n) * time.Minute
		case "h":
			d = time.Duration(n) * time.Hour
		case "d":
			d = time.Duration(n) * 24 * time.Hour
		case "w":
			d = time.Duration(n) * 7 * 24 * time.Hour
		}
		return time.Now().Add(d).UTC(), nil
	}

	// 2. Explicit-tz forms — try RFC3339 first (it covers Z, ±HH:MM, and
	//    nanoseconds). Bypass the bare-layout loop when a tz is clearly present
	//    so a typo like "2026-05-12T03:00:00Q" fails with the RFC3339 message
	//    instead of being silently reinterpreted as "bare local".
	if tzSuffix.MatchString(input) {
		if t, err := time.Parse(time.RFC3339Nano, input); err == nil {
			return t.UTC(), nil
		}
		if t, err := time.Parse(time.RFC3339, input); err == nil {
			return t.UTC(), nil
		}
		return time.Time{}, fmt.Errorf("invalid timestamp %q: tz suffix present but value is not valid RFC3339", input)
	}

	// 3. Bare layouts — interpreted in the configured timezone.
	for _, layout := range bareTimestampLayouts {
		if t, err := time.ParseInLocation(layout, input, loc); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse %q (accepts relative like 30m/2h/1d/1w, or 2026-05-12, 2026-05-12 03:00, 2026-05-12T03:00:00Z)", input)
}

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
