package models

import (
	"encoding/json"
	"testing"
)

// A spec's request is scrubbed on decode, escaped controls included, and is
// otherwise kept as sent: key order, spacing, number text and HTML characters.
// Two keys that scrub to the same one both stay.
func TestDisplayJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"type": "PING\u001b[2J", "targets": {"devices": ["fw\u009b1"]}}`: `{"type": "PING[2J", "targets": {"devices": ["fw1"]}}`,
		`{"k\u0007": 1}`:                             `{"k": 1}`,
		`{"a\u001b": 1, "a": 2}`:                     `{"a": 1, "a": 2}`,
		`{"count": 12345678901234567890, "f": 1.50}`: `{"count": 12345678901234567890, "f": 1.50}`,
		`{"cmd": "a < b && c > d"}`:                  `{"cmd": "a < b && c > d"}`,
		`null`:                                       `null`,
		`"\u001b]0;title\u0007"`:                     `"]0;title"`,
	} {
		var d DisplayJSON
		if err := json.Unmarshal([]byte(in), &d); err != nil || string(d) != want {
			t.Errorf("%s: decoded %s (%v), want %s", in, d, err, want)
		}
	}
	var empty DisplayJSON
	if out, _ := json.Marshal(struct {
		Request DisplayJSON `json:"request"`
	}{empty}); string(out) != `{"request":null}` {
		t.Errorf("absent request encoded %s, want null", out)
	}
	var bad struct {
		Request DisplayJSON `json:"request"`
	}
	if err := json.Unmarshal([]byte(`{"request": {"a": }}`), &bad); err == nil {
		t.Error("invalid JSON decoded")
	}
}
