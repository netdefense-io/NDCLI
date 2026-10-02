package output

import (
	"strings"
	"testing"
)

// TestSymbolForResult_TrustItems guards the TRUST_CA/TRUST_CERT result
// items, using the action/status/code strings NDAgent emits: a "warning"
// on any trust_* type is informational (⚠, never ✗), a successful
// trust_reload reads as a reload whatever its action verb, and every
// "blocked" or "error" item still reads as a failure.
func TestSymbolForResult_TrustItems(t *testing.T) {
	cases := []struct {
		name  string
		entry syncResultEntry
		want  string
	}{
		{"ca created", syncResultEntry{Type: "trust_ca", Action: "created", Status: "success"}, "+"},
		{"cert updated", syncResultEntry{Type: "trust_cert", Action: "updated", Status: "success"}, "~"},
		{"cert unchanged", syncResultEntry{Type: "trust_cert", Action: "unchanged", Status: "success"}, "="},
		{"cert deleted", syncResultEntry{Type: "trust_cert", Action: "deleted", Status: "success"}, "-"},
		{"in use", syncResultEntry{Type: "trust_ca", Action: "retained", Status: "blocked", Code: "TRUST_IN_USE"}, "✗"},
		{"name collision", syncResultEntry{Type: "trust_cert", Action: "blocked", Status: "blocked", Code: "NAME_COLLISION_UNMANAGED"}, "✗"},
		{"ca rekeyed", syncResultEntry{Type: "trust_ca", Action: "blocked", Status: "blocked", Code: "TRUST_CA_REKEYED"}, "✗"},
		{"ca dn conflict", syncResultEntry{Type: "trust_ca", Action: "blocked", Status: "blocked", Code: "TRUST_CA_DN_CONFLICT"}, "✗"},
		{"opnsense too old", syncResultEntry{Type: "trust_cert", Action: "blocked", Status: "blocked", Code: "TRUST_OPNSENSE_TOO_OLD"}, "✗"},
		{"content unsupported", syncResultEntry{Type: "trust_cert", Action: "unsupported", Status: "blocked", Code: "TRUST_CONTENT_UNSUPPORTED"}, "✗"},
		{"device policy", syncResultEntry{Type: "trust_cert", Action: "rejected", Status: "blocked", Code: "TRUST_REJECTED_DANGEROUS"}, "✗"},
		{"import failed", syncResultEntry{Type: "trust_cert", Action: "updated", Status: "error", Code: "TRUST_IMPORT_FAILED"}, "✗"},
		{"reloaded", syncResultEntry{Type: "trust_reload", Action: "openvpn configure", Status: "success"}, "↻"},
		{"reload scheduled", syncResultEntry{Type: "trust_reload", Action: "scheduled", Status: "success", Code: "TRUST_RELOAD_SCHEDULED"}, "↻"},
		{"reload failed", syncResultEntry{Type: "trust_reload", Action: "ipsec reload", Status: "warning", Code: "TRUST_RELOAD_FAILED"}, "⚠"},
		{"unknown consumer", syncResultEntry{Type: "trust_reload", Action: "none", Status: "warning", Code: "TRUST_CONSUMER_UNKNOWN"}, "⚠"},
		// The informational-warning rule stays scoped to the two families.
		{"other family warning", syncResultEntry{Type: "alias", Action: "created", Status: "warning"}, "✗"},
	}
	for _, tc := range cases {
		if got := symbolForResult(tc.entry); got != tc.want {
			t.Errorf("%s: symbolForResult() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFormatTaskMessage_TrustRenewalWithReloads is the end-to-end path for
// an in-place renewal: the renewed certificate, the services reloaded for
// it (each naming the reload that ran), the web GUI restart scheduled
// after the result, a failed reload and a consumer with no known reload.
// Warnings must not render, or sort, as failures.
func TestFormatTaskMessage_TrustRenewalWithReloads(t *testing.T) {
	raw := `{"message": "Trust ~1; reloads 1, scheduled 1, warnings 2", "results": [` +
		`{"type": "trust_reload", "uuid": "", "name": "lab-portal", "action": "none", "status": "warning",` +
		` "code": "TRUST_CONSUMER_UNKNOWN", "error": "bound, no known reload: restart to apply"},` +
		`{"type": "trust_cert", "uuid": "221f3268-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "name": "fw-wildcard", "action": "updated", "status": "success"},` +
		`{"type": "trust_reload", "uuid": "0f6e1a52-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "name": "corp-vpn", "action": "openvpn configure", "status": "success"},` +
		`{"type": "trust_reload", "uuid": "5c1d0e8f-cccc-4ccc-8ccc-cccccccccccc", "name": "HQ tunnel", "action": "ipsec reload", "status": "warning",` +
		` "code": "TRUST_RELOAD_FAILED", "error": "reload failed"},` +
		`{"type": "trust_reload", "uuid": "", "name": "webgui", "action": "scheduled", "status": "success", "code": "TRUST_RELOAD_SCHEDULED"}` +
		`], "validation_errors": []}`
	got := FormatTaskMessage(raw)

	for _, want := range []string{
		"~ trust_cert    fw-wildcard",
		"↻ trust_reload  corp-vpn (openvpn configure)",
		"↻ trust_reload  webgui (scheduled)",
		"code: TRUST_RELOAD_SCHEDULED",
		"⚠ trust_reload  HQ tunnel (ipsec reload) — reload failed",
		"code: TRUST_RELOAD_FAILED",
		"⚠ trust_reload  lab-portal — bound, no known reload: restart to apply",
		"code: TRUST_CONSUMER_UNKNOWN",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n--- got ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "(none)") {
		t.Errorf("a consumer with no known reload must not show the placeholder verb, got:\n%s", got)
	}
	if strings.Contains(got, "✗") {
		t.Errorf("no item here is a failure, got:\n%s", got)
	}
	// Failures sort first; a warning keeps its type/name position.
	if strings.Index(got, "trust_cert") > strings.Index(got, "lab-portal") {
		t.Errorf("a warning row sorted ahead of trust_cert as if it were a failure:\n%s", got)
	}
}
