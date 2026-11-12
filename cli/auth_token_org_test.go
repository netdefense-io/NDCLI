package cli

import (
	"testing"
)

// On auth token create -o is the token's restriction, the same flag as --org,
// and leaves the root's organization alone.
func TestAuthTokenCreateTakesOAsItsOrg(t *testing.T) {
	cmd := findCommand(t, "auth", "token", "create")
	t.Cleanup(func() {
		f := cmd.Flags().Lookup("org")
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	before := orgName
	if err := cmd.ParseFlags([]string{"-o", "acme"}); err != nil {
		t.Fatalf("-o refused: %v", err)
	}
	if got, _ := cmd.Flags().GetString("org"); got != "acme" {
		t.Errorf("--org = %q after -o acme, want acme", got)
	}
	if orgName != before {
		t.Errorf("the root organization changed to %q", orgName)
	}
	if f := cmd.Flags().Lookup("org"); f == nil || f.Shorthand != "o" {
		t.Errorf("--org has no -o shorthand: %+v", f)
	}
}
