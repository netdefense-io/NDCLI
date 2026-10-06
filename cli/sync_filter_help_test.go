package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSyncFilterHelpNamesExactOrgAndOU: NDManager matches the organization and
// OU filters by exact name, and only the device and template filters as
// regexes. Help that called --org a regex led a client to send ^acme$, which
// matches no organization.
func TestSyncFilterHelpNamesExactOrgAndOU(t *testing.T) {
	for _, cmd := range []*cobra.Command{syncStatusCmd, syncApplyCmd} {
		for _, name := range []string{"org", "ou"} {
			usage := cmd.Flags().Lookup(name).Usage
			if !strings.Contains(usage, "exact") || strings.Contains(usage, "regex") || strings.Contains(usage, "pattern") {
				t.Errorf("sync %s --%s must say it takes an exact name, got %q", cmd.Name(), name, usage)
			}
		}
		for _, name := range []string{"device", "template"} {
			if usage := cmd.Flags().Lookup(name).Usage; !strings.Contains(usage, "regex") {
				t.Errorf("sync %s --%s must say it takes a regex, got %q", cmd.Name(), name, usage)
			}
		}
	}
}
