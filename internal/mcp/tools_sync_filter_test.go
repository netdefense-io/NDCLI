package mcp

import (
	"strings"
	"testing"
)

// TestSyncToolsNameExactOrgAndOU is the MCP side of
// TestSyncFilterHelpNamesExactOrgAndOU: organization and ou are exact names,
// device and template regexes, in the tool descriptions and in each parameter.
func TestSyncToolsNameExactOrgAndOU(t *testing.T) {
	for _, tool := range []string{"ndcli.sync.status", "ndcli.sync.apply"} {
		if description := registeredToolDescription(t, tool); !strings.Contains(description, "organization and ou are exact names") {
			t.Errorf("%s description does not say organization and ou are exact names:\n%s", tool, description)
		}
		properties, _ := registeredToolSchema(t, tool)["properties"].(map[string]any)
		describe := func(name string) string {
			property, _ := properties[name].(map[string]any)
			description, _ := property["description"].(string)
			return description
		}
		for _, name := range []string{"organization", "ou"} {
			if d := describe(name); !strings.Contains(d, "Exact") || strings.Contains(d, "regex") {
				t.Errorf("%s %s must say it takes an exact name, got %q", tool, name, d)
			}
		}
		for _, name := range []string{"device", "template"} {
			if d := describe(name); !strings.Contains(d, "regex") {
				t.Errorf("%s %s must say it takes a regex, got %q", tool, name, d)
			}
		}
	}
}
