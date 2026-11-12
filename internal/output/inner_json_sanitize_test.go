package output

import (
	"encoding/json"
	"strings"
	"testing"
)

// A JSON document held in a string is decoded a second time by these
// helpers, which is where a \u001b in it becomes a live byte. The outer
// decode's sanitize pass never saw that byte.
func TestInnerJSONIsSanitized(t *testing.T) {
	d := parseFirmwareUpgradeData(`{"resolved_mode": "minor\u001b[2J", "from_version": "26.1\u001b]0;x\u0007", "to_version": "26.7\u009b31m", "applied": true}`)
	if d == nil {
		t.Fatal("firmware upgrade data not parsed")
	}
	lines := strings.Join(formatFirmwareDataLines(d), "\n")
	if strings.ContainsAny(lines, "\x1b\x07\u009b") || !strings.Contains(lines, "minor[2J") {
		t.Errorf("firmware result lines %q carry a control character, or lost the mode", lines)
	}

	summary := scheduledTaskRequestSummary(json.RawMessage(`{"type": "PING\u001b[2J", "targets": {"devices": ["fw\u001b[31m"], "ous": ["edge\u0007"]}, "organization": "acme\u001b[0m", "device": "fw\u009b1", "ou": "o\u001bP"}`))
	if strings.ContainsAny(summary, "\x1b\x07\u009b") || !strings.Contains(summary, "type=PING[2J") {
		t.Errorf("scheduled task summary %q carries a control character, or lost its text", summary)
	}
}

// The request summary's fallbacks (JSON that is not an object, or an object
// with none of the summarised keys) show the bytes as sent, where DEL and the
// C1 controls may stand unescaped.
func TestRequestSummaryFallbacksAreSanitized(t *testing.T) {
	for _, raw := range []string{"[\"\u009b2J\"]", "{\"x\": \"\u009b2J\u007f\"}", "\"\u009d0;title\""} {
		if got := scheduledTaskRequestSummary(json.RawMessage(raw)); strings.ContainsAny(got, "\u009b\u009d\x7f") || !strings.Contains(got, "2J") && !strings.Contains(got, "0;title") {
			t.Errorf("summary of %q = %q", raw, got)
		}
	}
}
