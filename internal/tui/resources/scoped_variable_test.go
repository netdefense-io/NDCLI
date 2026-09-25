package resources

import "testing"

// TestScopedVarResource_ValueFieldsAreMasked guards the new-variable and
// edit forms against showing a typed value in clear text on screen — a
// variable's value can be an AUTH_SERVER's ldap_bindpw secret reference
// target, or an override of one, and the CLI/MCP surfaces already refuse
// to let a secret value travel unmasked. The TUI is a third input path
// for the same value and must not be the one place it leaks.
func TestScopedVarResource_ValueFieldsAreMasked(t *testing.T) {
	for _, action := range (ScopedVarResource{}).Actions() {
		if action.Key != "n" && action.Key != "e" {
			continue
		}
		found := false
		for _, field := range action.Form {
			if field.Key != "value" {
				continue
			}
			found = true
			if !field.Mask {
				t.Errorf("action %q value field is not masked", action.Key)
			}
		}
		if !found {
			t.Errorf("action %q has no value field", action.Key)
		}
	}
}
