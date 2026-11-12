package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	for _, tc := range []struct {
		in       string
		min, max time.Duration
	}{
		{"", 0, 0},
		{"5", 5 * time.Second, 5 * time.Second},
		{" 7 ", 7 * time.Second, 7 * time.Second},
		{"0", 0, 0},
		{"-3", 0, 0},
		{"soon", 0, 0},
		{"99999999999", maxRetryAfter, maxRetryAfter},
		{"99999999999999999999", maxRetryAfter, maxRetryAfter},
		{"-99999999999999999999", 0, 0},
		{future, 85 * time.Second, 90 * time.Second},
		{past, 0, 0},
	} {
		if got := parseRetryAfter(tc.in); got < tc.min || got > tc.max {
			t.Errorf("parseRetryAfter(%q) = %v, want between %v and %v", tc.in, got, tc.min, tc.max)
		}
	}
}

func TestParseErrorKeepsRetryAfter(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": {"12"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"slow down"}`)),
	}
	if got := ParseError(resp).RetryAfter; got != 12*time.Second {
		t.Errorf("RetryAfter = %v, want 12s", got)
	}
}
