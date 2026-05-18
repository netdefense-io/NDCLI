package helpers

import (
	"bytes"
	"encoding/json"
	"strings"
)

// PrettyJSON returns s indented with two spaces. If s is not valid JSON it
// is returned unchanged so the caller can fall back to raw display.
// json.Indent preserves the original key order — it operates on raw bytes
// and does not round-trip through a map.
func PrettyJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// MinifyJSON returns s with all insignificant whitespace removed. If s is
// not valid JSON it is returned unchanged. Like PrettyJSON, this is a
// whitespace-only transform that preserves key order.
func MinifyJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return s
	}
	return buf.String()
}
