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
