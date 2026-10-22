package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// runOrgDeprecation is the line cobra prints on stderr when a run command is
// given the boolean --org, written out so the test pins the text itself.
const runOrgDeprecation = "Flag --org has been deprecated, use --all; in the next major version " +
	"--org <name> selects the organization, as on other commands\n"

var runCommandNames = []string{"ping", "poweroff", "restart", "plugin-install", "plugin-reload", "firmware-upgrade", "backup"}

// Every run command targets the whole organization with --all. The boolean
// --org it replaces still works, deprecated and out of the help, and -o names
// the organization as it does on other commands.
func TestRunCommandsOfferAllAndOrganization(t *testing.T) {
	for _, name := range runCommandNames {
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(t, "run", name)
			if cmd.Name() != name {
				t.Fatalf("found %q", cmd.Name())
			}

			if all := cmd.Flags().Lookup("all"); all == nil || all.Value.Type() != "bool" || all.Hidden {
				t.Errorf("--all = %+v, want a visible boolean", all)
			}
			org := cmd.Flags().Lookup("org")
			if org == nil || org.Value.Type() != "bool" || !org.Hidden {
				t.Fatalf("--org = %+v, want a hidden boolean", org)
			}
			if got := "Flag --org has been deprecated, " + org.Deprecated + "\n"; got != runOrgDeprecation {
				t.Errorf("deprecation line = %q\nwant              %q", got, runOrgDeprecation)
			}
			if o := cmd.Flags().ShorthandLookup("o"); o == nil || o.Name != "organization" || o.Value.Type() != "string" || o.Hidden {
				t.Errorf("-o = %+v, want the visible string flag --organization", o)
			}

			usage := cmd.Flags().FlagUsages()
			for _, want := range []string{"--all ", "-o, --organization string"} {
				if !strings.Contains(usage, want) {
					t.Errorf("help lacks %q:\n%s", want, usage)
				}
			}
			if strings.Contains(usage, "--org ") {
				t.Errorf("help shows the deprecated --org:\n%s", usage)
			}
		})
	}
}

func TestRunBackupDeprecatedOrgStillTargetsEveryDevice(t *testing.T) {
	got := runBackup(t, "json", http.StatusCreated, runBackupFixture, "--org")

	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.body != `{"targets":{"all":true}}` {
		t.Errorf("body = %s", got.body)
	}
	var result models.BackupRunResult
	decodeOne(t, got.stdout, &result)
	if !strings.Contains(got.stderr, runOrgDeprecation) {
		t.Errorf("stderr lacks the deprecation line:\n%s", got.stderr)
	}
}

type recordedRequest struct {
	method, path string
	body         map[string]any
}

// runRecorded runs ndcli in a child process, through the root command, against
// a fake control plane that answers every request with status and response
// and records it. The configured organization is acme.
func runRecorded(t *testing.T, status int, response string, args ...string) (requests []recordedRequest, stdout, stderr string, code int) {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := recordedRequest{method: r.Method, path: r.URL.EscapedPath()}
		_ = json.Unmarshal(raw, &req.body)
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	defer srv.Close()

	home := t.TempDir()
	conf := filepath.Join(home, "config.yaml")
	config := "controlplane:\n  host: " + srv.URL + "\n  ssl_verify: true\norganization:\n  name: acme\n"
	if err := os.WriteFile(conf, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	stdout, stderr, code = runNdcli(t, home, "", append([]string{"--conf", conf}, args...)...)
	mu.Lock()
	defer mu.Unlock()
	return requests, stdout, stderr, code
}

const runRestartResponse = `{"type":"REBOOT","organization":"acme","total":1,"tasks":[{"task":"T1","device_name":"fw1","status":"PENDING"}]}`

func TestRunOrganizationFlag(t *testing.T) {
	for _, tc := range []struct {
		name, response, path string
		args                 []string
	}{
		{"-o on the run command", runRestartResponse, "/api/v1/organizations/other-org/tasks",
			[]string{"run", "restart", "--device", "fw1", "-o", "other-org"}},
		{"-o before the run command", runRestartResponse, "/api/v1/organizations/other-org/tasks",
			[]string{"-o", "other-org", "run", "restart", "--device", "fw1"}},
		{"--organization", runRestartResponse, "/api/v1/organizations/other-org/tasks",
			[]string{"run", "restart", "--device", "fw1", "--organization", "other-org"}},
		{"no -o: the configured organization", runRestartResponse, "/api/v1/organizations/acme/tasks",
			[]string{"run", "restart", "--device", "fw1"}},
		{"run backup", runBackupFixture, "/api/v1/organizations/other-org/backups/run",
			[]string{"run", "backup", "--all", "-o", "other-org"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests, _, stderr, code := runRecorded(t, http.StatusCreated, tc.response, append([]string{"-f", "json"}, tc.args...)...)

			if code != 0 {
				t.Fatalf("exit %d\nstderr: %s", code, stderr)
			}
			if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].path != tc.path {
				t.Errorf("requests = %+v, want one POST %s", requests, tc.path)
			}
		})
	}
}

// --all and the deprecated boolean --org send the same request. Only --org
// warns, on stderr, so -f json still prints one document on stdout.
func TestRunAllAndDeprecatedOrg(t *testing.T) {
	wantTargets := map[string]any{"devices": []any{}, "ous": []any{}, "all": true}
	for _, flag := range []string{"--all", "--org"} {
		t.Run(flag, func(t *testing.T) {
			requests, stdout, stderr, code := runRecorded(t, http.StatusCreated, runRestartResponse, "-f", "json", "run", "restart", flag)

			if code != 0 {
				t.Fatalf("exit %d\nstderr: %s", code, stderr)
			}
			if len(requests) != 1 || requests[0].path != "/api/v1/organizations/acme/tasks" {
				t.Fatalf("requests = %+v", requests)
			}
			if !reflect.DeepEqual(requests[0].body["targets"], wantTargets) {
				t.Errorf("targets = %v, want %v", requests[0].body["targets"], wantTargets)
			}
			var result models.RunResult
			decodeOne(t, stdout, &result)
			if strings.Contains(stdout, "deprecated") {
				t.Errorf("the deprecation reached stdout:\n%s", stdout)
			}
			wantWarnings := 0
			if flag == "--org" {
				wantWarnings = 1
			}
			if got := strings.Count(stderr, runOrgDeprecation); got != wantWarnings {
				t.Errorf("stderr has %d deprecation line(s), want %d:\n%s", got, wantWarnings, stderr)
			}
		})
	}
}

// --all, or the deprecated --org, with --device or --ou is refused in the
// command's own words (the MCP tools use the same), through the root command,
// before anything is sent.
func TestRunAllWithNamedTargetsIsRefused(t *testing.T) {
	const combined = "Error: --all cannot be combined with --device or --ou\n"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"--all with --device", []string{"run", "restart", "--all", "--device", "fw1"}, combined},
		{"--all with --ou", []string{"run", "restart", "--all", "--ou", "edge"}, combined},
		{"deprecated --org with --device", []string{"run", "restart", "--org", "--device", "fw1"}, combined},
		{"run backup --all with --device", []string{"run", "backup", "--all", "--device", "fw1"}, combined},
		{"no target", []string{"run", "restart"}, "Error: at least one of --device, --ou, or --all is required\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests, stdout, stderr, code := runRecorded(t, http.StatusCreated, runRestartResponse, tc.args...)

			if code != 1 {
				t.Fatalf("exit %d, want 1\nstderr: %s", code, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr lacks %q:\n%s", tc.want, stderr)
			}
			if stdout != "" || len(requests) != 0 {
				t.Errorf("stdout %q, %d request(s); want neither", stdout, len(requests))
			}
		})
	}
}

// Without an organization every command says how to give one. "Use --org"
// would be wrong advice on the run commands, where --org is the deprecated
// boolean; -o works on every command.
func TestOrganizationRequiredSaysHowToGiveOne(t *testing.T) {
	home := t.TempDir()
	conf := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(conf, []byte("controlplane:\n  host: http://127.0.0.1:9\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	const want = "Error: Organization is required. Use -o <name>, or set a default with 'ndcli config set org <name>'.\n"
	for _, args := range [][]string{{"run", "restart", "--device", "fw1"}, {"device", "list"}} {
		_, stderr, code := runNdcli(t, home, "", append([]string{"--conf", conf}, args...)...)
		if code != 1 || !strings.Contains(stderr, want) {
			t.Errorf("%v: exit %d, stderr:\n%s\nwant %q", args, code, stderr, want)
		}
	}
}
