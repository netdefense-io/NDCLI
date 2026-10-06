package output

import (
	"strings"
	"testing"
)

// TestSymbolForResult_RuleAndAliasItems uses the type/action/status/code
// strings NDAgent emits for RULE and ALIAS snippets: refusals and device
// errors read as failures, placement warnings as warnings, a moved local rule
// as a change (the agent's summary counts it as "Local rules ~N"), and a rule
// or alias the device already holds as unchanged.
func TestSymbolForResult_RuleAndAliasItems(t *testing.T) {
	cases := []struct {
		name  string
		entry syncResultEntry
		want  string
	}{
		{"rule created", syncResultEntry{Type: "rule", Action: "created", Status: "success"}, "+"},
		{"rule updated", syncResultEntry{Type: "rule", Action: "updated", Status: "success"}, "~"},
		{"rule deleted", syncResultEntry{Type: "rule", Action: "deleted", Status: "success"}, "-"},
		{"rule unchanged", syncResultEntry{Type: "rule", Action: "unchanged", Status: "success"}, "="},
		{"unknown field", syncResultEntry{Type: "rule", Action: "blocked", Status: "blocked", Code: "RULE_FIELD_UNKNOWN"}, "✗"},
		{"invalid value", syncResultEntry{Type: "rule", Action: "blocked", Status: "blocked", Code: "RULE_VALUE_INVALID"}, "✗"},
		{"missing interface", syncResultEntry{Type: "rule", Action: "blocked", Status: "blocked", Code: "INTERFACE_NOT_FOUND"}, "✗"},
		{"no sequence room", syncResultEntry{Type: "rule", Action: "blocked", Status: "blocked", Code: "RULE_NO_SEQUENCE_ROOM"}, "✗"},
		{"rejected by device", syncResultEntry{Type: "rule", Action: "updated", Status: "error", Code: "RULE_REJECTED_BY_DEVICE"}, "✗"},
		{"rule model unreadable", syncResultEntry{Type: "rule_discovery", Name: "rule_model", Action: "discover", Status: "error", Code: "RULE_MODEL_UNAVAILABLE"}, "✗"},
		{"local renumbered", syncResultEntry{Type: "rule_local", Action: "renumbered", Status: "success", Code: "RULE_LOCAL_RENUMBERED"}, "~"},
		{"local renumber failed", syncResultEntry{Type: "rule_local", Action: "renumber", Status: "error", Code: "RULE_LOCAL_RENUMBER_FAILED"}, "✗"},
		{"local renumber unverified", syncResultEntry{Type: "rule_local", Action: "renumber", Status: "error", Code: "RULE_LOCAL_RENUMBER_UNVERIFIED"}, "✗"},
		{"local vanished", syncResultEntry{Type: "rule_local", Action: "renumber", Status: "warning", Code: "RULE_LOCAL_VANISHED"}, "⚠"},
		{"prepend after local", syncResultEntry{Type: "rule_placement", Action: "warning", Status: "warning", Code: "RULE_PREPEND_AFTER_LOCAL"}, "⚠"},
		{"append before local", syncResultEntry{Type: "rule_placement", Action: "warning", Status: "warning", Code: "RULE_APPEND_BEFORE_LOCAL"}, "⚠"},
		{"section order", syncResultEntry{Type: "rule_placement", Action: "warning", Status: "warning", Code: "RULE_SECTION_ORDER"}, "⚠"},
		{"section mismatch", syncResultEntry{Type: "rule_placement", Action: "warning", Status: "warning", Code: "RULE_SECTION_MISMATCH"}, "⚠"},
		{"placement unavailable", syncResultEntry{Type: "rule_placement", Name: "placement", Action: "warning", Status: "warning", Code: "RULE_PLACEMENT_UNAVAILABLE"}, "⚠"},
		{"placement unverified", syncResultEntry{Type: "rule_placement", Action: "verify", Status: "error", Code: "RULE_PLACEMENT_UNVERIFIED"}, "✗"},
		{"apply withheld", syncResultEntry{Type: "rule_apply", Name: "apply", Action: "skipped", Status: "error", Code: "RULE_APPLY_WITHHELD"}, "✗"},
		{"alias created", syncResultEntry{Type: "alias", Action: "created", Status: "success"}, "+"},
		{"alias unchanged", syncResultEntry{Type: "alias", Action: "unchanged", Status: "success"}, "="},
		{"alias unknown field", syncResultEntry{Type: "alias", Action: "blocked", Status: "blocked", Code: "ALIAS_FIELD_UNKNOWN"}, "✗"},
		{"alias invalid value", syncResultEntry{Type: "alias", Action: "blocked", Status: "blocked", Code: "ALIAS_VALUE_INVALID"}, "✗"},
		{"alias rejected", syncResultEntry{Type: "alias", Action: "created", Status: "error", Code: "ALIAS_REJECTED_BY_DEVICE"}, "✗"},
		{"alias model unreadable", syncResultEntry{Type: "alias_discovery", Name: "alias_model", Action: "discover", Status: "error", Code: "ALIAS_MODEL_UNAVAILABLE"}, "✗"},
	}
	for _, tc := range cases {
		if got := symbolForResult(tc.entry); got != tc.want {
			t.Errorf("%s: symbolForResult() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestSymbolForResult_WarningIsNeverAFailure: every NDAgent family that
// reports a "warning" item still completes the task, so none of them may read
// as a failure. A status this build does not know still does.
func TestSymbolForResult_WarningIsNeverAFailure(t *testing.T) {
	for _, entry := range []syncResultEntry{
		{Type: "auth_local_server", Action: "warning", Status: "warning", Code: "ORDER_NAMES_LOCAL_SERVER"},
		{Type: "trust_reload", Action: "ipsec reload", Status: "warning", Code: "TRUST_RELOAD_FAILED"},
		{Type: "rule_placement", Action: "warning", Status: "warning", Code: "RULE_PREPEND_AFTER_LOCAL"},
		{Type: "rule_local", Action: "renumber", Status: "warning", Code: "RULE_LOCAL_VANISHED"},
		{Type: "software", Name: "pkg_update", Action: "warning", Status: "warning"},
		{Type: "user", Name: "svc-backup", Action: "remove_group_memberships", Status: "warning"},
	} {
		if got := symbolForResult(entry); got != "⚠" {
			t.Errorf("%s %s warning: symbolForResult() = %q, want ⚠", entry.Type, entry.Name, got)
		}
	}
	if got := symbolForResult(syncResultEntry{Type: "rule", Action: "created", Status: "some_future_status"}); got != "✗" {
		t.Errorf("an unknown status must still read as a failure, got %q", got)
	}
}

// TestFormatTaskMessage_RulePlacementResult is the end-to-end path for a SYNC
// that places a PREPEND rule ahead of a fresh OPNsense 26.7 box's default LAN
// rule: the summary line NDAgent builds, the moved local rule with both
// sequences, a placement warning, and a refused rule, failures first.
func TestFormatTaskMessage_RulePlacementResult(t *testing.T) {
	raw := `{"message": "Rules +1; Local rules ~1 (1 errors)", "results": [` +
		`{"type": "rule", "uuid": "221f3268-0000-4000-8000-0000000000a1", "name": "Allow-HTTPS-from-SOC", "action": "created", "status": "success"},` +
		`{"type": "rule", "uuid": "221f3268-0000-4000-8000-0000000000a2", "name": "Block-telnet", "action": "unchanged", "status": "success"},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e01", "name": "Default allow LAN to any rule", "action": "renumbered", "status": "success",` +
		` "code": "RULE_LOCAL_RENUMBERED", "before": ["1"], "after": ["2"]},` +
		`{"type": "rule_placement", "uuid": "221f3268-0000-4000-8000-0000000000a1", "name": "Allow-HTTPS-from-SOC", "action": "warning", "status": "warning",` +
		` "code": "RULE_PREPEND_AFTER_LOCAL", "error": "Rule \"Allow-HTTPS-from-SOC\" (snippet \"allow-https\") is PREPEND, but OPNsense evaluates the local rule \"Floating DNS\" first: floating rules come before interface-group rules, and both before single-interface rules"},` +
		`{"type": "rule", "uuid": "221f3268-0000-4000-8000-0000000000a3", "name": "logtest", "action": "blocked", "status": "blocked",` +
		` "code": "RULE_FIELD_UNKNOWN", "error": "Rule \"logtest\" (snippet \"rule-logtest\"): field \"lgo\" (did you mean \"log\"?) is not in this device's rule model (OPNsense 26.7)"}` +
		`], "validation_errors": []}`

	want := strings.Join([]string{
		`Rules +1; Local rules ~1 (1 errors)`,
		``,
		`Changes:`,
		`  ✗ rule            logtest — Rule "logtest" (snippet "rule-logtest"): field "lgo" (did you mean "log"?) is not in this device's rule model (OPNsense 26.7)`,
		`      code: RULE_FIELD_UNKNOWN`,
		`  + rule            Allow-HTTPS-from-SOC`,
		`  = rule            Block-telnet`,
		`  ~ rule_local      Default allow LAN to any rule`,
		`      before: [1]  after: [2]`,
		`      code: RULE_LOCAL_RENUMBERED`,
		`  ⚠ rule_placement  Allow-HTTPS-from-SOC — Rule "Allow-HTTPS-from-SOC" (snippet "allow-https") is PREPEND, but OPNsense evaluates the local rule "Floating DNS" first: floating rules come before interface-group rules, and both before single-interface rules`,
		`      code: RULE_PREPEND_AFTER_LOCAL`,
	}, "\n")
	if got := FormatTaskMessage(raw); got != want {
		t.Errorf("rendered SYNC result:\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatTaskMessage_RuleApplyWithheld: when a renumber may have left a
// re-created local rule behind, NDAgent applies no firewall rule at all and
// says which rule to check. That outcome is a failure, shown with its message.
func TestFormatTaskMessage_RuleApplyWithheld(t *testing.T) {
	raw := `{"message": "Rules ~1; Local rules ~1 (2 errors)", "results": [` +
		`{"type": "rule", "uuid": "221f3268-0000-4000-8000-0000000000a1", "name": "Allow-HTTPS-from-SOC", "action": "updated", "status": "success"},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e01", "name": "Default allow LAN to any rule", "action": "renumbered", "status": "success",` +
		` "code": "RULE_LOCAL_RENUMBERED", "before": ["1"], "after": ["2"]},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e03", "name": "Allow LAN NTP", "action": "renumber", "status": "error",` +
		` "code": "RULE_LOCAL_RENUMBER_UNVERIFIED", "error": "Local rule \"Allow LAN NTP\" was deleted on the device while NetDefense moved it, and the move re-created it with OPNsense's defaults (a pass rule); deleting that failed, so delete it on the device: HTTP 500"},` +
		`{"type": "rule_apply", "uuid": "", "name": "apply", "action": "skipped", "status": "error",` +
		` "code": "RULE_APPLY_WITHHELD", "error": "Firewall rules were not applied: \"Allow LAN NTP\" may have been re-created as a pass rule by a renumber and could not be confirmed removed; check the device, then SYNC again"}` +
		`], "validation_errors": []}`

	want := strings.Join([]string{
		`Rules ~1; Local rules ~1 (2 errors)`,
		``,
		`Changes:`,
		`  ✗ rule_apply  apply — Firewall rules were not applied: "Allow LAN NTP" may have been re-created as a pass rule by a renumber and could not be confirmed removed; check the device, then SYNC again`,
		`      code: RULE_APPLY_WITHHELD`,
		`  ✗ rule_local  Allow LAN NTP — Local rule "Allow LAN NTP" was deleted on the device while NetDefense moved it, and the move re-created it with OPNsense's defaults (a pass rule); deleting that failed, so delete it on the device: HTTP 500`,
		`      code: RULE_LOCAL_RENUMBER_UNVERIFIED`,
		`  ~ rule        Allow-HTTPS-from-SOC`,
		`  ~ rule_local  Default allow LAN to any rule`,
		`      before: [1]  after: [2]`,
		`      code: RULE_LOCAL_RENUMBERED`,
	}, "\n")
	if got := FormatTaskMessage(raw); got != want {
		t.Errorf("rendered SYNC result:\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatTaskMessage_AliasResult: an alias the device already holds, a new
// one, and one refused for a key the device's alias model lacks.
func TestFormatTaskMessage_AliasResult(t *testing.T) {
	raw := `{"message": "Aliases +1 (1 errors)", "results": [` +
		`{"type": "alias", "uuid": "221f3268-0000-4000-8000-0000000000b1", "name": "soc_hosts", "action": "unchanged", "status": "success"},` +
		`{"type": "alias", "uuid": "221f3268-0000-4000-8000-0000000000b2", "name": "lab_v6_hosts", "action": "created", "status": "success"},` +
		`{"type": "alias", "uuid": "221f3268-0000-4000-8000-0000000000b3", "name": "geo_block", "action": "blocked", "status": "blocked",` +
		` "code": "ALIAS_FIELD_UNKNOWN", "error": "Alias \"geo_block\" (snippet \"geo-block\"): field \"prto\" (did you mean \"proto\"?) is not in this device's alias model (OPNsense 26.7)"}` +
		`], "validation_errors": []}`

	want := strings.Join([]string{
		`Aliases +1 (1 errors)`,
		``,
		`Changes:`,
		`  ✗ alias  geo_block — Alias "geo_block" (snippet "geo-block"): field "prto" (did you mean "proto"?) is not in this device's alias model (OPNsense 26.7)`,
		`      code: ALIAS_FIELD_UNKNOWN`,
		`  + alias  lab_v6_hosts`,
		`  = alias  soc_hosts`,
	}, "\n")
	if got := FormatTaskMessage(raw); got != want {
		t.Errorf("rendered SYNC result:\n%s\n--- want ---\n%s", got, want)
	}
}
