package models

import (
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
