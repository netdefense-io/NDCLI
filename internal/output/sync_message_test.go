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
