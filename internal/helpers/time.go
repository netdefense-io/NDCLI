package helpers

import (
	"fmt"
	"math"
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

// AtInputKind is what an --at-style input is, by its shape alone.
type AtInputKind int

const (
	// AtEmpty is a blank input.
	AtEmpty AtInputKind = iota
	// AtRelative is an offset from now (30m, 2h), resolved in UTC.
	AtRelative
	// AtExplicitZone ends in its own zone designator, Z or ±HH:MM.
	AtExplicitZone
	// AtBare is a timestamp with no zone of its own: ambiguous without a
	// location, so the MCP surface refuses it unless the caller names one.
	AtBare
)

// ClassifyAtInput is the one place an --at-style input is classified;
// IsBareTimestamp, HasExplicitZone and IsRelativeOffset read it, so the three
// cannot drift apart.
func ClassifyAtInput(input string) AtInputKind {
	in := strings.TrimSpace(input)
	switch {
	case in == "":
		return AtEmpty
	case relativeFuturePattern.MatchString(strings.ToLower(in)):
		return AtRelative
	case tzSuffix.MatchString(in):
		return AtExplicitZone
	}
	return AtBare
}

// IsBareTimestamp reports whether input is a timestamp carrying no timezone of
// its own (AtBare).
func IsBareTimestamp(input string) bool { return ClassifyAtInput(input) == AtBare }

// HasExplicitZone reports whether input carries a zone designator of its own
// (AtExplicitZone). Callers use it to name the zone from the input rather than
// from whatever location the parse happened to attach.
func HasExplicitZone(input string) bool { return ClassifyAtInput(input) == AtExplicitZone }

// IsRelativeOffset reports whether input is a relative offset (AtRelative),
// which is anchored to "now" and therefore always resolves in UTC.
func IsRelativeOffset(input string) bool { return ClassifyAtInput(input) == AtRelative }

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
		d, err := relativeOffset(input, m[1], m[2], maxDuration)
		if err != nil {
			return time.Time{}, err
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

	return time.Time{}, fmt.Errorf("could not parse %q (accepts relative like 30m/2h/1d/1w, or 2030-05-12, 2030-05-12 03:00, 2030-05-12T03:00:00Z)", input)
}

// relativeFilterPattern is a list filter's relative bound: 45s, 30m, 2h, 7d,
// 2w back from now, the same with a leading -, or with a leading + that far
// ahead.
var relativeFilterPattern = regexp.MustCompile(`^([+-]?)(\d+)([smhdw])$`)

// maxFilterOffset bounds ResolveTimeFilter's relative bound well inside
// time.Duration's range.
const maxFilterOffset = 100 * 365 * 24 * time.Hour

// maxDuration is the longest offset time.Duration holds; past it a count
// wraps around.
const maxDuration = time.Duration(math.MaxInt64)

var relativeUnits = map[string]time.Duration{
	"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour,
}

// relativeOffset is count units as a duration, refused past limit so a huge
// count cannot overflow time.Duration and wrap.
func relativeOffset(input, count, unit string, limit time.Duration) (time.Duration, error) {
	n, err := strconv.Atoi(count)
	u := relativeUnits[unit]
	if err != nil || u == 0 || time.Duration(n) > limit/u {
		return 0, fmt.Errorf("%q is too far from now", input)
	}
	return time.Duration(n) * u, nil
}

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
		offset, err := relativeOffset(input, m[2], m[3], maxFilterOffset)
		if err != nil {
			return time.Time{}, err
		}
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

// relativeAgoPattern is ParseTimeFilter's relative form: 30m, 2h, 7d, 2w ago.
var relativeAgoPattern = regexp.MustCompile(`^(\d+)([mhdw])$`)

func parseTimeFilterAt(input string, now time.Time) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}

	if m := relativeAgoPattern.FindStringSubmatch(strings.ToLower(input)); m != nil {
		d, err := relativeOffset(input, m[1], m[2], maxDuration)
		if err != nil {
			return "", err
		}
		return now.Add(-d).UTC().Format(time.RFC3339), nil
	}

	return input, nil
}
