package output

import (
	"fmt"
	"time"
)

// RelativeTime returns a human-readable relative time string
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	// Convert both times to display timezone for consistent comparison
	t = inDisplayZone(t)
	now := inDisplayZone(time.Now())
	diff := now.Sub(t)

	// Future times (shouldn't happen often, but handle it)
	if diff < 0 {
		return t.Format("2006-01-02 15:04")
	}

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < 2*time.Minute:
		return "1 min ago"
	case diff < time.Hour:
		return fmt.Sprintf("%d min ago", int(diff.Minutes()))
	case diff < 2*time.Hour:
		return "1 hour ago"
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(diff.Hours()))
	case diff < 48*time.Hour:
		return "yesterday"
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(diff.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// RelativeTimeShort returns a shorter relative time string (for tables)
func RelativeTimeShort(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	// Convert both times to display timezone for consistent comparison
	t = inDisplayZone(t)
	now := inDisplayZone(time.Now())
	diff := now.Sub(t)

	if diff < 0 {
		return t.Format("01-02 15:04")
	}

	switch {
	case diff < time.Minute:
		return "now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(diff.Hours()/24))
	default:
		return t.Format("01-02")
	}
}

// FormatTimestamp formats a time for display (with seconds)
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return inDisplayZone(t).Format("2006-01-02 15:04:05")
}

// FormatTimestampShort formats a time for display (without seconds)
func FormatTimestampShort(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return inDisplayZone(t).Format("2006-01-02 15:04")
}

// FormatDate formats a date for display (no time)
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return inDisplayZone(t).Format("2006-01-02")
}
