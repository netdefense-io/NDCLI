package pathfinder

import (
	"testing"
	"time"
)

// A relay Retry-After too large for a duration is capped, as the API client
// caps it, instead of wrapping to a short wait.
func TestParseRetryAfterIsCapped(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"5":                             5 * time.Second,
		"86400":                         24 * time.Hour,
		"86401":                         24 * time.Hour,
		"9223372036854775807":           24 * time.Hour,
		"18446744074":                   24 * time.Hour,
		"99999999999999999999":          24 * time.Hour,
		"-5":                            0,
		"-99999999999999999999":         0,
		"Fri, 31 Dec 9999 23:59:59 GMT": 24 * time.Hour,
	} {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}
