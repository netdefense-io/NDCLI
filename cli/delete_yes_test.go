package cli

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// promptCase is a command that asks before it changes anything: what it is
// given, what the server answers to its reads, and the write it sends once
// confirmed.
type promptCase struct {
	path  []string
	args  []string
	flags map[string]string
	gets  map[string]string
	want  string
	// answer is the write's body; {} when empty.
	answer string
	// local is a command that writes local state, not the server's: only
	// the runs that must not write are made.
	local bool
}

// softwarePolicyWithEntries is a software policy holding a repository named
// extra and an external package named agent.
const softwarePolicyWithEntries = `{"name":"tools","content":"{\"repositories\":[{\"name\":\"extra\",\"url\":\"https://pkg.example.test/extra\"}],\"external\":[{\"name\":\"agent\",\"version\":\"1.0\",\"url\":\"https://pkg.example.test/agent.pkg\"}]}"}`

// promptCases is every command that asks with a y/N prompt.
func promptCases() []promptCase {
	return []promptCase{
		{path: []string{"snippet", "delete"}, args: []string{"base"}, want: "DELETE /api/v1/organizations/acme/snippets/base"},
		{path: []string{"ou", "delete"}, args: []string{"branch"}, want: "DELETE /api/v1/organizations/acme/ous/branch"},
		{path: []string{"device", "remove"}, args: []string{"fw01"}, want: "DELETE /api/v1/organizations/acme/devices/fw01"},
		{path: []string{"template", "delete"}, args: []string{"base"}, want: "DELETE /api/v1/organizations/acme/templates/base"},
		{path: []string{"backup", "config", "delete"}, want: "DELETE /api/v1/organizations/acme/backup-config"},
		{path: []string{"org", "delete"}, args: []string{"old-org"}, want: "DELETE /api/v1/organizations/old-org"},
		{path: []string{"org", "unverified-repos"}, args: []string{"allow"}, want: "PUT /api/v1/organizations/acme/allow-unverified-repos",
			answer: `{"organization_name":"acme","allow_unverified_repos":true}`},
		{path: []string{"org", "unverified-repos"}, args: []string{"forbid"}, want: "PUT /api/v1/organizations/acme/allow-unverified-repos",
			answer: `{"organization_name":"acme","allow_unverified_repos":false}`},
		{path: []string{"software", "delete"}, args: []string{"base"}, want: "DELETE /api/v1/organizations/acme/software-policies/base"},
		{path: []string{"org", "account", "disable"}, args: []string{"ann@acme.test"}, want: "PUT /api/v1/organizations/acme/accounts/ann@acme.test/disable"},
		{path: []string{"auth", "token", "revoke"}, args: []string{"ci"}, want: "DELETE /api/v1/auth/tokens/ci"},
		{path: []string{"device", "approve-all"},
			gets: map[string]string{"/api/v1/organizations/acme/devices": `{"items":[{"name":"fw01","status":"PENDING"}],"total":1}`},
			want: "POST /api/v1/organizations/acme/devices/fw01/approve"},
		{path: []string{"backup", "config", "set"}, flags: map[string]string{"no-schedule": "true"},
			want: "PUT /api/v1/organizations/acme/backup-config/schedule"},
		{path: []string{"network", "delete"}, args: []string{"corp"}, want: "DELETE /api/v1/organizations/acme/vpn-networks/corp"},
		{path: []string{"network", "member", "remove"}, args: []string{"corp", "fw01"},
			want: "DELETE /api/v1/organizations/acme/vpn-networks/corp/members/fw01"},
		{path: []string{"network", "prefix", "remove"}, args: []string{"corp", "fw01", "lan"},
			want: "DELETE /api/v1/organizations/acme/vpn-networks/corp/members/fw01/prefixes/lan"},
		{path: []string{"variable", "org", "delete"}, args: []string{"dns"}, want: "DELETE /api/v1/organizations/acme/variables/dns"},
		{path: []string{"sync", "apply"},
			gets: map[string]string{"/api/v1/sync/status": `{"items":[{"device_name":"fw01"}],"total":1}`},
			want: "POST /api/v1/sync"},
		{path: []string{"network", "link", "delete"}, args: []string{"corp", "fw01", "fw02"},
			want: "DELETE /api/v1/organizations/acme/vpn-networks/corp/links/fw01/fw02"},
		{path: []string{"ou", "remove-device"}, args: []string{"branch", "fw01"},
			want: "DELETE /api/v1/organizations/acme/ous/branch/devices/fw01"},
		{path: []string{"ou", "template", "remove"}, args: []string{"branch", "base"},
			want: "DELETE /api/v1/organizations/acme/ous/branch/templates/base"},
		{path: []string{"template", "remove-snippet"}, args: []string{"base", "dns"},
			want: "DELETE /api/v1/organizations/acme/templates/base/snippets/dns"},
		{path: []string{"template", "remove-software"}, args: []string{"base", "tools"},
			want: "DELETE /api/v1/organizations/acme/templates/base/software-policies/tools"},
		{path: []string{"software", "remove-repo"}, args: []string{"tools", "extra"},
			gets: map[string]string{"/api/v1/organizations/acme/software-policies/tools": softwarePolicyWithEntries},
			want: "PUT /api/v1/organizations/acme/software-policies/tools/content"},
		{path: []string{"software", "remove-external"}, args: []string{"tools", "agent"},
			gets: map[string]string{"/api/v1/organizations/acme/software-policies/tools": softwarePolicyWithEntries},
			want: "PUT /api/v1/organizations/acme/software-policies/tools/content"},
		{path: []string{"backup", "encryption-key", "remove"}, args: []string{"fw01"},
			want: "DELETE /api/v1/organizations/acme/devices/fw01/backup/encryption-key"},
		{path: []string{"org", "invite", "revoke"}, args: []string{"ann@acme.test"},
			want: "DELETE /api/v1/organizations/acme/invites/ann@acme.test"},
		{path: []string{"config", "reset"}, local: true},
		{path: []string{"network", "update"}, args: []string{"corp"}, flags: map[string]string{"auto-connect-hubs": "true"},
			gets: map[string]string{
				"/api/v1/organizations/acme/vpn-networks/corp":         `{"name":"corp","auto_connect_hubs":false}`,
				"/api/v1/organizations/acme/vpn-networks/corp/members": `{"items":[{"device_name":"hub1","role":"HUB"},{"device_name":"hub2","role":"HUB"}],"total":2}`,
			},
			want: "PATCH /api/v1/organizations/acme/vpn-networks/corp"},
	}
}

// runPromptCase runs tc's command once with stdin holding input and no
// terminal, after setting yes when asked to, and returns the writes it sent,
// its stdout and stderr and the error it returned.
func runPromptCase(t *testing.T, tc promptCase, yes bool, input string) (writes, stdout, stderr string, err error) {
	t.Helper()
	if tc.local {
		if yes || strings.HasPrefix(input, "y") {
			t.Fatalf("%s writes local state: only a run that must not write is made", strings.Join(tc.path, " "))
		}
		// Should the refusal ever fail to stop it, the write lands here,
		// not in the developer's own configuration. That holds only while no
		// config file is loaded: CreateDefaultConfig writes to a loaded one.
		if path := config.GetConfigFilePath(); path != "" {
			t.Fatalf("a config file is loaded (%s), so %s could write it", path, strings.Join(tc.path, " "))
		}
		// Cleanups run last first: the variable is restored, then re-read.
		t.Cleanup(xdg.Reload)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		xdg.Reload()
	}
	cmd := findCommand(t, tc.path...)
	flags := map[string]string{}
	for k, v := range tc.flags {
		flags[k] = v
	}
	if yes {
		flags["yes"] = "true"
	}
	defer setFlags(t, cmd, flags)()

	var mu sync.Mutex
	var sent []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			body, ok := tc.gets[r.URL.Path]
			if !ok {
				body = `{}`
			}
			_, _ = w.Write([]byte(body))
			return
		}
		mu.Lock()
		sent = append(sent, r.Method+" "+r.URL.Path)
		mu.Unlock()
		answer := tc.answer
		if answer == "" {
			answer = `{}`
		}
		_, _ = w.Write([]byte(answer))
	}
	stdout, stderr = runStreams(t, handler, "table", input, func() error {
		err = cmd.RunE(cmd, tc.args)
		return nil
	})
	mu.Lock()
	defer mu.Unlock()
	return strings.Join(sent, ", "), stdout, stderr, err
}

// setFlags sets each flag on cmd and returns the func that puts them back,
// value and Changed: the commands are package globals shared by every test.
func setFlags(t *testing.T, cmd *cobra.Command, flags map[string]string) (reset func()) {
	t.Helper()
	var resets []func()
	reset = func() {
		for _, r := range resets {
			r()
		}
	}
	for name, value := range flags {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			reset()
			t.Fatalf("%s has no --%s", cmd.CommandPath(), name)
		}
		def := f.DefValue
		resets = append(resets, func() {
			_ = f.Value.Set(def)
			f.Changed = false
		})
		if err := cmd.Flags().Set(name, value); err != nil {
			reset()
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	return reset
}

// Every command that asks can skip its prompt with -y/--yes, so a script or
// test runner does not have to pipe "y" into it (#329). Without the flag a
// piped answer still decides.
func TestPromptingCommandsSkipThePromptWithYes(t *testing.T) {
	for _, tc := range promptCases() {
		name := strings.Join(tc.path, " ")
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(t, tc.path...)
			if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
				t.Fatalf("%s has no -y/--yes", name)
			}
			if !tc.local {
				confirmed(t, tc)
			}
			got, stdout, stderr, err := runPromptCase(t, tc, false, "n\n")
			if err != nil || got != "" || stdout != "" || !strings.HasSuffix(stderr, "Cancelled\n") {
				t.Errorf("answered n: err %v, sent %q, stdout %q, stderr %q; want nothing sent and Cancelled", err, got, stdout, stderr)
			}
		})
	}
}

// confirmed checks that tc sends its write with --yes and with a piped y.
func confirmed(t *testing.T, tc promptCase) {
	t.Helper()
	if got, _, _, err := runPromptCase(t, tc, true, ""); err != nil || got != tc.want {
		t.Errorf("with --yes: requests = %q (err %v), want %q", got, err, tc.want)
	}
	if got, _, _, err := runPromptCase(t, tc, false, "y\n"); err != nil || got != tc.want {
		t.Errorf("answered y on a pipe: requests = %q (err %v), want %q", got, err, tc.want)
	}
}

// A prompt nobody can answer, on a stdin that is not a terminal and ends
// before an answer, fails the command and names the flag: before, it printed
// Cancelled and exited 0, so a script that forgot -y did nothing and went on.
func TestAnUnansweredPromptFailsNamingTheFlag(t *testing.T) {
	for _, tc := range promptCases() {
		name := strings.Join(tc.path, " ")
		t.Run(name, func(t *testing.T) {
			got, stdout, stderr, err := runPromptCase(t, tc, false, "")
			if err == nil || !strings.Contains(err.Error(), "pass -y to confirm") {
				t.Errorf("err = %v, want the refusal naming -y", err)
			}
			if got != "" {
				t.Errorf("sent %q, want nothing", got)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want it empty", stdout)
			}
			if strings.Contains(stderr, "Cancelled") {
				t.Errorf("stderr = %q: an unanswered prompt is not a cancel", stderr)
			}
		})
	}
	// auth delete asks for DELETE typed out and has no -y.
	t.Run("auth delete", func(t *testing.T) {
		cmd := findCommand(t, "auth", "delete")
		var err error
		stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("an unanswered delete must not reach the server: %s %s", r.Method, r.URL.Path)
		}, "table", "", func() error {
			svc = service.NewFromProvider(api.NewClient("http://127.0.0.1:1", false, &noopAuthProvider{}), userStub{}, nil)
			err = runAuthDelete(cmd, nil)
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "pass --yes to confirm") {
			t.Errorf("err = %v, want the refusal naming --yes", err)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want it empty", stdout)
		}
	})
}

// Every delete, revoke, remove and clear asks first, or refuses without
// --force: a new one added without -y/--yes or --force fails here, whatever
// promptCases lists. config reset is held to the same rule.
func TestEveryRemovingCommandHasAPromptFlag(t *testing.T) {
	prepareRootCommand()
	checked := 0
	for _, cmd := range walkCommands(rootCmd) {
		name := cmd.Name()
		removes := name == "delete" || name == "revoke" || name == "clear" || strings.HasPrefix(name, "remove") ||
			cmd.CommandPath() == "ndcli config reset"
		if !removes || cmd.HasSubCommands() {
			continue
		}
		checked++
		if cmd.Flags().Lookup("yes") == nil && cmd.Flags().Lookup("force") == nil {
			t.Errorf("%s has neither -y/--yes nor --force", cmd.CommandPath())
		}
	}
	if checked < 25 {
		t.Errorf("checked only %d commands: the walk is not reaching the tree", checked)
	}
}
