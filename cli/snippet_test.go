package cli

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSnippetCreateTypeFlag_IncludesAUTHTypesByName guards `snippet create
// --type` and `snippet list --type` against AUTH_SERVER/AUTH_ORDER being
// silently dropped from models.SnippetCreatableTypes — the existing
// equality tests in cli/snippet_types_test.go would keep passing even if
// both were removed from that list, since they only compare against it,
// never against a literal expectation.
func TestSnippetCreateTypeFlag_IncludesAUTHTypesByName(t *testing.T) {
	for _, cmdName := range []struct {
		cmd  string
		help string
	}{
		{"create", snippetCreateCmd.Flags().Lookup("type").Usage},
		{"list", snippetListCmd.Flags().Lookup("type").Usage},
	} {
		for _, want := range []string{"AUTH_SERVER", "AUTH_ORDER"} {
			if !strings.Contains(cmdName.help, want) {
				t.Errorf("%s --type help does not mention %q, got: %s", cmdName.cmd, want, cmdName.help)
			}
		}
	}
}

// TestSnippetPullTypeFlag_ExcludesAUTHTypesByName is the pull-side mirror:
// AUTH_SERVER and AUTH_ORDER must never appear in `snippet pull --type`'s
// help, by name — there is no AUTH PULL.
func TestSnippetPullTypeFlag_ExcludesAUTHTypesByName(t *testing.T) {
	help := snippetPullCmd.Flags().Lookup("type").Usage
	for _, absent := range []string{"AUTH_SERVER", "AUTH_ORDER"} {
		if strings.Contains(help, absent) {
			t.Errorf("pull --type help mentions %q, which must never be pullable; got: %s", absent, help)
		}
	}
}

// TestRequireSnippetType_ErrorNamesAUTHTypes pins that the --type-required
// error text (built from models.SnippetCreatableTypes) lists the AUTH
// types alongside every other creatable type.
func TestRequireSnippetType_ErrorNamesAUTHTypes(t *testing.T) {
	err := requireSnippetType("")
	if err == nil {
		t.Fatal("requireSnippetType(\"\") = nil, want an error")
	}
	for _, want := range []string{"AUTH_SERVER", "AUTH_ORDER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

// TestSnippetCreateLong_DocumentsAUTHServerAndOrder pins the AUTH_SERVER/
// AUTH_ORDER help text — the consistency rule and the prerequisites line
// are shared constants so this and every other surface that states them
// can never drift apart, and every other requirement is checked by its
// own distinguishing phrase so a future edit that drops one silently
// fails here rather than reaching an operator first.
func TestSnippetCreateLong_DocumentsAUTHServerAndOrder(t *testing.T) {
	long := snippetCreateCmd.Long
	// Collapse whitespace so a phrase that happens to wrap across two
	// source lines doesn't fail a substring check over an embedded
	// newline — the wrapping is a formatting detail, not part of the
	// content this test pins.
	flat := strings.Join(strings.Fields(long), " ")

	if !strings.Contains(flat, strings.Join(strings.Fields(models.AuthOrderConsistencyRule), " ")) {
		t.Errorf("create --help must state the consistency rule verbatim, got: %s", long)
	}
	if !strings.Contains(flat, models.AuthPrerequisites) {
		t.Errorf("create --help must state the prerequisites verbatim, got: %s", long)
	}

	for _, want := range []string{
		// A webadmin order list must always try the local account
		// database before any external server.
		`"Local Database" must always be first`,
		// Single-quoting ${var} in shells.
		"Single-quote the ${...} reference",
		// webadmin scope + qualified recovery.
		"web GUI and password login over SSH, the console, su and sudo",
		"needs reject_dangerous_snippets off on",
		// Hand-made servers get none of NetDefense's guardrails.
		"gets none of NetDefense's guardrails",
		// Cleartext acknowledgement.
		"nd_allow_cleartext_ldap: true",
		// Attaching or detaching an AUTH_SERVER/AUTH_ORDER snippet or its
		// template (or moving a device into an OU that carries one) only
		// needs org:rw, even though authoring the content needs org:su.
		"only needs org:rw",
		"onto more devices",
		// The recovery example must be a copy-pasteable AUTH_ORDER
		// document — the top level is exactly {"facilities"}, so a
		// bare {"order": [...]} fragment would 422 if pasted as-is.
		`{"facilities": {"webadmin": {"order": ["Local Database"]}}}`,
		// CONNECT recovery names the setting it depends on, not
		// just "full remote access" in prose.
		"remote_access_policy",
		// The variable-creation example includes the required NAME
		// argument, and does not tell the reader to run --value-stdin
		// without piping something into it.
		"ndcli variable org create AD_BIND_PW --secret --value-stdin",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("create --help missing expected phrase %q\n\ngot:\n%s", want, long)
		}
	}

	// The local-database-first rule must not be overstated as an absolute
	// lockout guarantee — directory-only accounts still lose password
	// login when every server is unreachable; the very next sentence
	// covers that recovery.
	if strings.Contains(flat, "never lock out password login") {
		t.Error("create --help overstates the local-database-first rule as an absolute lockout guarantee, got: " + long)
	}
}
