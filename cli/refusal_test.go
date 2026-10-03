package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// A refusal a command explains in its own words is still an error: it must
// return non-nil, so the command exits 1, leave stdout empty, and render
// under the same [CODE] and JSON rules as every other error. Each of them
// used to print its explanation and return nil, which exits 0 for something
// that did not happen.

const (
	registrationRestrictedBody = `{"error":"Registration is restricted","code":"REGISTRATION_RESTRICTED"}`

	ouDeleteBlockedBody = `{"error":"Cannot delete OU with active devices. Remove these devices from OU before deletion:",` +
		`"code":"HAS_DEPENDENCIES","blocking_resources":["fw01","fw02"],"count":2}`
	ouDeleteBlockedMessage = "Cannot delete OU 'branch' - 2 active device(s) must be removed first:\n  • fw01\n  • fw02"

	soleSuperuserError = "Cannot delete account - you are the only superuser for these organizations. " +
		"Transfer ownership or delete the organization(s) first."
	authDeleteBlockedBody = `{"error":"` + soleSuperuserError + `","code":"HAS_DEPENDENCIES",` +
		`"blocking_resources":["acme","globex"],"count":2}`
	authDeleteBlockedMessage = soleSuperuserError +
		"\nOrganizations where you are the only superuser:\n  • acme\n  • globex"
)

// assertRefusal checks what a refused command returned and printed: an error,
// nothing on stdout, and the coded error in text and JSON.
func assertRefusal(t *testing.T, err error, stdout, code, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("a refusal must return an error, so the command exits non-zero")
	}
	if stdout != "" {
		t.Errorf("nothing belongs on stdout, got %q", stdout)
	}
	useFormatter(t, output.NewTableFormatter(), "")
	if got, want := printed(err), "Error: ["+code+"] "+message+"\n"; got != want {
		t.Errorf("text:\ngot  %q\nwant %q", got, want)
	}
	useFormatter(t, output.NewJSONFormatter(), "json")
	if got := decodeCommandError(t, printed(err)); got["code"] != code || got["message"] != message {
		t.Errorf("json: got %v", got)
	}
}

func TestOrgCreateRegistrationRestrictedIsAnError(t *testing.T) {
	answerAll(t, http.StatusForbidden, registrationRestrictedBody)

	var err error
	out := captureStdout(t, func() {
		err = runOrgCreate(orgCreateCmd, []string{"acme-new"})
	})
	assertRefusal(t, err, out, "REGISTRATION_RESTRICTED", registrationRestrictedMessage)
}

func TestOUDeleteBlockedByDevicesIsAnError(t *testing.T) {
	cmd := findCommand(t, "ou", "delete")
	var err error
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, ouDeleteBlockedBody)
	}, "table", "y\n", func() error {
		err = runOUDelete(cmd, []string{"branch"})
		return nil
	})
	assertRefusal(t, err, stdout, "HAS_DEPENDENCIES", ouDeleteBlockedMessage)
}

// TestAuthDeleteBlockedBySoleSuperuserIsAnError asserts the exit at the
// function level. A child process cannot reach this refusal: under
// NDCLI_TOKEN, auth delete stops earlier for want of cached user info. The
// child-process tests below prove that a returned error exits 1.
func TestAuthDeleteBlockedBySoleSuperuserIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/auth/me" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, authDeleteBlockedBody)
	}))
	defer srv.Close()

	cmd := findCommand(t, "auth", "delete")
	var err error
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}, "table", "DELETE\n", func() error {
		// auth delete reads the signed-in user first, which needs an auth
		// manager; the service runStreams builds has none.
		svc = service.NewFromProvider(api.NewClient(srv.URL, false, &noopAuthProvider{}), userStub{}, nil)
		err = runAuthDelete(cmd, nil)
		return nil
	})
	assertRefusal(t, err, stdout, "HAS_DEPENDENCIES", authDeleteBlockedMessage)
}

// TestRefusalsExitOne runs ndcli in a child process, through the real
// PersistentPreRunE, Execute and main's exit, against a fake NDManager that
// refuses. Exit code 1 is what a script checks.
func TestRefusalsExitOne(t *testing.T) {
	for _, tc := range []struct {
		name, stdin, body, code, message string
		status                           int
		args                             []string
	}{
		{"org create", "", registrationRestrictedBody, "REGISTRATION_RESTRICTED", registrationRestrictedMessage,
			http.StatusForbidden, []string{"org", "create", "acme-new"}},
		{"ou delete", "y\n", ouDeleteBlockedBody, "HAS_DEPENDENCIES", ouDeleteBlockedMessage,
			http.StatusConflict, []string{"-o", "acme", "ou", "delete", "branch"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		home := t.TempDir()
		conf := filepath.Join(home, "config.yaml")
		if err := os.WriteFile(conf, []byte("controlplane:\n  host: "+srv.URL+"\n  ssl_verify: true\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		for _, format := range []string{"table", "json"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				args := append([]string{"--conf", conf, "-f", format}, tc.args...)
				stdout, stderr, code := runNdcli(t, home, tc.stdin, args...)
				if code != 1 {
					t.Fatalf("exit code %d, want 1\nstderr: %s", code, stderr)
				}
				if stdout != "" {
					t.Errorf("nothing belongs on stdout, got %q", stdout)
				}
				if format == "table" {
					if want := "Error: [" + tc.code + "] " + tc.message + "\n"; !strings.Contains(stderr, want) {
						t.Errorf("stderr missing %q:\n%s", want, stderr)
					}
					return
				}
				start := strings.Index(stderr, "{")
				if start < 0 {
					t.Fatalf("no JSON error on stderr:\n%s", stderr)
				}
				var doc map[string]map[string]string
				if err := json.NewDecoder(strings.NewReader(stderr[start:])).Decode(&doc); err != nil {
					t.Fatalf("stderr does not hold a JSON error: %v\n%s", err, stderr)
				}
				if doc["error"]["code"] != tc.code || doc["error"]["message"] != tc.message {
					t.Errorf("json error: got %v", doc["error"])
				}
			})
		}
		srv.Close()
	}
}

// runNdcli runs this test binary as ndcli with args and stdin, and returns its
// stdout, stderr and exit code. Inherited NDCLI_ variables are dropped so the
// operator's own settings cannot reach the child.
func runNdcli(t *testing.T, home, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelperProcess$")
	cmd.Stdin = strings.NewReader(stdin)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NDCLI_") && !strings.HasPrefix(kv, "HOME=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	cmd.Env = append(cmd.Env,
		"GO_WANT_HELPER_PROCESS=1",
		"HELPER_ARGS="+string(encoded),
		"HOME="+home,
		"NDCLI_TOKEN=ndpat_test",
		"NDCLI_NO_UPDATE_CHECK=1",
	)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run ndcli: %v", err)
	}
	return out.String(), errOut.String(), code
}

// TestMainHelperProcess is ndcli's main when runNdcli starts this binary as a
// child; in a normal test run it does nothing.
func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("HELPER_ARGS")), &args); err != nil {
		t.Fatalf("decode HELPER_ARGS: %v", err)
	}
	os.Args = append([]string{"ndcli"}, args...)
	if err := Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
