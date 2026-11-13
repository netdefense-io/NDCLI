package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// actionCase is a command that changes state and reports it in one line: its
// arguments, the JSON it prints under -f json, and the line it prints in the
// human formats, unchanged by #444.
type actionCase struct {
	path   []string
	args   []string
	flags  map[string]string
	gets   map[string]string // GET path → body; {} otherwise
	answer string            // the write's body; {} when empty
	json   map[string]string
	values map[string]interface{} // the non-string values json cannot hold, as they decode (a number is a float64)
	mcp    []string               // the keys of the command's MCP tool result, which the JSON must carry too
	line   string                 // the human output, every line
}

func runActionCase(t *testing.T, tc actionCase, format string) string {
	t.Helper()
	cmd := findCommand(t, tc.path...)
	flags := map[string]string{}
	for k, v := range tc.flags {
		flags[k] = v
	}
	if cmd.Flags().Lookup("yes") != nil {
		flags["yes"] = "true"
	}
	defer setFlags(t, cmd, flags)()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, ok := tc.gets[r.URL.Path]
		if r.Method != http.MethodGet {
			body, ok = tc.answer, tc.answer != ""
		}
		if !ok {
			body = `{}`
		}
		_, _ = w.Write([]byte(body))
	}
	stdout, _ := runStreams(t, handler, format, "", func() error { return cmd.RunE(cmd, tc.args) })
	return stdout
}

// checkActionCases runs each case under -f json, which must be one document
// carrying the facts, and under every human format, which must print the
// line the command has always printed.
func checkActionCases(t *testing.T, cases []actionCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, " "), func(t *testing.T) {
			var doc map[string]interface{}
			decodeOne(t, runActionCase(t, tc, "json"), &doc)
			for k, want := range tc.json {
				if doc[k] != want {
					t.Errorf("json %s = %v, want %q (%v)", k, doc[k], want, doc)
				}
			}
			for k, want := range tc.values {
				if !reflect.DeepEqual(doc[k], want) {
					t.Errorf("json %s = %v, want %v (%v)", k, doc[k], want, doc)
				}
			}
			for _, k := range tc.mcp {
				if _, ok := doc[k]; !ok {
					t.Errorf("json lacks %s, which the MCP tool returns: %v", k, doc)
				}
			}
			for _, format := range []string{"table", "simple", "detailed"} {
				if got := runActionCase(t, tc, format); got != tc.line+"\n" {
					t.Errorf("%s: %q, want %q", format, got, tc.line+"\n")
				}
			}
		})
	}
}

// The ou and device commands print one JSON document, with the keys their MCP
// tools return (internal/mcp/tools_ou.go, tools_device.go), and the human line
// they always printed.
func TestOUAndDeviceCommandsPrintOneJSONDocument(t *testing.T) {
	checkActionCases(t, []actionCase{
		{path: []string{"ou", "create"}, args: []string{"branch"},
			json: map[string]string{"ou": "branch", "action": "created"}, mcp: []string{"organizational_unit", "action"}, line: "✓ OU created: branch"},
		{path: []string{"ou", "delete"}, args: []string{"branch"},
			json: map[string]string{"ou": "branch", "action": "deleted"}, mcp: []string{"ou", "action"}, line: "✓ OU deleted: branch"},
		{path: []string{"ou", "rename"}, args: []string{"branch", "branch-2"},
			json: map[string]string{"ou": "branch", "action": "renamed", "new_name": "branch-2"}, mcp: []string{"ou", "new_name", "action"}, line: "✓ OU renamed: branch → branch-2"},
		{path: []string{"ou", "add-device"}, args: []string{"branch", "fw01"},
			json: map[string]string{"ou": "branch", "action": "added", "device": "fw01"}, mcp: []string{"ou", "device", "action"}, line: "✓ Device added to branch: fw01"},
		{path: []string{"ou", "remove-device"}, args: []string{"branch", "fw01"},
			json: map[string]string{"ou": "branch", "action": "removed", "device": "fw01"}, mcp: []string{"ou", "device", "action"}, line: "✓ Device removed from branch: fw01"},
		{path: []string{"ou", "template", "add"}, args: []string{"branch", "base"},
			json: map[string]string{"ou": "branch", "action": "added", "template": "base"}, mcp: []string{"ou", "template", "action"}, line: "✓ Template added to branch: base"},
		{path: []string{"ou", "template", "remove"}, args: []string{"branch", "base"},
			json: map[string]string{"ou": "branch", "action": "removed", "template": "base"}, mcp: []string{"ou", "template", "action"}, line: "✓ Template removed from branch: base"},
		{path: []string{"device", "approve"}, args: []string{"fw01"},
			json: map[string]string{"device": "fw01", "action": "approved", "status": "ENABLED"}, mcp: []string{"device", "action", "status"}, line: "✓ Device approved: fw01"},
		{path: []string{"device", "rename"}, args: []string{"fw01", "fw02"},
			json: map[string]string{"device": "fw01", "action": "renamed", "new_name": "fw02"}, mcp: []string{"device", "new_name", "action"}, line: "✓ Device renamed: fw01 → fw02"},
		{path: []string{"device", "remove"}, args: []string{"fw01"},
			json: map[string]string{"device": "fw01", "action": "removed", "consequence": service.DeviceRemoveConsequence}, mcp: []string{"device", "action", "consequence"}, line: "✓ Device removed: fw01"},
	})
}

// approve-all with nothing pending is an empty result, not prose.
func TestApproveAllWithNothingPending(t *testing.T) {
	cmd := findCommand(t, "device", "approve-all")
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}
	stdout, _ := runStreams(t, handler, "json", "", func() error { return cmd.RunE(cmd, nil) })
	var doc map[string]interface{}
	decodeOne(t, stdout, &doc)
	if doc["approved"] != float64(0) || doc["failed_count"] != float64(0) || len(doc["approved_names"].([]interface{})) != 0 || len(doc["failed"].([]interface{})) != 0 {
		t.Errorf("json %v, want zero counts and empty lists", doc)
	}
	if human, _ := runStreams(t, handler, "table", "", func() error { return cmd.RunE(cmd, nil) }); human != "No pending devices found\n" {
		t.Errorf("table %q", human)
	}
}

// The org commands print one JSON document with the keys their MCP tools
// return (internal/mcp/tools_org.go); org create puts the organization object
// under its key, as ndcli.org.create does.
func TestOrgCommandsPrintOneJSONDocument(t *testing.T) {
	created := "✓ Organization created: acme-new\n  Token: tok123\n\nUse this token to register devices with the NetDefense agent.\n" +
		"To retrieve the token later: ndcli org describe acme-new\n\nTo set this organization as your default:\n  ndcli config set org acme-new"
	checkActionCases(t, []actionCase{
		{path: []string{"org", "create"}, args: []string{"acme-new"},
			gets:   map[string]string{"/api/v1/organizations": `{"items":[{"name":"acme"}],"total":1}`},
			answer: `{"name":"acme-new","token":"tok123"}`,
			json:   map[string]string{"action": "created", "agent_token": "tok123"},
			mcp:    []string{"organization", "action", "agent_token", "token_warning"}, line: created},
		{path: []string{"org", "delete"}, args: []string{"old-org"},
			json: map[string]string{"organization": "old-org", "action": "deleted", "name": "old-org"}, mcp: []string{"name", "action"}, line: "✓ Organization deleted: old-org"},
		{path: []string{"org", "set-default-ou"}, args: []string{"branch"},
			json: map[string]string{"organization": "acme", "action": "default_ou_set", "default_ou": "branch"}, mcp: []string{"organization", "default_ou"}, line: "✓ Default OU set to: branch"},
		{path: []string{"org", "invite", "send"}, args: []string{"ann@acme.test"}, flags: map[string]string{"role": "RO"},
			json: map[string]string{"organization": "acme", "action": "invited", "email": "ann@acme.test", "role": "RO"}, mcp: []string{"organization", "email", "role", "action"}, line: "✓ Invitation sent to: ann@acme.test"},
		{path: []string{"org", "invite", "send"}, args: []string{"bob@acme.test"}, flags: map[string]string{"role": "readonly"},
			json: map[string]string{"organization": "acme", "action": "invited", "email": "bob@acme.test", "role": "RO"}, mcp: []string{"organization", "email", "role", "action"}, line: "✓ Invitation sent to: bob@acme.test"},
		{path: []string{"org", "invite", "accept"}, args: []string{"globex"},
			json: map[string]string{"organization": "globex", "action": "accepted"}, mcp: []string{"organization", "action"}, line: "✓ Invitation to globex accepted"},
		{path: []string{"org", "invite", "decline"}, args: []string{"globex"},
			json: map[string]string{"organization": "globex", "action": "declined"}, mcp: []string{"organization", "action"}, line: "✓ Invitation to globex declined"},
		{path: []string{"org", "invite", "revoke"}, args: []string{"ann@acme.test"},
			json: map[string]string{"organization": "acme", "action": "revoked", "email": "ann@acme.test"}, mcp: []string{"organization", "email", "action"}, line: "✓ Invitation revoked for: ann@acme.test"},
		{path: []string{"org", "account", "disable"}, args: []string{"ann@acme.test"},
			json: map[string]string{"organization": "acme", "action": "disabled", "email": "ann@acme.test"}, mcp: []string{"organization", "email", "action"}, line: "✓ Account disabled: ann@acme.test"},
		{path: []string{"org", "account", "disable"}, args: []string{"ann@acme.test"}, flags: map[string]string{"remove": "true"},
			json: map[string]string{"organization": "acme", "action": "removed", "email": "ann@acme.test"}, mcp: []string{"organization", "email", "action"}, line: "✓ Account removed from organization: ann@acme.test"},
		{path: []string{"org", "account", "enable"}, args: []string{"ann@acme.test"},
			json: map[string]string{"organization": "acme", "action": "enabled", "email": "ann@acme.test"}, mcp: []string{"organization", "email", "action"}, line: "✓ Account enabled: ann@acme.test"},
		{path: []string{"org", "account", "role"}, args: []string{"ann@acme.test", "rw"},
			json: map[string]string{"organization": "acme", "action": "role_set", "email": "ann@acme.test", "role": "RW"}, mcp: []string{"organization", "email", "role"}, line: "✓ Role set to RW for: ann@acme.test"},
	})
}

// org create's JSON holds the organization the server answered, without the
// empty id key the model would otherwise write, and says it was not made the
// default.
func TestOrgCreateJSONCarriesTheOrganization(t *testing.T) {
	var doc struct {
		Organization map[string]interface{} `json:"organization"`
		Default      *bool                  `json:"default"`
		TokenWarning string                 `json:"token_warning"`
	}
	decodeOne(t, runActionCase(t, actionCase{path: []string{"org", "create"}, args: []string{"acme-new"},
		gets:   map[string]string{"/api/v1/organizations": `{"items":[{"name":"acme"}],"total":1}`},
		answer: `{"name":"acme-new","token":"tok123","status":"ENABLED"}`}, "json"), &doc)
	if doc.Organization["name"] != "acme-new" || doc.Organization["status"] != "ENABLED" {
		t.Errorf("organization %v", doc.Organization)
	}
	if _, ok := doc.Organization["id"]; ok {
		t.Errorf("organization carries an id key: %v", doc.Organization)
	}
	if doc.Default == nil || *doc.Default {
		t.Errorf("default = %v, want false", doc.Default)
	}
	if doc.TokenWarning != models.AgentTokenWarning {
		t.Errorf("token_warning %q", doc.TokenWarning)
	}
}

// org create made the default organization: the hint to make it one goes,
// and -f json says default is true.
func TestOrgCreatedResultWhenMadeTheDefault(t *testing.T) {
	r := orgCreatedResult("acme-new", &models.Organization{Name: "acme-new", Token: "tok123"}, true)
	if r.Message != "✓ Organization created and set as default: acme-new" || r.Details["default"] != true {
		t.Errorf("result %+v", r)
	}
	want := []string{"  Token: tok123", "", "Use this token to register devices with the NetDefense agent.", "To retrieve the token later: ndcli org describe acme-new"}
	if strings.Join(r.Notes, "|") != strings.Join(want, "|") {
		t.Errorf("notes %q, want %q", r.Notes, want)
	}
	if !strings.Contains(strings.Join(orgCreatedResult("acme-new", &models.Organization{}, false).Notes, "\n"), "ndcli config set org acme-new") {
		t.Error("the hint to make it the default is missing when it was not made one")
	}
}

// The network, link, member and prefix commands print one JSON document with
// the keys their MCP tools return (internal/mcp/tools_network.go): the object
// the server answered for a create or update, the names for a delete.
func TestNetworkCommandsPrintOneJSONDocument(t *testing.T) {
	const net = "/api/v1/organizations/acme/vpn-networks/corp"
	spokes := map[string]string{
		net:                   `{"name":"corp","auto_connect_hubs":false}`,
		net + "/members/fw01": `{"device_name":"fw01","role":"SPOKE"}`,
		net + "/members/fw02": `{"device_name":"fw02","role":"SPOKE"}`,
	}
	checkActionCases(t, []actionCase{
		{path: []string{"network", "create"}, args: []string{"corp"}, flags: map[string]string{"cidr": "10.100.0.0/24"},
			answer: `{"name":"corp"}`, json: map[string]string{"action": "created"}, mcp: []string{"network", "action"},
			line: "✓ VPN network created: corp"},
		{path: []string{"network", "update"}, args: []string{"corp"}, flags: map[string]string{"name": "corp2"},
			answer: `{"name":"corp2"}`, json: map[string]string{"action": "updated"}, mcp: []string{"network", "action"},
			line: "✓ VPN network updated: corp2"},
		{path: []string{"network", "delete"}, args: []string{"corp"},
			json: map[string]string{"network": "corp", "action": "deleted"}, mcp: []string{"network", "action"},
			line: "✓ VPN network deleted: corp"},
		{path: []string{"network", "link", "create"}, args: []string{"corp", "fw01", "fw02"}, gets: spokes,
			answer: `{"device_a":"fw01","device_b":"fw02"}`,
			json:   map[string]string{"action": "created", "network": "corp", "device_a": "fw01", "device_b": "fw02"}, mcp: []string{"link", "action"},
			line: "✓ Link created: fw01 ↔ fw02 in corp"},
		{path: []string{"network", "link", "update"}, args: []string{"corp", "fw01", "fw02"}, flags: map[string]string{"enabled": "false"}, gets: spokes,
			answer: `{"device_a":"fw01","device_b":"fw02"}`,
			json:   map[string]string{"action": "updated", "network": "corp"}, mcp: []string{"link", "action"},
			line: "✓ Link updated: fw01 ↔ fw02 in corp"},
		{path: []string{"network", "link", "delete"}, args: []string{"corp", "fw01", "fw02"}, gets: spokes,
			json: map[string]string{"network": "corp", "action": "deleted", "device_a": "fw01", "device_b": "fw02"},
			mcp:  []string{"network", "device_a", "device_b", "action"},
			line: "✓ Link deleted: fw01 ↔ fw02 in corp (devices disconnected)"},
		{path: []string{"network", "member", "add"}, args: []string{"corp", "fw01"},
			gets: map[string]string{
				net:              `{"name":"corp","auto_connect_hubs":false}`,
				net + "/members": `{"items":[{"device_name":"hub1","role":"HUB"}],"total":1}`,
			},
			answer: `{"device_name":"fw01","role":"SPOKE","overlay_ip_v4":"10.100.0.2"}`,
			json:   map[string]string{"action": "added", "network": "corp"}, mcp: []string{"member", "action"},
			line: "✓ Member added to corp: fw01 (SPOKE, 10.100.0.2)\n  Auto-connected to 1 hub(s)"},
		{path: []string{"network", "member", "update"}, args: []string{"corp", "fw01"}, flags: map[string]string{"mtu": "1400"},
			answer: `{"device_name":"fw01"}`, json: map[string]string{"action": "updated", "network": "corp"}, mcp: []string{"member", "action"},
			line: "✓ Member updated: fw01 in corp"},
		{path: []string{"network", "member", "remove"}, args: []string{"corp", "fw01"},
			json: map[string]string{"network": "corp", "device": "fw01", "action": "removed"}, mcp: []string{"network", "device", "action"},
			line: "✓ Member removed from corp: fw01"},
		{path: []string{"network", "prefix", "update"}, args: []string{"corp", "fw01", "lan"}, flags: map[string]string{"publish": "true"},
			json: map[string]string{"action": "updated", "network": "corp", "device": "fw01"}, mcp: []string{"prefix", "action"},
			line: "✓ Prefix updated: lan on fw01 in corp"},
		{path: []string{"network", "prefix", "remove"}, args: []string{"corp", "fw01", "lan"},
			json: map[string]string{"network": "corp", "device": "fw01", "variable": "lan", "action": "removed"},
			mcp:  []string{"network", "device", "variable", "action"},
			line: "✓ Prefix removed: lan from fw01 in corp"},
	})
}

// The template commands print one JSON document with the keys their MCP tools
// return (internal/mcp/tools_template.go, tools_software_policy.go).
func TestTemplateCommandsPrintOneJSONDocument(t *testing.T) {
	checkActionCases(t, []actionCase{
		{path: []string{"template", "create"}, args: []string{"base"}, answer: `{"name":"base"}`,
			json: map[string]string{"action": "created"}, mcp: []string{"template", "action"}, line: "✓ Template created: base"},
		{path: []string{"template", "update"}, args: []string{"base"}, flags: map[string]string{"name": "base-2", "description": "Edge"},
			answer: `{"name":"base-2"}`,
			json:   map[string]string{"template": "base", "action": "updated", "old_name": "base", "name": "base-2"},
			values: map[string]interface{}{"renamed": true},
			mcp:    []string{"name", "renamed", "old_name", "action"},
			line:   "✓ Template renamed: base -> base-2\n✓ Template description updated: base-2"},
		{path: []string{"template", "update"}, args: []string{"base"}, flags: map[string]string{"position": "append"},
			answer: `{"name":"base"}`,
			json:   map[string]string{"template": "base", "action": "updated", "old_name": "base", "name": "base"},
			values: map[string]interface{}{"renamed": false},
			mcp:    []string{"name", "renamed", "old_name", "action"},
			line:   "✓ Template position updated: base"},
		{path: []string{"template", "delete"}, args: []string{"base"},
			json: map[string]string{"template": "base", "action": "deleted", "name": "base"}, mcp: []string{"name", "action"},
			line: "✓ Template deleted: base"},
		{path: []string{"template", "add-snippet"}, args: []string{"base", "dns"},
			json: map[string]string{"template": "base", "action": "added", "snippet": "dns"}, mcp: []string{"template", "snippet", "action"},
			line: "✓ Snippet added to base: dns"},
		{path: []string{"template", "remove-snippet"}, args: []string{"base", "dns"},
			json: map[string]string{"template": "base", "action": "removed", "snippet": "dns"}, mcp: []string{"template", "snippet", "action"},
			line: "✓ Snippet removed from base: dns"},
		{path: []string{"template", "add-software"}, args: []string{"base", "tools"},
			json: map[string]string{"template": "base", "action": "attached", "software_policy_name": "tools"},
			mcp:  []string{"template", "software_policy_name", "action"}, line: "✓ Software policy added to base: tools"},
		{path: []string{"template", "remove-software"}, args: []string{"base", "tools"},
			json: map[string]string{"template": "base", "action": "detached", "software_policy_name": "tools"},
			mcp:  []string{"template", "software_policy_name", "action"}, line: "✓ Software policy removed from base: tools"},
	})
}

// The snippet and software policy commands print one JSON document with the
// keys their MCP tools return (internal/mcp/tools_snippet.go,
// tools_software_policy.go); a content update's conflict warnings stay
// yellow lines below it, and are "warnings" in the JSON, as over MCP.
func TestSnippetAndSoftwareCommandsPrintOneJSONDocument(t *testing.T) {
	file := filepath.Join(t.TempDir(), "content.json")
	if err := os.WriteFile(file, []byte(`{"present":["curl"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	checkActionCases(t, []actionCase{
		{path: []string{"snippet", "create"}, args: []string{"dns"}, flags: map[string]string{"type": "ALIAS", "content": `{"name":"a"}`},
			answer: `{"name":"dns"}`, json: map[string]string{"action": "created"}, mcp: []string{"snippet", "action"},
			line: "✓ Snippet created: dns"},
		{path: []string{"snippet", "update-content"}, args: []string{"dns", file},
			json: map[string]string{"snippet": "dns", "name": "dns", "action": "updated"}, mcp: []string{"name", "action"},
			line: "✓ Snippet content updated: dns"},
		{path: []string{"snippet", "rename"}, args: []string{"dns", "dns-2"},
			json: map[string]string{"snippet": "dns", "name": "dns", "new_name": "dns-2", "action": "renamed"}, mcp: []string{"name", "new_name", "action"},
			line: "✓ Snippet renamed: dns -> dns-2"},
		{path: []string{"snippet", "set-priority"}, args: []string{"dns", "500"},
			json: map[string]string{"snippet": "dns", "name": "dns", "action": "priority_set"}, values: map[string]interface{}{"priority": float64(500)},
			mcp:  []string{"name", "priority"},
			line: "✓ Priority set to 500 for: dns"},
		{path: []string{"snippet", "delete"}, args: []string{"dns"},
			json: map[string]string{"snippet": "dns", "name": "dns", "action": "deleted"}, mcp: []string{"name", "action"},
			line: "✓ Snippet deleted: dns"},
		{path: []string{"software", "create"}, args: []string{"tools"},
			answer: `{"name":"tools"}`, json: map[string]string{"action": "created"}, mcp: []string{"software_policy", "action"},
			line: "✓ Software policy created: tools\n  Use 'ndcli software require-package tools <pkg>' or 'block-package tools <pkg>' to populate it."},
		{path: []string{"software", "update-content"}, args: []string{"tools", file},
			answer: `{"name":"tools","warnings":["curl is blocked by policy base"]}`,
			json:   map[string]string{"software_policy": "tools", "name": "tools", "action": "updated"},
			values: map[string]interface{}{"warnings": []interface{}{"curl is blocked by policy base"}},
			mcp:    []string{"name", "action", "warnings"},
			line:   "✓ Software policy content updated: tools\n\n⚠ 1 conflict warning(s) — the change was saved, but sync will fail until they are resolved:\n  • curl is blocked by policy base"},
		{path: []string{"software", "rename"}, args: []string{"tools", "tools-2"},
			json: map[string]string{"software_policy": "tools", "name": "tools", "new_name": "tools-2", "action": "renamed"}, mcp: []string{"name", "new_name", "action"},
			line: "✓ Software policy renamed: tools -> tools-2"},
		{path: []string{"software", "delete"}, args: []string{"tools"},
			json: map[string]string{"software_policy": "tools", "name": "tools", "action": "deleted"}, mcp: []string{"name", "action"},
			line: "✓ Software policy deleted: tools"},
	})
}

// The schedule and variable commands print one JSON document with the keys
// their MCP tools return (internal/mcp/tools_schedule.go, tools_variable.go):
// an enable or disable returns the schedule or spec itself, so its fields are
// the document's, beside the action.
func TestScheduleAndVariableCommandsPrintOneJSONDocument(t *testing.T) {
	schedule := `{"name":"nightly","enabled":true,"schedule":"0 3 * * *","timezone":"UTC","organization_name":"acme"}`
	spec := `{"code":"Ab12Cd34","schedule_name":"nightly","kind":"RUN","firmware_track":null,"request":{},"enabled":true}`
	scheduleKeys := []string{"name", "enabled", "schedule", "timezone", "organization_name", "action"}
	specKeys := []string{"code", "schedule_name", "kind", "firmware_track", "request", "enabled", "action"}
	variable := `{"name":"region","value":"eu","scope":"ORG","scope_name":null,"secret":false}`
	checkActionCases(t, []actionCase{
		{path: []string{"schedule", "enable"}, args: []string{"nightly"}, answer: schedule,
			json: map[string]string{"name": "nightly", "action": "enabled", "schedule": "0 3 * * *"}, mcp: scheduleKeys,
			line: `Schedule "nightly" enabled`},
		{path: []string{"schedule", "disable"}, args: []string{"nightly"}, answer: strings.Replace(schedule, `"enabled":true`, `"enabled":false`, 1),
			json: map[string]string{"name": "nightly", "action": "disabled"}, mcp: scheduleKeys,
			line: `Schedule "nightly" disabled`},
		{path: []string{"schedule", "delete"}, args: []string{"nightly"}, flags: map[string]string{"force": "true"},
			json: map[string]string{"name": "nightly", "action": "deleted"}, mcp: []string{"name", "deleted"},
			line: `Schedule "nightly" deleted`},
		{path: []string{"schedule", "tasks", "enable"}, args: []string{"Ab12Cd34"}, answer: spec,
			json: map[string]string{"code": "Ab12Cd34", "action": "enabled", "schedule_name": "nightly", "kind": "RUN"}, mcp: specKeys,
			line: `Spec Ab12Cd34 (schedule "nightly") enabled`},
		{path: []string{"schedule", "tasks", "disable"}, args: []string{"Ab12Cd34"}, answer: spec,
			json: map[string]string{"code": "Ab12Cd34", "action": "disabled"}, mcp: specKeys,
			line: `Spec Ab12Cd34 (schedule "nightly") disabled`},
		{path: []string{"schedule", "tasks", "remove"}, args: []string{"Ab12Cd34"}, flags: map[string]string{"force": "true"},
			gets: map[string]string{"/api/v1/organizations/acme/scheduled-tasks/Ab12Cd34": spec},
			json: map[string]string{"code": "Ab12Cd34", "action": "removed"}, mcp: []string{"code", "removed"},
			line: "Spec Ab12Cd34 removed"},
		{path: []string{"variable", "org", "create"}, args: []string{"region", "eu"}, answer: variable,
			json: map[string]string{"action": "created"}, mcp: []string{"variable", "action"},
			line: "Variable created: region (at organization level)"},
		{path: []string{"variable", "device", "create"}, args: []string{"fw01", "region", "us"}, answer: variable,
			json: map[string]string{"action": "created"}, mcp: []string{"variable", "action"},
			line: "Variable created: region (at device 'fw01')"},
		{path: []string{"variable", "org", "set"}, args: []string{"region"}, flags: map[string]string{"value": "us"}, answer: variable,
			json: map[string]string{"action": "updated"}, mcp: []string{"variable", "action"},
			line: "Variable updated: region"},
		{path: []string{"variable", "org", "delete"}, args: []string{"region"},
			json: map[string]string{"name": "region", "action": "deleted", "scope": "org", "entity": ""}, mcp: []string{"name", "scope", "entity", "cascade_count", "action"},
			line: "Variable deleted: region"},
		{path: []string{"variable", "device", "delete"}, args: []string{"fw01", "region"},
			json: map[string]string{"name": "region", "action": "deleted", "scope": "device", "entity": "fw01"}, mcp: []string{"name", "scope", "entity", "cascade_count", "action"},
			line: "Variable deleted: region"},
	})
}

// A variable create or set puts the variable the server answered under
// "variable", as ndcli.variable.create and set do: for a secret, the value the
// server answers is its redaction.
func TestVariableCreateJSONCarriesTheVariable(t *testing.T) {
	var doc struct {
		Variable map[string]interface{} `json:"variable"`
	}
	decodeOne(t, runActionCase(t, actionCase{path: []string{"variable", "org", "create"}, args: []string{"bind-pw", "s3cret"},
		flags:  map[string]string{"secret": "true"},
		answer: `{"name":"bind-pw","value":"********","scope":"ORG","scope_name":null,"secret":true}`}, "json"), &doc)
	if doc.Variable["name"] != "bind-pw" || doc.Variable["secret"] != true || doc.Variable["value"] != "********" {
		t.Errorf("variable %v", doc.Variable)
	}
}

// The warnings shown before a prompt go to stderr, so -f json is still one
// document: an org variable's overrides, and the backup spec's.
func TestScheduleAndVariableWarningsGoToStderr(t *testing.T) {
	cases := []struct {
		path        []string
		args        []string
		flags       map[string]string
		get, body   string
		key, warned string
		want        interface{}
	}{
		{path: []string{"variable", "org", "delete"}, args: []string{"region"}, flags: map[string]string{"yes": "true"},
			get:  "/api/v1/organizations/acme/variables",
			body: `{"items":[{"name":"region","definitions":[{"scope":"organization"},{"scope":"device","scope_name":"fw01"}]}],"total":1}`,
			key:  "cascade_count", want: float64(1), warned: "This will also delete 1 override(s)"},
		{path: []string{"schedule", "tasks", "remove"}, args: []string{"Ab12Cd34"}, flags: map[string]string{"force": "true"},
			get:  "/api/v1/organizations/acme/scheduled-tasks/Ab12Cd34",
			body: `{"code":"Ab12Cd34","schedule_name":"nightly","kind":"BACKUP","request":{},"enabled":true}`,
			key:  "removed", want: true, warned: "This is the org's BACKUP spec"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, " "), func(t *testing.T) {
			cmd := findCommand(t, tc.path...)
			defer setFlags(t, cmd, tc.flags)()
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body := `{}`
				if r.Method == http.MethodGet && r.URL.Path == tc.get {
					body = tc.body
				}
				_, _ = w.Write([]byte(body))
			}
			stdout, stderr := runStreams(t, handler, "json", "", func() error { return cmd.RunE(cmd, tc.args) })
			var doc map[string]interface{}
			decodeOne(t, stdout, &doc)
			if doc[tc.key] != tc.want {
				t.Errorf("json %s = %v, want %v (%v)", tc.key, doc[tc.key], tc.want, doc)
			}
			if !strings.Contains(stderr, tc.warned) {
				t.Errorf("stderr %q lacks %q", stderr, tc.warned)
			}
		})
	}
}

// A spec's request comes out as the server sent it, its key order and number
// text kept, as ndcli.schedule.tasks_enable returns it.
func TestScheduleTasksEnableKeepsTheRequestAsSent(t *testing.T) {
	request := `{"b":1,"a":1.10,"n":12345678901234567890,"e":1e3}`
	stdout := runActionCase(t, actionCase{path: []string{"schedule", "tasks", "enable"}, args: []string{"Ab12Cd34"},
		answer: `{"code":"Ab12Cd34","schedule_name":"nightly","kind":"RUN","request":` + request + `,"enabled":true}`}, "json")
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(stdout)); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, stdout)
	}
	if !strings.Contains(compact.String(), `"request":`+request) {
		t.Errorf("request changed: %s", compact.String())
	}
}

// The backup commands print one JSON document with the keys their MCP tools
// return (internal/mcp/tools_backup.go). The encryption key commands have no
// tool (secret material); set reads the key from a terminal and is not run
// here.
func TestBackupCommandsPrintOneJSONDocument(t *testing.T) {
	attach := `{"code":"Ab12Cd34","kind":"BACKUP","schedule_name":"nightly","enabled":true,"created_by":"ann@acme.test"}`
	attachKeys := []string{"action", "code", "kind", "schedule_name", "enabled", "created_by", "created_at"}
	checkActionCases(t, []actionCase{
		{path: []string{"backup", "config", "delete"},
			json: map[string]string{"organization": "acme", "action": "deleted"}, mcp: []string{"action"},
			line: "Backup configuration deleted"},
		{path: []string{"backup", "config", "enable"},
			json: map[string]string{"organization": "acme", "action": "enabled", "status": "ENABLED"}, mcp: []string{"status"},
			line: "Backup configuration enabled"},
		{path: []string{"backup", "config", "disable"},
			json: map[string]string{"organization": "acme", "action": "disabled", "status": "DISABLED"}, mcp: []string{"status"},
			line: "Backup configuration disabled"},
		{path: []string{"backup", "enable"}, args: []string{"fw01"},
			json: map[string]string{"device": "fw01", "action": "enabled"}, values: map[string]interface{}{"enabled": true}, mcp: []string{"device", "enabled"},
			line: "Backup enabled for device: fw01"},
		{path: []string{"backup", "disable"}, args: []string{"fw01"},
			json: map[string]string{"device": "fw01", "action": "disabled"}, values: map[string]interface{}{"enabled": false}, mcp: []string{"device", "enabled"},
			line: "Backup disabled for device: fw01"},
		{path: []string{"backup", "encryption-key", "remove"}, args: []string{"fw01"},
			json: map[string]string{"device": "fw01", "action": "encryption_key_removed"},
			line: "Encryption key override removed for device: fw01 (will use organization default)"},
		{path: []string{"backup", "config", "set"}, flags: map[string]string{"schedule": "nightly"}, answer: attach,
			json: map[string]string{"schedule_name": "nightly", "action": "attached", "code": "Ab12Cd34", "kind": "BACKUP"}, mcp: attachKeys,
			line: `Backup attached to schedule "nightly" — spec code: Ab12Cd34`},
		{path: []string{"backup", "config", "set"}, flags: map[string]string{"no-schedule": "true"}, answer: `{"detached":true,"organization_name":"acme"}`,
			json: map[string]string{"organization_name": "acme", "action": "detached"}, values: map[string]interface{}{"detached": true},
			mcp: []string{"action", "detached", "organization_name"}, line: "Backup detached from schedule (org: acme)"},
		{path: []string{"backup", "config", "set"}, flags: map[string]string{"s3-bucket": "b2"}, answer: `{"s3_bucket":"b2","status":"ENABLED"}`,
			json: map[string]string{"action": "updated"}, mcp: []string{"config", "action"},
			line: "Backup configuration updated"},
		{path: []string{"backup", "config", "set"}, flags: map[string]string{"s3-bucket": "b2", "schedule": "nightly"},
			answer: `{"s3_bucket":"b2","status":"ENABLED","code":"Ab12Cd34","kind":"BACKUP","schedule_name":"nightly","enabled":true}`,
			json:   map[string]string{"action": "updated"}, mcp: []string{"config", "action", "schedule"},
			line: "Backup configuration updated\nBackup attached to schedule \"nightly\" — spec code: Ab12Cd34"},
	})
}

// config set creates a configuration when there is none, and puts the schedule
// step under "schedule"; with nothing to change it prints nothing in the human
// formats, as before, and the configuration under -f json.
func TestBackupConfigSetCreateAndUnchanged(t *testing.T) {
	cmd := findCommand(t, "backup", "config", "set")
	created := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"no backup configuration"}}`))
		case strings.HasSuffix(r.URL.Path, "/schedule"):
			_, _ = w.Write([]byte(`{"code":"Ab12Cd34","kind":"BACKUP","schedule_name":"nightly","enabled":true}`))
		default:
			_, _ = w.Write([]byte(`{"s3_bucket":"b1","status":"ENABLED","has_encryption_key":true}`))
		}
	}
	restore := setFlags(t, cmd, map[string]string{"s3-endpoint": "https://s3.test", "s3-bucket": "b1", "s3-key-id": "AK",
		"s3-access-key": "secret", "encryption-key": "key", "schedule": "nightly"})
	var doc struct {
		Config   map[string]interface{} `json:"config"`
		Action   string                 `json:"action"`
		Schedule map[string]interface{} `json:"schedule"`
	}
	stdout, _ := runStreams(t, created, "json", "", func() error { return cmd.RunE(cmd, nil) })
	decodeOne(t, stdout, &doc)
	if doc.Action != "created" || doc.Config["s3_bucket"] != "b1" || doc.Schedule["action"] != "attached" || doc.Schedule["schedule_name"] != "nightly" {
		t.Errorf("json %s", stdout)
	}
	if human, _ := runStreams(t, created, "table", "", func() error { return cmd.RunE(cmd, nil) }); human != "Backup configuration created\nBackup attached to schedule \"nightly\" — spec code: Ab12Cd34\n" {
		t.Errorf("table %q", human)
	}
	restore()

	existing := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"s3_bucket":"b1","status":"ENABLED"}`))
	}
	doc.Config, doc.Action = nil, ""
	stdout, _ = runStreams(t, existing, "json", "", func() error { return cmd.RunE(cmd, nil) })
	decodeOne(t, stdout, &doc)
	if doc.Action != "unchanged" || doc.Config["s3_bucket"] != "b1" {
		t.Errorf("json %s", stdout)
	}
	for _, format := range []string{"table", "simple", "detailed"} {
		if human, _ := runStreams(t, existing, format, "", func() error { return cmd.RunE(cmd, nil) }); human != "" {
			t.Errorf("%s %q, want nothing", format, human)
		}
	}
}

// The secret prompt goes to stderr: on a stdin that is no terminal the key
// cannot be read, and stdout stays empty.
func TestBackupSecretPromptGoesToStderr(t *testing.T) {
	cmd := findCommand(t, "backup", "encryption-key", "set")
	stdout, stderr := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}, "json", "", func() error {
		if err := cmd.RunE(cmd, []string{"fw01"}); err == nil {
			t.Error("the key was read from a stdin that is no terminal")
		}
		return nil
	})
	if stdout != "" || !strings.Contains(stderr, "Encryption Key: ") {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

// An update that went through is reported when the schedule step after it
// fails, and the command still fails.
func TestBackupConfigSetReportsTheUpdateWhenTheScheduleFails(t *testing.T) {
	cmd := findCommand(t, "backup", "config", "set")
	defer setFlags(t, cmd, map[string]string{"s3-bucket": "b2", "schedule": "nightly"})()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/schedule") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"SCHEDULE_NOT_FOUND","message":"no schedule nightly"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"s3_bucket":"b2","status":"ENABLED"}`))
	}
	var runErr error
	stdout, _ := runStreams(t, handler, "json", "", func() error { runErr = cmd.RunE(cmd, nil); return nil })
	if runErr == nil {
		t.Fatal("the failed schedule step did not fail the command")
	}
	var doc map[string]interface{}
	decodeOne(t, stdout, &doc)
	if doc["action"] != "updated" {
		t.Errorf("json %v", doc)
	}
	if human, _ := runStreams(t, handler, "table", "", func() error { _ = cmd.RunE(cmd, nil); return nil }); human != "Backup configuration updated\n" {
		t.Errorf("table %q", human)
	}
}

// An update that went through is reported when the user declines the detach
// that follows it.
func TestBackupConfigSetReportsTheUpdateWhenTheDetachIsDeclined(t *testing.T) {
	cmd := findCommand(t, "backup", "config", "set")
	defer setFlags(t, cmd, map[string]string{"s3-bucket": "b2", "no-schedule": "true"})()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/schedule") {
			t.Errorf("the declined detach was sent")
		}
		_, _ = w.Write([]byte(`{"s3_bucket":"b2","status":"ENABLED"}`))
	}
	stdout, stderr := runStreams(t, handler, "json", "n\n", func() error { return cmd.RunE(cmd, nil) })
	var doc map[string]interface{}
	decodeOne(t, stdout, &doc)
	if doc["action"] != "updated" || !strings.Contains(stderr, "Cancelled") {
		t.Errorf("stdout %s, stderr %q", stdout, stderr)
	}
}

// The software policy entry edits print one JSON document with the keys their
// MCP tools return (internal/mcp/tools_software_policy.go): the packages'
// outcomes and a summary, or the repository's or external package's, with the
// conflict notices under "warnings" when there are some.
func TestSoftwareEntryEditsPrintOneJSONDocument(t *testing.T) {
	policy := func(content string) map[string]string {
		raw, _ := json.Marshal(map[string]string{"name": "tools", "content": content})
		return map[string]string{"/api/v1/organizations/acme/software-policies/tools": string(raw)}
	}
	required := policy(`{"present":["bash"]}`)
	withRepo := policy(`{"repositories":[{"name":"r1","url":"https://pkg.test/r1","priority":1,"enabled":true}]}`)
	withExternal := policy(`{"external":[{"name":"tool","version":"1.0","url":"https://pkg.test/tool.pkg"}]}`)
	packageKeys := []string{"policy", "op", "results", "summary"}
	entryKeys := []string{"policy", "kind", "name", "action"}
	checkActionCases(t, []actionCase{
		{path: []string{"software", "require-package"}, args: []string{"tools", "curl", "bash"}, gets: required,
			json: map[string]string{"policy": "tools", "op": "require", "summary": "1 change(s), 0 move(s), 1 no-op(s)"},
			values: map[string]interface{}{"results": []interface{}{
				map[string]interface{}{"package": "curl", "action": "required"},
				map[string]interface{}{"package": "bash", "action": "no-change", "from": "required"}}},
			mcp:  packageKeys,
			line: "✓ Required: curl\nℹ bash: already required (no change)\nApplied 1 change(s) (1 no-op)."},
		{path: []string{"software", "block-package"}, args: []string{"tools", "bash"}, gets: required,
			json: map[string]string{"policy": "tools", "op": "block", "summary": "0 change(s), 1 move(s), 0 no-op(s)"}, mcp: packageKeys,
			line: "↻ Blocked: bash (was: required)\nApplied 1 change(s)."},
		{path: []string{"software", "waive-package"}, args: []string{"tools", "bash"}, gets: required,
			json: map[string]string{"policy": "tools", "op": "waive", "summary": "1 change(s), 0 move(s), 0 no-op(s)"}, mcp: packageKeys,
			line: "✓ Waived: bash (was: required)\nApplied 1 change(s)."},
		{path: []string{"software", "require-package"}, args: []string{"tools", "bash"}, gets: required,
			json: map[string]string{"summary": "0 change(s), 0 move(s), 1 no-op(s)"}, mcp: packageKeys,
			line: "ℹ bash: already required (no change)\nNo changes."},
		{path: []string{"software", "require-package"}, args: []string{"tools", "curl"}, gets: required,
			answer: `{"name":"tools","warnings":["curl is blocked by policy base"]}`,
			values: map[string]interface{}{"warnings": []interface{}{"curl is blocked by policy base"}},
			mcp:    append(packageKeys, "warnings"),
			line: "✓ Required: curl\nApplied 1 change(s).\n\n⚠ 1 conflict warning(s) — the change was saved, but sync will fail until they are resolved:\n" +
				"  • curl is blocked by policy base"},
		{path: []string{"software", "add-repo"}, args: []string{"tools"}, gets: required,
			flags: map[string]string{"name": "r2", "url": "https://pkg.test/r2", "priority": "2", "signature-type": "none"},
			json:  map[string]string{"policy": "tools", "kind": "repository", "name": "r2", "action": "added"}, mcp: entryKeys,
			line: "✓ Repository added: r2"},
		{path: []string{"software", "add-repo"}, args: []string{"tools"}, gets: withRepo,
			flags: map[string]string{"name": "r1", "url": "https://pkg.test/r1-new"},
			json:  map[string]string{"kind": "repository", "name": "r1", "action": "updated"}, mcp: entryKeys,
			line: "✓ Repository updated: r1"},
		{path: []string{"software", "remove-repo"}, args: []string{"tools", "r1"}, gets: withRepo,
			json: map[string]string{"policy": "tools", "kind": "repository", "name": "r1", "action": "removed"}, mcp: entryKeys,
			line: "✓ Repository removed: r1"},
		{path: []string{"software", "remove-repo"}, args: []string{"tools", "r9"}, gets: withRepo,
			json: map[string]string{"kind": "repository", "name": "r9", "action": "no-change"}, mcp: entryKeys,
			line: "ℹ Repository r9: no change"},
		{path: []string{"software", "add-external"}, args: []string{"tools"}, gets: required,
			flags: map[string]string{"name": "tool", "version": "1.0", "url": "https://pkg.test/tool.pkg"},
			json:  map[string]string{"policy": "tools", "kind": "external", "name": "tool", "action": "added"}, mcp: entryKeys,
			line: "✓ External package added: tool"},
		{path: []string{"software", "remove-external"}, args: []string{"tools", "tool"}, gets: withExternal,
			answer: `{"name":"tools","warnings":["tool conflicts with base"]}`,
			json:   map[string]string{"policy": "tools", "kind": "external", "name": "tool", "action": "removed"},
			values: map[string]interface{}{"warnings": []interface{}{"tool conflicts with base"}},
			mcp:    append(entryKeys, "warnings"),
			line: "✓ External package removed: tool\n\n⚠ 1 conflict warning(s) — the change was saved, but sync will fail until they are resolved:\n" +
				"  • tool conflicts with base"},
	})
}

// objectFields leaves the resource key and "action" to the result, and fails
// on a value that does not encode to an object.
func TestObjectFieldsLeavesTheReservedKeysOut(t *testing.T) {
	fields, err := objectFields(map[string]interface{}{"name": "Nightly", "action": "x", "enabled": true}, "name")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["name"]; ok {
		t.Errorf("the resource key stayed: %v", fields)
	}
	if _, ok := fields["action"]; ok {
		t.Errorf("action stayed: %v", fields)
	}
	if string(fields["enabled"].(json.RawMessage)) != "true" {
		t.Errorf("fields %v", fields)
	}
	if _, err := objectFields([]string{"a"}, "name"); err == nil {
		t.Error("a list encoded to fields")
	}
}
