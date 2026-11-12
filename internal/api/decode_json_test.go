package api

import (
	"strings"
	"testing"
)

// A decode that fails on one field has filled the others, and DecodeJSON
// scrubs them anyway, so a caller that keeps them never holds a raw escape. A
// body that is not valid JSON fills nothing.
func TestDecodeJSONSanitizesAPartlyDecodedTarget(t *testing.T) {
	for body, want := range map[string]string{
		`{"message": "closed\u001b[2J", "outcome": 1}`:  "closed[2J",
		`{"message": "closed\u001b[2J", "outcome": {}}`: "closed[2J",
		`{"message": "closed\u001b[2J", "outcome": "x"`: "",
	} {
		var target struct {
			Message string `json:"message"`
			Outcome string `json:"outcome"`
		}
		if err := DecodeJSON(strings.NewReader(body), &target); err == nil {
			t.Fatalf("body %s: decoded without error", body)
		}
		if target.Message != want {
			t.Errorf("body %s: message %q, want %q", body, target.Message, want)
		}
	}
}
