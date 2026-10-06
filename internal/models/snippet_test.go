package models

import (
	"slices"
	"strings"
	"testing"
)

// TestAuthOrderConsistencyRule_MatchesTheIntendedWording pins the
// constant against a literal copy of its intended wording ("'Local
// Database' is always valid", single-quoted) rather than only against
// itself — every consumer (cli/snippet.go help) compares against this
// constant, so an accidental edit to it would otherwise pass every test
// in this repo silently.
func TestAuthOrderConsistencyRule_MatchesTheIntendedWording(t *testing.T) {
	const wantWording = `every name must be an auth server delivered to the same device by any of its templates, or already configured on the device; names match exactly (case-sensitive); 'Local Database' is always valid`
	if AuthOrderConsistencyRule != wantWording {
		t.Errorf("AuthOrderConsistencyRule drifted from the intended wording:\ngot:  %s\nwant: %s", AuthOrderConsistencyRule, wantWording)
	}
	if !strings.Contains(AuthOrderConsistencyRule, `'Local Database' is always valid`) {
		t.Error("AuthOrderConsistencyRule must single-quote 'Local Database', matching the intended punctuation")
	}
}

// TestSnippetPullableTypesAreSubsetOfCreatable guards the invariant that
// every pullable type is also creatable — SnippetPullableTypes narrows
// SnippetCreatableTypes, it never diverges from it.
func TestSnippetPullableTypesAreSubsetOfCreatable(t *testing.T) {
	creatable := make(map[string]bool, len(SnippetCreatableTypes))
	for _, ty := range SnippetCreatableTypes {
		creatable[ty] = true
	}
	for _, ty := range SnippetPullableTypes {
		if !creatable[ty] {
			t.Errorf("pullable type %q is not in SnippetCreatableTypes", ty)
		}
	}
}

// TestSnippetPullableTypesExcludesAUTHTypes guards against AUTH_SERVER and
// AUTH_ORDER ever being added to SnippetPullableTypes: pulling their
// content back down would round-trip secret values through the pull path,
// so they may be creatable without being pullable. The subset test alone
// can't catch this — it passes even if both lists gain the same type.
func TestSnippetPullableTypesExcludesAUTHTypes(t *testing.T) {
	for _, ty := range []string{"AUTH_SERVER", "AUTH_ORDER"} {
		for _, pullable := range SnippetPullableTypes {
			if pullable == ty {
				t.Errorf("SnippetPullableTypes contains %q, which must never be pullable", ty)
			}
		}
	}
}

// TestSnippetPullableTypesExcludesTRUSTTypes is the TRUST_CA/TRUST_CERT
// counterpart: there is no TRUST PULL, and for TRUST_CERT one would hand
// back the private key itself (config.xml stores it in the clear) rather
// than the ${var} reference the snippet was authored with. Both types must
// stay creatable (the subset test can't tell a type that was never added
// from one that was dropped).
func TestSnippetPullableTypesExcludesTRUSTTypes(t *testing.T) {
	for _, ty := range []string{"TRUST_CA", "TRUST_CERT"} {
		if !slices.Contains(SnippetCreatableTypes, ty) {
			t.Errorf("SnippetCreatableTypes is missing %q", ty)
		}
		if slices.Contains(SnippetPullableTypes, ty) {
			t.Errorf("SnippetPullableTypes contains %q, which must never be pullable", ty)
		}
	}
}

// TestTrustPrerequisites_MatchesTheIntendedWording pins the constant
// against a literal copy, for the same reason as
// TestAuthOrderConsistencyRule_MatchesTheIntendedWording: every consumer
// compares against the constant itself.
func TestTrustPrerequisites_MatchesTheIntendedWording(t *testing.T) {
	const want = "ndagent 1.21.0 or later, OPNsense 26.1.11 or later, an unencrypted private key (TRUST_CERT), reject_dangerous_snippets off"
	if TrustPrerequisites != want {
		t.Errorf("TrustPrerequisites drifted:\ngot:  %s\nwant: %s", TrustPrerequisites, want)
	}
}

// TestSnippetFieldContract_MatchesTheIntendedWording pins the contract and
// its agent version against literal copies, for the same reason as
// TestAuthOrderConsistencyRule_MatchesTheIntendedWording. The version is
// NDManager's FULL_FIELD_MIN_AGENT_VERSION, below which it refuses the build
// with SNIPPET_AGENT_TOO_OLD.
func TestSnippetFieldContract_MatchesTheIntendedWording(t *testing.T) {
	if SnippetFieldMinAgentVersion != "1.23.0" {
		t.Errorf("SnippetFieldMinAgentVersion = %q, want 1.23.0", SnippetFieldMinAgentVersion)
	}
	const want = `ALIAS, RULE and UNBOUND_* values are JSON strings in OPNsense's own form ("443", "1", "lan,wan"), as snippet pull returns them; ` +
		`enabled may also be true or false (so may UNBOUND_DOMAIN_FORWARD's forward_tcp_upstream and forward_first), ` +
		`and ALIAS content and UNBOUND_ACL networks may be lists of strings. ` +
		`Any other kind is refused when the snippet is saved, except for keys a pull copies but no device applies (%action, alias_meta_*, sort_order, ...), which are ignored. ` +
		`RULE and ALIAS take any field OPNsense's API defines for the object, and a field left out gets OPNsense's default; ` +
		`a key the device's OPNsense does not define refuses that rule or alias at sync, which the device keeps as it was (RULE_FIELD_UNKNOWN, ALIAS_FIELD_UNKNOWN), ` +
		`and the rest of the sync still applies. ` +
		`A RULE needs a non-empty action, since OPNsense's default (pass) would loosen policy by omission: it is refused when the snippet is saved, and by the agent from 1.23.0 at sync. ` +
		`A list value of only blanks or commas holds no option key: the sync refuses it (RULE_VALUE_INVALID, or ALIAS_VALUE_INVALID for an alias). ` +
		`A rule's sequence, like the uuid, is managed by NetDefense: one in content is ignored, and the template's position (PREPEND/APPEND) places the rule. ` +
		`Full field support needs ndagent 1.23.0 or later: NetDefense refuses to sync a device whose older agent ` +
		`would apply one of these snippets only in part (SNIPPET_AGENT_TOO_OLD).`
	if SnippetFieldContract != want {
		t.Errorf("SnippetFieldContract drifted:\ngot:  %s\nwant: %s", SnippetFieldContract, want)
	}
}

// TestSnippetTypeListsHaveNoDuplicates catches a copy-paste slip in either
// list — a duplicate entry would make a stringEnumProperty/Options list
// advertise the same value twice.
func TestSnippetTypeListsHaveNoDuplicates(t *testing.T) {
	for name, types := range map[string][]string{
		"SnippetCreatableTypes": SnippetCreatableTypes,
		"SnippetPullableTypes":  SnippetPullableTypes,
	} {
		seen := make(map[string]bool, len(types))
		for _, ty := range types {
			if seen[ty] {
				t.Errorf("%s contains %q more than once", name, ty)
			}
			seen[ty] = true
		}
	}
}
