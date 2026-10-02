package cli

import (
	"encoding/json"
	"maps"
	"slices"
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

// TestSnippetTypeFlags_TRUSTTypesCreatableNeverPullable is the TRUST_CA/
// TRUST_CERT counterpart of the AUTH by-name tests above: both types must
// reach `snippet create --type`, `snippet list --type` and the --type
// required error by name, and never `snippet pull --type`.
func TestSnippetTypeFlags_TRUSTTypesCreatableNeverPullable(t *testing.T) {
	requiredErr := requireSnippetType("")
	if requiredErr == nil {
		t.Fatal("requireSnippetType(\"\") = nil, want an error")
	}
	creatable := map[string]string{
		"create --type":      snippetCreateCmd.Flags().Lookup("type").Usage,
		"list --type":        snippetListCmd.Flags().Lookup("type").Usage,
		"--type is required": requiredErr.Error(),
	}
	pullHelp := snippetPullCmd.Flags().Lookup("type").Usage

	for _, ty := range []string{"TRUST_CA", "TRUST_CERT"} {
		for surface, text := range creatable {
			if !strings.Contains(text, ty) {
				t.Errorf("%s does not mention %q, got: %s", surface, ty, text)
			}
		}
		if strings.Contains(pullHelp, ty) {
			t.Errorf("pull --type help mentions %q, which must never be pullable; got: %s", ty, pullHelp)
		}
	}
}

// TestSnippetCreateLong_DocumentsTRUSTTypes pins the TRUST_CA/TRUST_CERT
// help: the prerequisites line is a shared constant, and each other
// requirement is checked by its own distinguishing phrase.
func TestSnippetCreateLong_DocumentsTRUSTTypes(t *testing.T) {
	long := snippetCreateCmd.Long
	flat := strings.Join(strings.Fields(long), " ")

	if !strings.Contains(flat, "Prerequisites: "+models.TrustPrerequisites) {
		t.Errorf("create --help must state the TRUST prerequisites verbatim, got: %s", long)
	}

	for _, want := range []string{
		// The key is a whole-value reference to a secret org variable,
		// filled from the key file rather than typed or passed as an
		// argument.
		`"key": "${FW_KEY}"`,
		"naming an org-scope secret variable",
		"unencrypted PEM private key",
		// The key variable's only permitted sink: any number of
		// TRUST_CERT key fields, nothing else.
		"may appear only in TRUST_CERT key fields",
		"ndcli variable org create FW_KEY --secret --value-stdin < fw-wildcard.key",
		"the no-echo prompt reads only one line",
		// crt may come from a variable, which is what makes per-device
		// certificates possible.
		"or exactly ${VAR_NAME}",
		// The jq recipe for both types, and the create command it feeds.
		`jq -Rs '{name: "Corp Root CA", crt: .}' corp-root-ca.pem > corp-root-ca.json`,
		"ndcli snippet create corp-root-ca --type TRUST_CA --file corp-root-ca.json",
		`jq -Rs '{name: "fw-wildcard", crt: ., key: "${FW_KEY}"}' fw-wildcard.crt > fw-wildcard.json`,
		"ndcli snippet create fw-wildcard --type TRUST_CERT --file fw-wildcard.json",
		// Superuser only, and only from an interactive login.
		"needs org:su from an interactive login",
		"A token is not enough",
		// Variables resolved into the content are Superuser-written.
		"must be written by a Superuser",
		// Outcomes a first sync commonly hits.
		"NAME_COLLISION_UNMANAGED",
		"TRUST_KEY_MISMATCH",
		"TRUST_CA_REKEYED",
		// A rollover needs a new subject DN: the device links issuers by
		// DN, so two CAs sharing one with different keys can't coexist.
		"whose CA has a new subject DN",
		"TRUST_CA_DN_CONFLICT",
		// The key sizes and algorithms accepted at all, so a weak or
		// unsupported key is caught before a sync fails on it. RSA is
		// bounded above as well as below, per type, with one floor.
		"RSA of 2048 to 4096 bits for a TRUST_CERT or 2048 to 8192 for a TRUST_CA, or EC on P-256, P-384 or P-521",
		"A shorter RSA key is refused with TRUST_KEY_WEAK",
		"including a longer RSA key, Ed25519, Ed448 and DSA, is refused with TRUST_KEY_UNSUPPORTED",
		// name is measured in bytes, the way the device stores it.
		"1 to 255 bytes (UTF-8), with no ${ and no control characters",
		// The server mints uuid and refuses one supplied on create; an edit
		// may carry the stored one back unchanged.
		"NetDefense adds one on create and refuses one supplied there",
		"never change it",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("create --help missing expected phrase %q\n\ngot:\n%s", want, long)
		}
	}

	// Every create example (the shapes, the jq recipe) must leave uuid out,
	// or a user copying one would be refused.
	for _, line := range strings.Split(long, "\n") {
		if strings.Contains(line, "TRUST_") || strings.HasPrefix(strings.TrimSpace(line), "jq ") {
			if strings.Contains(line, "uuid") {
				t.Errorf("create example supplies a uuid, which the server refuses on create: %s", line)
			}
		}
	}

	if strings.Contains(flat, "Auth0") {
		t.Error("create --help must say \"interactive login\", never name an identity provider")
	}
}

// TestSnippetCreateLong_TRUSTContentShapesMatchTheirKeys parses the two
// content-shape lines in the help as JSON and checks their key sets, so
// the help can never show a key the server would reject as unknown, or
// leave out a required one. uuid is never shown: the server mints it.
func TestSnippetCreateLong_TRUSTContentShapesMatchTheirKeys(t *testing.T) {
	want := map[string][]string{
		"TRUST_CA":   {"crt", "name"},
		"TRUST_CERT": {"crt", "key", "name"},
	}
	found := map[string]bool{}
	for _, line := range strings.Split(snippetCreateCmd.Long, "\n") {
		ty, content, ok := strings.Cut(strings.TrimSpace(line), ": ")
		keys, isShape := want[ty]
		if !ok || !isShape {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &obj); err != nil {
			t.Errorf("%s shape in create --help is not valid JSON: %v (%s)", ty, err, content)
			continue
		}
		got := slices.Sorted(maps.Keys(obj))
		if !slices.Equal(got, keys) {
			t.Errorf("%s shape keys = %v, want %v", ty, got, keys)
		}
		found[ty] = true
	}
	for ty := range want {
		if !found[ty] {
			t.Errorf("create --help shows no %s content shape", ty)
		}
	}
}
