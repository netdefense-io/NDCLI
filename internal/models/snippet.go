package models

import "strings"

// Snippet represents a configuration snippet
type Snippet struct {
	Name string `json:"name"`
	// Type is one of SnippetCreatableTypes.
	Type         string       `json:"type,omitempty"`
	Content      string       `json:"content,omitempty"`
	Priority     int          `json:"priority"`
	Organization string       `json:"organization_name,omitempty"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
}

// SnippetCreatableTypes are the snippet types offered by `snippet create`
// and `snippet list --type`, and advertised by the ndcli.snippet.create and
// ndcli.snippet.list MCP tool enums. This is the single source of truth for
// every such surface — do not hand-maintain a second copy. Treat this slice
// as read-only: it is shared, by reference, with every schema/form that
// renders it, so appending to or sorting it in place would corrupt every
// other surface too.
//
// Declared separately from SnippetPullableTypes rather than derived from
// it, so a type that is creatable but not pullable never needs to touch
// this list — TestSnippetPullableTypesAreSubsetOfCreatable
// (internal/models/snippet_test.go) keeps the two lists in that
// relationship.
var SnippetCreatableTypes = []string{
	"USER", "GROUP", "ALIAS", "RULE",
	"UNBOUND_HOST_OVERRIDE", "UNBOUND_DOMAIN_FORWARD", "UNBOUND_HOST_ALIAS", "UNBOUND_ACL",
	"ZABBIX_SETTINGS", "ZABBIX_USERPARAMETER", "ZABBIX_ALIAS",
	"AUTH_SERVER", "AUTH_ORDER",
}

// SnippetPullableTypes are the snippet types offered by `snippet pull
// --type` and advertised by the ndcli.snippet.pull MCP tool enum. It is
// always a subset of SnippetCreatableTypes (see
// TestSnippetPullableTypesAreSubsetOfCreatable) — some creatable types are
// deliberately not pullable, for example because pulling their content back
// down would round-trip a secret value through the pull path. Treat this
// slice as read-only — see the note on SnippetCreatableTypes.
//
// AUTH_SERVER and AUTH_ORDER must never be added here: there is no AUTH
// PULL (NDManager rejects it at every entry point). A device pull reads
// the object straight out of config.xml, which stores ldap_bindpw in
// plaintext — not the ${var} secret reference AUTH_SERVER content is
// authored with — so pulling it back down would leak the resolved
// password itself (TestSnippetPullableTypesExcludesAUTHTypes).
var SnippetPullableTypes = []string{
	"USER", "GROUP", "ALIAS", "RULE",
	"UNBOUND_HOST_OVERRIDE", "UNBOUND_DOMAIN_FORWARD", "UNBOUND_HOST_ALIAS", "UNBOUND_ACL",
	"ZABBIX_SETTINGS", "ZABBIX_USERPARAMETER", "ZABBIX_ALIAS",
}

// SnippetTypesHelp renders a type list for flag help and error text: a
// comma-separated list in declaration order.
func SnippetTypesHelp(types []string) string {
	return strings.Join(types, ", ")
}

// AuthOrderConsistencyRule is the exact wording for how an AUTH_ORDER
// facility's order list is checked against a device: it may name a
// managed AUTH_SERVER delivered by any of the device's templates, or a
// server already configured by hand on the device — and the check only
// happens when the device syncs, never when the snippet is saved.
// `cli/snippet.go`'s create help states this verbatim today; anything
// else that documents the rule (a docs page, say) should quote it
// exactly rather than paraphrase, so it never drifts from this constant.
const AuthOrderConsistencyRule = `every name must be an auth server delivered to the same device by any of its templates, or already configured on the device; names match exactly (case-sensitive); 'Local Database' is always valid`

// AuthPrerequisites is the exact minimum-version/consent line every
// AUTH_SERVER/AUTH_ORDER-facing surface states.
const AuthPrerequisites = "ndagent 1.19.0 or later, OPNsense 26.1.6 or later, reject_dangerous_snippets off"

// SnippetListResponse represents a paginated list of snippets
type SnippetListResponse struct {
	Items      []Snippet `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`
}
