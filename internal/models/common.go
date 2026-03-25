package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FlexibleID handles ID fields that may come as string or number
type FlexibleID string

// UnmarshalJSON implements custom JSON unmarshaling for flexible ID parsing
func (fid *FlexibleID) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*fid = ""
		return nil
	}
	// If it's a number (no quotes in original), convert it
	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*fid = FlexibleID(num.String())
		return nil
	}
	// Otherwise treat as string
	*fid = FlexibleID(s)
	return nil
}

// String returns the ID as a string
func (fid FlexibleID) String() string {
	return string(fid)
}

// MarshalJSON implements JSON marshaling
func (fid FlexibleID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, string(fid))), nil
}

// FlexibleTime handles multiple timestamp formats from the API
type FlexibleTime struct {
	time.Time
}

// Common timestamp formats the API might return
var timeFormats = []string{
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// UnmarshalJSON implements custom JSON unmarshaling for flexible time parsing
func (ft *FlexibleTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		ft.Time = time.Time{}
		return nil
	}

	var parseErr error
	for _, format := range timeFormats {
		t, err := time.Parse(format, s)
		if err == nil {
			ft.Time = t
			return nil
		}
		parseErr = err
	}
	return parseErr
}

// MarshalJSON implements JSON marshaling
func (ft FlexibleTime) MarshalJSON() ([]byte, error) {
	if ft.Time.IsZero() {
		return []byte(`null`), nil
	}
	return []byte(`"` + ft.Time.Format(time.RFC3339) + `"`), nil
}

// Format returns a formatted time string
func (ft FlexibleTime) Format(layout string) string {
	if ft.Time.IsZero() {
		return "-"
	}
	return ft.Time.Format(layout)
}

// IsZero returns true if the time is zero
func (ft FlexibleTime) IsZero() bool {
	return ft.Time.IsZero()
}

// Quota represents resource usage limits for an organization
type Quota struct {
	Limit     int  `json:"limit"`
	Used      int  `json:"used"`
	Available int  `json:"available"`
	Unlimited bool `json:"unlimited"`
}
