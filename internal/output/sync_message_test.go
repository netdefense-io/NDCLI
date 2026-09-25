package output

import (
	"strings"
	"testing"
)

func TestFormatTaskMessage_PlainString(t *testing.T) {
	got := FormatTaskMessage("plain text")
	if got != "plain text" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestFormatTaskMessage_NotJSONFallsThrough(t *testing.T) {
	got := FormatTaskMessage("Aliases +2; Rules +2")
	if got != "Aliases +2; Rules +2" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestFormatTaskMessage_SyncEnvelope(t *testing.T) {
	raw := `{"message": "Aliases +2; Rules +2; Zabbix settings ~1", "results": [` +
		`{"type": "alias", "uuid": "221f3268-aaa", "name": "red_pki_server_addr", "action": "created", "status": "success"},` +
		`{"type": "alias", "uuid": "221f3268-bbb", "name": "red_pki_server_addr_ports", "action": "created", "status": "success"},` +
		`{"type": "rule", "uuid": "221f3268-ccc", "name": "pki-monitoria-in", "action": "created", "status": "success"},` +
		`{"type": "rule", "uuid": "221f3268-ddd", "name": "pki-monitoria-out", "action": "created", "status": "success"},` +
		`{"type": "zabbix_settings", "uuid": "", "name": "bt003.pki.zone", "action": "updated", "status": "success"}` +
		`], "validation_errors": []}`
	got := FormatTaskMessage(raw)

	wantContains := []string{
		"Aliases +2; Rules +2; Zabbix settings ~1",
		"Changes:",
		"+ alias",
		"red_pki_server_addr",
		"+ rule",
		"pki-monitoria-in",
		"~ zabbix_settings",
		"bt003.pki.zone",
	}
	for _, w := range wantContains {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\n--- got ---\n%s", w, got)
		}
	}
	if strings.Contains(got, "validation_errors") {
		t.Errorf("empty validation_errors should not render a section\n--- got ---\n%s", got)
	}
}

func TestFormatTaskMessage_ErrorRowPrefixed(t *testing.T) {
	raw := `{"message": "Aliases +1 (1 errors)", "results": [` +
		`{"type": "alias", "name": "good", "action": "created", "status": "success"},` +
		`{"type": "rule", "name": "bad", "action": "created", "status": "error", "error": "boom"}` +
		`], "validation_errors": []}`
	got := FormatTaskMessage(raw)
	if !strings.Contains(got, "✗ rule") {
		t.Errorf("expected error row to use ✗ marker\n%s", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("expected error text in row\n%s", got)
	}
}

func TestFormatTaskMessage_ValidationErrors(t *testing.T) {
	raw := `{"message": "No changes applied", "results": [],` +
		`"validation_errors": [{"type": "alias", "name": "x", "message": "still in use"}]}`
	got := FormatTaskMessage(raw)
	if !strings.Contains(got, "Validation errors:") {
		t.Errorf("expected validation section\n%s", got)
	}
	if !strings.Contains(got, "still in use") {
		t.Errorf("expected error text\n%s", got)
	}
}

func TestFormatTaskMessage_JSONWithoutSyncFields(t *testing.T) {
	// A JSON object that happens to have a `message` key but no results
	// or validation_errors should unwrap to just the message text.
	got := FormatTaskMessage(`{"message": "just a string"}`)
	if got != "just a string" {
		t.Errorf("got %q, want %q", got, "just a string")
	}
}

// A software-policy result must be visible in `task describe`. It was
// not: symbolForResult only knew the config-sync verbs and returned ""
// for everything else, and writeResultLines dropped every entry with an
// empty symbol. An installed package rendered as nothing at all.
func TestSymbolForResult_SoftwareAndRepositoryActions(t *testing.T) {
	cases := map[string]string{
		"INSTALLED":       "+",
		"REMOVED":         "-",
		"ALREADY_PRESENT": "=",
		"NOT_FOUND":       "✗",
		"REPO_CONFIGURED": "+",
		"REPO_UNCHANGED":  "=",
		"REPO_REMOVED":    "-",
		"REPO_CONFLICT":   "✗",
		"SHADOWED":        "✗",
		"create":          "+",
		"updated":         "~",
	}
	for action, want := range cases {
		got := symbolForResult(syncResultEntry{Action: action, Status: "success"})
		if got != want {
			t.Errorf("symbolForResult(%q) = %q, want %q", action, got, want)
		}
	}

	// Unknown actions stay visible — "nothing happened" and "this build
	// does not know that word" must not look the same.
	if got := symbolForResult(syncResultEntry{Action: "SOME_FUTURE_ACTION", Status: "success"}); got == "" {
		t.Error("an unknown action must still render")
	}

	// A failed status wins over whatever the action says.
	if got := symbolForResult(syncResultEntry{Action: "INSTALLED", Status: "error"}); got != "✗" {
		t.Errorf("failed status: got %q, want ✗", got)
	}
}

// TestFormatTaskMessage_SanitizesInnerEnvelope guards the second JSON
// decode inside FormatTaskMessage. The outer Task went through
// api.DecodeJSON/sanitize.Struct already, but that pass only sees
// Task.Message as one opaque string — a \u001b escape inside the
// envelope's own JSON is still literal escape text at that point, and
// only becomes a live control byte when this function decodes the
// envelope a second time. Every additive AUTH field (before/after/
// available/risks/code) is device-controlled, so each gets its own case.
func TestFormatTaskMessage_SanitizesInnerEnvelope(t *testing.T) {
	raw := `{"message": "AUTH +0; 1 errors", "results": [` +
		`{"type": "auth_facility", "name": "evil\u001b]0;pwn\u0007", "action": "refused", "status": "blocked",` +
		` "code": "AUTH_ORDER_UNRESOLVED\u001b[2J", "error": "boom\u001b[31m",` +
		` "before": ["evil\u001b]0;pwn\u0007"], "after": ["evil\u001b]0;pwn\u0007"],` +
		` "available": ["x\u001b[2J"], "risks": ["cleartext\u001b[2J"]}` +
		`], "validation_errors": []}`
	got := FormatTaskMessage(raw)

	for _, control := range []string{"\x1b", "\a"} {
		if strings.Contains(got, control) {
			t.Errorf("rendered output still contains a raw control byte %q:\n%q", control, got)
		}
	}
}

func TestWriteResultLines_KeepsSoftwareResults(t *testing.T) {
	var b strings.Builder
	writeResultLines(&b, []syncResultEntry{
		{Type: "SOFTWARE", Name: "btop", Action: "INSTALLED", Status: "success"},
		{Type: "REPOSITORY", Name: "mimugmail", Action: "REPO_UNCHANGED", Status: "success"},
	})
	out := b.String()
	if !strings.Contains(out, "btop") || !strings.Contains(out, "mimugmail") {
		t.Errorf("software results dropped from the rendered output:\n%s", out)
	}
}
