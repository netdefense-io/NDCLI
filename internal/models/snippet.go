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
	"TRUST_CA", "TRUST_CERT",
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
//
// TRUST_CA and TRUST_CERT must never be added here either: there is no
// TRUST PULL for either type. For TRUST_CERT a pull would also leak the
// key — config.xml stores a certificate's private key in the clear, so it
// would come back instead of the ${var} reference the snippet was authored
// with (TestSnippetPullableTypesExcludesTRUSTTypes).
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

// SnippetUUIDGuidance is the sentence every snippet-writing surface states
// about the managed uuid of the types whose device object is owned by it:
// the help of snippet create and update-content, and the ndcli.snippet.create
// and update_content tool descriptions.
const SnippetUUIDGuidance = "ALIAS, RULE and UNBOUND_* content: leave uuid out and NetDefense supplies it. " +
	"When copying another snippet's content, drop its uuid: a uuid that another snippet already holds is refused with SNIPPET_UUID_IN_USE."

// SnippetFieldMinAgentVersion is the first ndagent release that applies every
// field of a RULE, ALIAS or UNBOUND_HOST_OVERRIDE snippet. NDManager refuses
// to build a sync for an older agent that would apply one only in part
// (SNIPPET_AGENT_TOO_OLD); its floor is the same version.
const SnippetFieldMinAgentVersion = "1.23.0"

// SnippetFieldContract is what every surface that states SnippetUUIDGuidance
// also states about ALIAS, RULE and UNBOUND_* values and fields.
const SnippetFieldContract = `ALIAS, RULE and UNBOUND_* values are JSON strings in OPNsense's own form ("443", "1", "lan,wan"), as snippet pull returns them; ` +
	`enabled may also be true or false (so may UNBOUND_DOMAIN_FORWARD's forward_tcp_upstream and forward_first), ` +
	`and ALIAS content and UNBOUND_ACL networks may be lists of strings. ` +
	`Any other kind is refused when the snippet is saved, except for keys a pull copies but no device applies (%action, alias_meta_*, sort_order, ...), which are ignored. ` +
	`RULE and ALIAS take any field OPNsense's API defines for the object, and a field left out gets OPNsense's default; ` +
	`a key the device's OPNsense does not define refuses that rule or alias at sync, which the device keeps as it was (RULE_FIELD_UNKNOWN, ALIAS_FIELD_UNKNOWN), ` +
	`and the rest of the sync still applies. ` +
	`A RULE needs a non-empty action, since OPNsense's default (pass) would loosen policy by omission: it is refused when the snippet is saved, and by the agent from ` + SnippetFieldMinAgentVersion + ` at sync. ` +
	`A list value of only blanks or commas holds no option key: the sync refuses it (RULE_VALUE_INVALID, or ALIAS_VALUE_INVALID for an alias). ` +
	`A rule's sequence, like the uuid, is managed by NetDefense: one in content is ignored, and the template's position (PREPEND/APPEND) places the rule. ` +
	`Full field support needs ndagent ` + SnippetFieldMinAgentVersion + ` or later: NetDefense refuses to sync a device whose older agent ` +
	`would apply one of these snippets only in part (SNIPPET_AGENT_TOO_OLD).`

// AuthPrerequisites is the exact minimum-version/consent line every
// AUTH_SERVER/AUTH_ORDER-facing surface states.
const AuthPrerequisites = "ndagent 1.19.2 or later, OPNsense 26.1.6 or later, reject_dangerous_snippets off"

// TrustPrerequisites is the exact minimum-version/consent line every
// TRUST_CA/TRUST_CERT-facing surface states. A device on an older agent
// fails its build with TRUST_AGENT_TOO_OLD; one below the OPNsense floor
// has trust changes refused with TRUST_OPNSENSE_TOO_OLD.
const TrustPrerequisites = "ndagent 1.21.0 or later, OPNsense 26.1.11 or later, an unencrypted private key (TRUST_CERT), reject_dangerous_snippets off"

// SnippetListResponse represents a paginated list of snippets
type SnippetListResponse struct {
	Items      []Snippet `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`
}

// SnippetPull is what `snippet pull` reports: the PULL task it created and,
// once that task has completed (--wait), the snippet it wrote.
type SnippetPull struct {
	Task       string `json:"task"`
	Status     string `json:"status"`
	ConfigType string `json:"config_type,omitempty"`
	// Snippet names the snippet the server creates or updates: the --name
	// given, or the name it derives from the match key.
	Snippet string `json:"snippet,omitempty"`
	// Content is the pulled content as the completed task reported it.
	Content string `json:"content,omitempty"`
}
