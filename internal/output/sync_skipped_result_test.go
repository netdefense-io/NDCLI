package output

import (
	"strings"
	"testing"
)

// A skipped item is neither done nor failed (#328): NDAgent leaves it alone
// on purpose and the task completes. Only a skip that needs attention
// (warning) or one a failure caused (a failure status) is marked otherwise.
func TestSymbolForResult_SkippedIsNeutral(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry syncResultEntry
		want  string
	}{
		{"a plain skip", syncResultEntry{Type: "crowdsec_settings", Name: "general", Action: "skipped", Status: "skipped"}, "·"},
		{"a skip that needs attention", syncResultEntry{Type: "crowdsec_apply", Name: "reconfigure", Action: "skipped", Status: "warning"}, "⚠"},
		{"a skip a failure caused", syncResultEntry{Type: "rule_apply", Name: "apply", Action: "skipped", Status: "error"}, "✗"},
	} {
		if got := symbolForResult(tc.entry); got != tc.want {
			t.Errorf("%s: symbolForResult() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The CrowdSec case from the E2E: the plugin is absent, so its settings item
// is skipped. It renders neutral and is not sorted ahead with the failures.
func TestFormatTaskMessage_SkippedItemIsNotAFailure(t *testing.T) {
	raw := `{"message": "Aliases +1; CrowdSec skipped", "results": [` +
		`{"type": "crowdsec_settings", "name": "soc-crowdsec", "action": "skipped", "status": "skipped", "error": "os-crowdsec is not installed"},` +
		`{"type": "alias", "name": "soc-hosts", "action": "created", "status": "success"}` +
		`], "validation_errors": []}`

	got := FormatTaskMessage(raw)
	if strings.Contains(got, "✗") {
		t.Errorf("a skipped item rendered as a failure:\n%s", got)
	}
	alias := strings.Index(got, "+ alias")
	skipped := strings.Index(got, "· crowdsec_settings")
	if alias < 0 || skipped < 0 {
		t.Fatalf("missing lines in:\n%s", got)
	}
	if skipped < alias {
		t.Errorf("the skipped item was sorted ahead like a failure:\n%s", got)
	}
	line, _, _ := strings.Cut(got[skipped:], "\n")
	if !strings.Contains(line, "soc-crowdsec") || !strings.HasSuffix(line, "— os-crowdsec is not installed") {
		t.Errorf("the skipped line does not name the item and say why: %q", line)
	}
}
