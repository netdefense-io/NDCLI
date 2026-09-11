package config

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUnboundEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "an unbound NDCLI_ variable is reported",
			environ: []string{"NDCLI_OAUTH2_DOMAIN=evil.example.com"},
			want:    []string{"NDCLI_OAUTH2_DOMAIN"},
		},
		{
			name: "bound config keys are not reported",
			environ: []string{
				"NDCLI_CONTROLPLANE_HOST=https://example.test",
				"NDCLI_AUTH_STORAGE=file",
				"NDCLI_UPDATE_CHECK_ENABLED=false",
			},
			want: nil,
		},
		{
			// NDCLI_TOKEN is the whole static-PAT path. Warning that it does
			// nothing would be both wrong and alarming.
			name: "variables read directly rather than through viper are not reported",
			environ: []string{
				"NDCLI_TOKEN=ndpat_example",
				"NDCLI_NO_UPDATE_CHECK=1",
				"NDCLI_IGNORE_MIN_VERSION=1",
				"NDCLI_TUI_REFRESH=10s",
				"NDCLI_KEYRING_LIVE_TEST=1",
			},
			want: nil,
		},
		{
			// NDCLI_DEBUG drives the connect-session teardown goroutine dump
			// and is read with os.Getenv. It is a different variable and a
			// different feature from NDCLI_DEBUG_ENABLED (debug.enabled), and
			// conflating the two is how it first got reported as inert.
			name:    "NDCLI_DEBUG works and must not be reported",
			environ: []string{"NDCLI_DEBUG=1"},
			want:    nil,
		},
		{
			name:    "variables without the prefix are ignored",
			environ: []string{"PATH=/usr/bin", "HOME=/home/someone", "EDITOR=vi"},
			want:    nil,
		},
		{
			// viper does not treat an empty value as an override either
			// (AllowEmptyEnv is off), so it is inert rather than ignored.
			name:    "an empty value is not reported",
			environ: []string{"NDCLI_OAUTH2_DOMAIN="},
			want:    nil,
		},
		{
			name: "results are sorted",
			environ: []string{
				"NDCLI_ZZZ=1",
				"NDCLI_AAA=1",
				"NDCLI_TOKEN=ndpat_example",
			},
			want: []string{"NDCLI_AAA", "NDCLI_ZZZ"},
		},
		{
			// os.Environ can return the same name twice; naming it twice in
			// one warning would read like two separate problems.
			name: "a repeated name is reported once",
			environ: []string{
				"NDCLI_ZZZ=1",
				"NDCLI_ZZZ=2",
				"NDCLI_AAA=1",
				"NDCLI_AAA=1",
			},
			want: []string{"NDCLI_AAA", "NDCLI_ZZZ"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnboundEnvVars(tt.environ)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}

func TestWarnUnboundEnvVars(t *testing.T) {
	t.Run("says nothing when everything set is understood", func(t *testing.T) {
		var buf bytes.Buffer
		WarnUnboundEnvVars([]string{"NDCLI_TOKEN=ndpat_example", "PATH=/usr/bin"}, &buf)
		if buf.Len() != 0 {
			t.Errorf("expected silence, got: %q", buf.String())
		}
	})

	t.Run("names the variable and what ndcli does read", func(t *testing.T) {
		var buf bytes.Buffer
		WarnUnboundEnvVars([]string{"NDCLI_OAUTH2_DOMAIN=evil.example.com"}, &buf)

		out := buf.String()
		for _, want := range []string{
			"NDCLI_OAUTH2_DOMAIN",
			"no effect",
			"NDCLI_AUTH_STORAGE",
			"config-file only",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("warning missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "variables are") {
			t.Errorf("a single variable should read in the singular:\n%s", out)
		}
	})

	t.Run("reads in the plural for more than one", func(t *testing.T) {
		var buf bytes.Buffer
		WarnUnboundEnvVars([]string{"NDCLI_AAA=1", "NDCLI_BBB=2"}, &buf)
		if !strings.Contains(buf.String(), "variables are set but have no effect") {
			t.Errorf("expected plural phrasing:\n%s", buf.String())
		}
	})
}

// TestEnvBindingsAndDirectReadersAreDisjoint guards the single-source-of-truth
// claim: a name in both maps would mean two different mechanisms believe they
// own the same variable.
func TestEnvBindingsAndDirectReadersAreDisjoint(t *testing.T) {
	for key, env := range envBindings {
		if _, clash := envDirectReaders[env]; clash {
			t.Errorf("%s is bound to config key %q and also listed as a direct reader", env, key)
		}
		if !strings.HasPrefix(env, envPrefix) {
			t.Errorf("binding for %q uses %q, which lacks the %s prefix", key, env, envPrefix)
		}
	}
	for env := range envDirectReaders {
		if !strings.HasPrefix(env, envPrefix) {
			t.Errorf("direct reader %q lacks the %s prefix", env, envPrefix)
		}
	}
}

// TestOAuth2KeysStayUnbound is a policy test, not a mechanism test. An
// environment override for the OAuth2 domain would be a second, easily-missed
// way to influence the check in validateOAuth2Domain that exists to stop a
// rogue control plane redirecting the device-code flow. Removing this test
// should take a deliberate argument, not a refactor.
func TestOAuth2KeysStayUnbound(t *testing.T) {
	for key := range envBindings {
		if strings.HasPrefix(key, "oauth2.") {
			t.Errorf("oauth2 key %q must not have an environment binding", key)
		}
	}
}

// TestEveryDirectEnvReadIsKnown sweeps the repository for direct
// os.Getenv("NDCLI_...") reads and fails if any of them is missing from the
// known set. Hand-maintaining envDirectReaders is exactly how NDCLI_DEBUG came
// to be reported as inert while the teardown watchdog was reading it, so the
// list is checked against the source rather than trusted.
//
// Constant-named reads (os.Getenv(EnvToken)) are not matched by the literal
// scan; those are covered by the table above.
func TestEveryDirectEnvReadIsKnown(t *testing.T) {
	root := filepath.Join("..", "..")
	known := knownEnvVars()
	pattern := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("(NDCLI_[A-Z0-9_]+)"\)`)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "bin" || name == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range pattern.FindAllStringSubmatch(string(src), -1) {
			name := m[1]
			if !known[name] {
				t.Errorf("%s reads %s directly, but it is in neither envDirectReaders nor envTestOnly — a user who sets it would be told it has no effect", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestTestOnlyVarsAreExemptButUnadvertised: a developer setting one must not
// be warned, and a user must not be told about a variable that only a test
// file reads.
func TestTestOnlyVarsAreExemptButUnadvertised(t *testing.T) {
	advertised := make(map[string]bool)
	for _, name := range boundEnvVarNames() {
		advertised[name] = true
	}

	for env := range envTestOnly {
		if advertised[env] {
			t.Errorf("%s is test-only and should not be offered to users", env)
		}
		if got := UnboundEnvVars([]string{env + "=1"}); len(got) != 0 {
			t.Errorf("%s should be exempt from the warning, got %v", env, got)
		}
	}
}
