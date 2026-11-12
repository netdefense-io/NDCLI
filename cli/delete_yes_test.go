package cli

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Every delete or remove that asks can skip its prompt with -y/--yes, so a
// script or test runner does not have to pipe "y" into it (#329). Without the
// flag the prompt still decides, and an unanswered one changes nothing.
func TestDeleteCommandsSkipThePromptWithYes(t *testing.T) {
	for _, tc := range []struct {
		path []string
		args []string
		want string
	}{
		{[]string{"snippet", "delete"}, []string{"base"}, "DELETE /api/v1/organizations/acme/snippets/base"},
		{[]string{"ou", "delete"}, []string{"branch"}, "DELETE /api/v1/organizations/acme/ous/branch"},
		{[]string{"device", "remove"}, []string{"fw01"}, "DELETE /api/v1/organizations/acme/devices/fw01"},
		{[]string{"template", "delete"}, []string{"base"}, "DELETE /api/v1/organizations/acme/templates/base"},
		{[]string{"backup", "config", "delete"}, nil, "DELETE /api/v1/organizations/acme/backup-config"},
		{[]string{"org", "delete"}, []string{"old-org"}, "DELETE /api/v1/organizations/old-org"},
		{[]string{"software", "delete"}, []string{"base"}, "DELETE /api/v1/organizations/acme/software-policies/base"},
		{[]string{"org", "account", "disable"}, []string{"ann@acme.test"}, "PUT /api/v1/organizations/acme/accounts/ann@acme.test/disable"},
	} {
		name := strings.Join(tc.path, " ")
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(t, tc.path...)
			if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
				t.Fatalf("%s has no -y/--yes", name)
			}

			for _, yes := range []bool{true, false} {
				var mu sync.Mutex
				var writes []string
				handler := func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						mu.Lock()
						writes = append(writes, r.Method+" "+r.URL.Path)
						mu.Unlock()
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
				}
				stdout, _ := runStreams(t, handler, "table", "", func() error {
					if yes {
						if err := cmd.Flags().Set("yes", "true"); err != nil {
							return err
						}
						defer func() {
							f := cmd.Flags().Lookup("yes")
							_ = f.Value.Set("false")
							f.Changed = false
						}()
					}
					return cmd.RunE(cmd, tc.args)
				})

				mu.Lock()
				got := strings.Join(writes, ", ")
				mu.Unlock()
				if yes && got != tc.want {
					t.Errorf("with --yes: requests = %q, want %q", got, tc.want)
				}
				if !yes && got != "" {
					t.Errorf("without --yes and no answer: sent %q", got)
				}
				if !yes && stdout != "" {
					t.Errorf("without --yes and no answer: stdout = %q, want it empty", stdout)
				}
			}
		})
	}
}
