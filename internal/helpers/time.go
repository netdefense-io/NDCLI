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

// IsBareTimestamp reports whether input is a timestamp carrying no timezone
// information of its own — neither a relative offset (which is anchored to
// "now") nor an explicit `Z`/`±HH:MM` suffix. Such an input is ambiguous
// without a location: the MCP surface refuses it unless the caller names one.
func IsBareTimestamp(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	if relativeFuturePattern.MatchString(strings.ToLower(input)) {
		return false
	}
	return !tzSuffix.MatchString(input)
}

// HasExplicitZone reports whether input carries a zone designator of its own —
// a trailing `Z` or `±HH:MM`. Callers use it to name the zone from the input
// rather than from whatever location the parse happened to attach.
func HasExplicitZone(input string) bool {
	return tzSuffix.MatchString(strings.TrimSpace(input))
}

// IsRelativeOffset reports whether input is a relative offset (`30m`, `2h`),
// which is anchored to "now" and therefore always resolves in UTC.
func IsRelativeOffset(input string) bool {
	return relativeFuturePattern.MatchString(strings.ToLower(strings.TrimSpace(input)))
}

// ParseFutureTimeZoned parses an --at-style input into an absolute instant.
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
// The returned instant keeps the zone it was parsed in — the explicit offset
// for RFC3339 inputs, `loc` for bare timestamps, UTC for relative offsets — so
// callers can render it the way the user expressed it. Converting to UTC and
// checking for a past time are service.ResolveScheduledAt's job; call that
// rather than this directly.
func ParseFutureTimeZoned(input string, loc *time.Location) (time.Time, error) {
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
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339, input); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid timestamp %q: tz suffix present but value is not valid RFC3339", input)
	}

	// 3. Bare layouts — interpreted in the configured timezone.
	for _, layout := range bareTimestampLayouts {
		if t, err := time.ParseInLocation(layout, input, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse %q (accepts relative like 30m/2h/1d/1w, or 2026-05-12, 2026-05-12 03:00, 2026-05-12T03:00:00Z)", input)
}

// relativeFilterPattern is a list filter's relative bound: 45s, 30m, 2h, 7d,
// 2w back from now, the same with a leading -, or with a leading + that far
// ahead.
var relativeFilterPattern = regexp.MustCompile(`^([+-]?)(\d+)([smhdw])$`)

// maxFilterOffset bounds a relative bound well inside time.Duration's range.
const maxFilterOffset = 100 * 365 * 24 * time.Hour

// ResolveTimeFilter turns a list filter's time bound into an instant:
//
//   - 45s, 30m, 2h, 7d, 2w: that long before now (-2h says the same);
//   - +30m, +2h, +7d: that long after now;
//   - an RFC 3339 time with Z or an offset: that instant;
//   - a bare 2026-10-18, 2026-10-18 03:00 or 2026-10-18T03:00:00: that time in
//     UTC, the way NDManager reads a bound without an offset.
//
// The instant is returned in UTC.
func ResolveTimeFilter(input string, now time.Time) (time.Time, error) {
	input = strings.TrimSpace(input)
	if m := relativeFilterPattern.FindStringSubmatch(strings.ToLower(input)); m != nil {
		n, err := strconv.Atoi(m[2])
		unit := map[string]time.Duration{
			"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour,
		}[m[3]]
		if err != nil || time.Duration(n) > maxFilterOffset/unit {
			return time.Time{}, fmt.Errorf("%q is too far from now", input)
		}
		offset := time.Duration(n) * unit
		if m[1] != "+" {
			offset = -offset
		}
		return now.Add(offset).UTC(), nil
	}
	// Only the absolute forms reach this parser, which does not refuse a past instant.
	if at, err := ParseFutureTimeZoned(input, time.UTC); err == nil {
		return at.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("could not parse %q (accepts 30m, 2h, 7d or 2w ago, +30m or +2h from now, "+
		"2026-10-18, 2026-10-18 03:00 (UTC) or 2026-10-18T03:00:00-03:00)", input)
}

// ParseTimeFilter parses a list filter's time bound (created_after and the
// like):
//
//   - Relative: 30m, 2h, 7d, 2w, that long ago. Sent as a UTC instant with Z:
//     NDManager reads a time without an offset as UTC, so the host's own wall
//     clock would move the bound by the host's UTC offset.
//   - Anything else, such as 2025-12-05, 2025-12-05T14:30:00 or
//     2025-12-05T14:30:00Z, is sent as typed, for NDManager to parse and
//     validate.
func ParseTimeFilter(input string) (string, error) {
	return parseTimeFilterAt(input, time.Now())
}

func parseTimeFilterAt(input string, now time.Time) (string, error) {
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

		return now.Add(-duration).UTC().Format(time.RFC3339), nil
	}

	return input, nil
}
