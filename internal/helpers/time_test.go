package helpers

import (
	"strings"
	"testing"
	"time"
)

func TestParseFutureTime_Relative(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		in     string
		approx time.Duration // expected offset from now
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"30M", 30 * time.Minute}, // case-insensitive
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseFutureTimeUTC(c.in, loc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			delta := time.Until(got) - c.approx
			if delta < -2*time.Second || delta > 2*time.Second {
				t.Fatalf("offset %v not within ±2s of expected %v", time.Until(got), c.approx)
			}
		})
	}
}

func TestParseFutureTime_ExplicitTZ(t *testing.T) {
	got, err := parseFutureTimeUTC("2026-05-12T03:00:00Z", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-05-12T03:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}

	// Offset gets normalized to UTC
	got, err = parseFutureTimeUTC("2026-05-12T03:00:00-03:00", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ = time.Parse(time.RFC3339, "2026-05-12T06:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("offset conversion: got %v want %v", got, want)
	}
}

func TestParseFutureTime_BareWithConfiguredTZ(t *testing.T) {
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("timezone db missing: %v", err)
	}
	// 2026-05-12 03:00 in São Paulo (UTC-3) → 06:00 UTC
	got, err := parseFutureTimeUTC("2026-05-12 03:00", saoPaulo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-05-12T06:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("bare-in-tz: got %v want %v", got, want)
	}
}

func TestParseFutureTime_DateOnly(t *testing.T) {
	utc := time.UTC
	got, err := parseFutureTimeUTC("2026-05-12", utc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-05-12T00:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("date-only: got %v want %v", got, want)
	}
}

func TestParseFutureTime_Errors(t *testing.T) {
	cases := []string{
		"",
		"not a time",
		"30x",                  // unknown unit
		"2026-13-99",           // invalid date
		"2026-05-12T03:00:00Q", // tz-suffix-like but invalid
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := parseFutureTimeUTC(in, time.UTC); err == nil {
				t.Fatalf("expected error for %q", in)
			}
		})
	}
}

func TestParseFutureTime_ErrorMessageMentionsExamples(t *testing.T) {
	_, err := parseFutureTimeUTC("garbage", time.UTC)
	if err == nil {
		t.Fatal("expected error")
	}
	// Make sure the message hints at accepted forms (the error is the
	// primary discoverability surface for the flag).
	if !strings.Contains(err.Error(), "30m") {
		t.Fatalf("error %q should mention example formats", err)
	}
}

// parseFutureTimeUTC is the UTC-forcing shape these tests were written
// against. Production goes through service.ResolveScheduledAt, which keeps the
// parsed zone for display and converts separately.
func parseFutureTimeUTC(input string, loc *time.Location) (time.Time, error) {
	t, err := ParseFutureTimeZoned(input, loc)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// TestIsBareTimestamp pins the three-way classification the MCP scheduling
// surface keys off: only an input carrying no zone of its own needs one
// supplied.
func TestIsBareTimestamp(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"30m", false},
		{"2h", false},
		{"1W", false},
		{"2026-05-12T03:00:00Z", false},
		{"2026-05-12T03:00:00-03:00", false},
		{"2026-05-12T03:00:00+05:30", false},
		{"2026-05-12 03:00", true},
		{"2026-05-12T03:00:00", true},
		{"2026-05-12", true},
		{"  2026-05-12 03:00  ", true},
		{"banana", true}, // no zone present; parseability is a separate question
		{"", false},
	} {
		if got := IsBareTimestamp(c.in); got != c.want {
			t.Errorf("IsBareTimestamp(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestResolveTimeFilter(t *testing.T) {
	now := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"2h", "2026-10-18T13:00:00Z"},
		{"-2h", "2026-10-18T13:00:00Z"},
		{"+2h", "2026-10-18T17:00:00Z"},
		{"+30M", "2026-10-18T15:30:00Z"},
		{"45s", "2026-10-18T14:59:15Z"},
		{"7d", "2026-10-11T15:00:00Z"},
		{"+1w", "2026-10-25T15:00:00Z"},
		{"2026-10-18T09:00:00-03:00", "2026-10-18T12:00:00Z"},
		{"2026-10-18T09:00:00Z", "2026-10-18T09:00:00Z"},
		// A bare time is UTC, the way NDManager reads a bound without an offset.
		{"2026-10-18 09:00", "2026-10-18T09:00:00Z"},
		{"2026-10-18T09:00:00", "2026-10-18T09:00:00Z"},
		{"2026-10-18", "2026-10-18T00:00:00Z"},
		{" 2h ", "2026-10-18T13:00:00Z"},
	} {
		got, err := ResolveTimeFilter(tc.input, now)
		if err != nil {
			t.Errorf("ResolveTimeFilter(%q): %v", tc.input, err)
			continue
		}
		if got.Location() != time.UTC || got.Format(time.RFC3339) != tc.want {
			t.Errorf("ResolveTimeFilter(%q) = %s (%s), want %s", tc.input, got.Format(time.RFC3339), got.Location(), tc.want)
		}
	}

	// The host's zone plays no part: the same input, the same instant.
	local := time.Local
	time.Local = time.FixedZone("UTC+3", 3*3600)
	t.Cleanup(func() { time.Local = local })
	if got, _ := ResolveTimeFilter("2h", now.In(time.Local)); got.Format(time.RFC3339) != "2026-10-18T13:00:00Z" {
		t.Errorf("2h on a UTC+3 host = %s", got.Format(time.RFC3339))
	}
	if got, _ := ResolveTimeFilter("2026-10-18 09:00", now); got.Format(time.RFC3339) != "2026-10-18T09:00:00Z" {
		t.Errorf("a bare time on a UTC+3 host = %s", got.Format(time.RFC3339))
	}

	for _, bad := range []string{"", "tomorrow", "2h30m", "+-2h", "2026-13-01", "2026-10-18T09:00:00Q", "+999999999w"} {
		if got, err := ResolveTimeFilter(bad, now); err == nil {
			t.Errorf("ResolveTimeFilter(%q) = %v, want an error", bad, got)
		}
	}
}

// TestParseTimeFilterSendsARelativeBoundInUTC: NDManager reads a bound without
// an offset as UTC, so a relative bound is computed in UTC and carries its Z.
// The clock is fixed, read in zones west and east of UTC and in UTC itself:
// the bound is the same instant from each of them.
func TestParseTimeFilterSendsARelativeBoundInUTC(t *testing.T) {
	instant := time.Date(2026, 10, 19, 4, 4, 18, 0, time.UTC)
	for _, zone := range []*time.Location{time.FixedZone("UTC-7", -7*3600), time.FixedZone("UTC+3", 3*3600), time.UTC} {
		now := instant.In(zone)
		for input, want := range map[string]string{
			"30m":  "2026-10-19T03:34:18Z",
			"30M":  "2026-10-19T03:34:18Z",
			"1h":   "2026-10-19T03:04:18Z",
			" 1h ": "2026-10-19T03:04:18Z",
			"7d":   "2026-10-12T04:04:18Z",
			"2w":   "2026-10-05T04:04:18Z",
		} {
			if got, err := parseTimeFilterAt(input, now); err != nil || got != want {
				t.Errorf("parseTimeFilterAt(%q) at %s = %q, %v; want %q", input, now.Format(time.RFC3339), got, err, want)
			}
		}
	}
}

// TestParseTimeFilterSendsAnAbsoluteTimeAsTyped: anything that is not a
// relative bound goes out unchanged, for NDManager to read and validate.
func TestParseTimeFilterSendsAnAbsoluteTimeAsTyped(t *testing.T) {
	now := time.Date(2026, 10, 18, 21, 4, 18, 0, time.FixedZone("UTC-7", -7*3600))
	for _, input := range []string{
		"2026-10-18", "2026-10-18T09:00:00", "2026-10-18 09:00:00", "2026-10-18T09:00:00Z",
		"2026-10-18T09:00:00-03:00", "2026-10-18T09:00:00+03:00", "tomorrow", "+2h",
	} {
		if got, err := parseTimeFilterAt(input, now); err != nil || got != input {
			t.Errorf("parseTimeFilterAt(%q) = %q, %v; want it as typed", input, got, err)
		}
	}
	if got, err := parseTimeFilterAt("  ", now); err != nil || got != "" {
		t.Errorf("a blank bound = %q, %v; want none", got, err)
	}
}

// TestParseTimeFilterIgnoresTheHostZone runs the exported function, on the
// real clock, on a host whose zone is UTC-7.
func TestParseTimeFilterIgnoresTheHostZone(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC-7", -7*3600)
	t.Cleanup(func() { time.Local = local })

	earliest := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	got, err := ParseTimeFilter("30m")
	latest := time.Now().UTC().Add(-30 * time.Minute)
	if err != nil || !strings.HasSuffix(got, "Z") {
		t.Fatalf("ParseTimeFilter(30m) = %q, %v; want a UTC instant with Z", got, err)
	}
	at, err := time.Parse(time.RFC3339, got)
	if err != nil || at.Before(earliest) || at.After(latest) {
		t.Errorf("ParseTimeFilter(30m) = %s, want between %s and %s", got, earliest.Format(time.RFC3339), latest.Format(time.RFC3339))
	}
}
