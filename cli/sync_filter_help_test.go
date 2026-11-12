package cli

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestSyncFilterHelpNamesExactOUAndRegexes: NDManager matches the OU filter by
// exact name, and only the device and template filters as regexes. Help that
// called a name filter a regex led a client to send ^acme$, which matches
// nothing.
func TestSyncFilterHelpNamesExactOUAndRegexes(t *testing.T) {
	for _, cmd := range []*cobra.Command{syncStatusCmd, syncApplyCmd} {
		if usage := cmd.Flags().Lookup("ou").Usage; !strings.Contains(usage, "exact") || strings.Contains(usage, "regex") || strings.Contains(usage, "pattern") {
			t.Errorf("sync %s --ou must say it takes an exact name, got %q", cmd.Name(), usage)
		}
		for _, name := range []string{"device", "template"} {
			if usage := cmd.Flags().Lookup(name).Usage; !strings.Contains(usage, "regex") {
				t.Errorf("sync %s --%s must say it takes a regex, got %q", cmd.Name(), name, usage)
			}
		}
	}
}

// A sync chooses its organization with the global -o/--org, as every other
// command does (#319). A local --org filter used to hide it: -o failed to
// parse, and --org was never read by requireOrganization, so a runner with no
// default organization could not sync at all.
func TestSyncChoosesTheOrganizationWithTheGlobalFlag(t *testing.T) {
	prepareRootCommand()
	for _, cmd := range []*cobra.Command{syncStatusCmd, syncApplyCmd} {
		if cmd.LocalNonPersistentFlags().Lookup("org") != nil {
			t.Errorf("sync %s declares its own --org, which hides the global -o/--org", cmd.Name())
		}
		if !strings.Contains(cmd.Long, "global -o/--org") {
			t.Errorf("sync %s help does not say where the organization comes from: %q", cmd.Name(), cmd.Long)
		}
	}

	for _, tc := range []struct {
		name string
		path []string
		args []string
		run  func(*cobra.Command, []string) error
	}{
		{"status --org", []string{"sync", "status"}, []string{"--org", "branch-org"}, runSyncStatus},
		{"status -o", []string{"sync", "status"}, []string{"-o", "branch-org"}, runSyncStatus},
		{"apply --org", []string{"sync", "apply"}, []string{"--org", "branch-org", "--yes"}, runSyncApply},
		{"apply -o", []string{"sync", "apply"}, []string{"-o", "branch-org", "--yes"}, runSyncApply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := findCommand(t, tc.path...)
			t.Cleanup(func() { resetParsedFlags(cmd) })

			var mu sync.Mutex
			var orgs []string
			handler := func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				orgs = append(orgs, r.URL.Query().Get("organization"))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/sync/status":
					_, _ = io.WriteString(w, `{"items":[{"device_name":"fw01","organization":"branch-org"}],"total":1}`)
				case "/api/v1/sync":
					_, _ = io.WriteString(w, `{"message":"Sync triggered for 1 device(s)","devices_affected":1,"skipped":0,"tasks":[],"errors":[]}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}
			runStreams(t, handler, "json", "", func() error {
				if err := cmd.ParseFlags(tc.args); err != nil {
					return err
				}
				return tc.run(cmd, nil)
			})

			mu.Lock()
			defer mu.Unlock()
			if len(orgs) != 1 || orgs[0] != "branch-org" {
				t.Errorf("organization sent = %v, want [branch-org] (the default org is acme)", orgs)
			}
		})
	}
}

// resetParsedFlags puts the flags a test parsed on a shared command back to
// their defaults, the inherited -o/--org included.
func resetParsedFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	})
}

// -o completes organization names wherever it is given, after positional
// arguments too, and on the run commands' own -o/--organization.
func TestOrganizationFlagCompletesAfterPositionalArguments(t *testing.T) {
	prepareRootCommand()
	orig := organizationNames
	organizationNames = func() []string { return []string{"acme", "branch-org"} }
	t.Cleanup(func() { organizationNames = orig })

	for _, tc := range []struct {
		path []string
		flag string
	}{
		{[]string{"device", "describe"}, "org"},
		{[]string{"sync", "apply"}, "org"},
		{[]string{"run", "ping"}, "organization"},
	} {
		cmd := findCommand(t, tc.path...)
		complete, ok := cmd.GetFlagCompletionFunc(tc.flag)
		if !ok {
			t.Errorf("%s --%s has no completion", strings.Join(tc.path, " "), tc.flag)
			continue
		}
		if names, _ := complete(cmd, []string{"fw01"}, ""); strings.Join(names, ",") != "acme,branch-org" {
			t.Errorf("%s fw01 --%s <TAB> = %v, want the organizations", strings.Join(tc.path, " "), tc.flag, names)
		}
	}
}
