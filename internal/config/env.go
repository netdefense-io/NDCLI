package config

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// envPrefix is the prefix every ndcli environment variable carries.
const envPrefix = "NDCLI_"

// envBindings maps a config key to the environment variable that overrides it.
//
// This is the single source of truth: bindEnvVars installs every entry into
// viper, and UnboundEnvVars uses the same map to decide whether an
// NDCLI_-prefixed variable means anything. A binding therefore cannot exist in
// one and be missing from the other.
//
// Every key here also needs a viper.SetDefault. viper.Unmarshal only sees an
// environment value for a key it already knows about, which is why auth.*
// stayed empty (issue #202) even where a binding would have existed: the nine
// keys that did work all had defaults, and auth.* had none.
//
// oauth2.* is deliberately absent. Those settings are fetched from NDManager
// at login, and validateOAuth2Domain (internal/auth/manager.go) refuses a
// domain that does not match the locally configured one precisely because a
// rogue control plane could otherwise redirect the device-code flow to a
// phishing domain. An environment override would be a way to talk that check
// into a different answer from a place that is easy to set and easy to miss,
// so the variables stay unbound — and the warning below tells anyone who sets
// one that it does nothing, which is the actual complaint in #202.
var envBindings = map[string]string{
	"auth.account":            "NDCLI_AUTH_ACCOUNT",
	"auth.path":               "NDCLI_AUTH_PATH",
	"auth.storage":            "NDCLI_AUTH_STORAGE",
	"controlplane.host":       "NDCLI_CONTROLPLANE_HOST",
	"controlplane.ssl_verify": "NDCLI_CONTROLPLANE_SSL_VERIFY",
	"debug.enabled":           "NDCLI_DEBUG_ENABLED",
	"debug.log_file":          "NDCLI_DEBUG_LOG_FILE",
	"organization.name":       "NDCLI_ORGANIZATION_NAME",
	"output.format":           "NDCLI_OUTPUT_FORMAT",
	"output.timezone":         "NDCLI_OUTPUT_TIMEZONE",
	"pathfinder.host":         "NDCLI_PATHFINDER_HOST",
	"pathfinder.ssl_verify":   "NDCLI_PATHFINDER_SSL_VERIFY",
	"update.check_enabled":    "NDCLI_UPDATE_CHECK_ENABLED",
}

// envDirectReaders are NDCLI_ variables read straight from the environment
// rather than through viper. They are not config keys, but they are not
// mistakes either, so they must never be warned about — NDCLI_TOKEN above all,
// which is the whole static-PAT authentication path.
//
// They live in packages this one cannot import (importing internal/update or
// internal/tui from internal/config would be a cycle), so the names are
// repeated here with the owner named. Keep them in step.
// The list comes from sweeping the repo for direct reads:
//
//	grep -rn 'os.Getenv("NDCLI_' --include='*.go' .
//
// Run that again when adding one. NDCLI_DEBUG is the trap: it is a different
// variable and a different feature from the viper-bound NDCLI_DEBUG_ENABLED
// (debug.enabled), and conflating the two is how it was missed the first time.
var envDirectReaders = map[string]string{
	"NDCLI_TOKEN":              "static personal access token (internal/auth/static.go)",
	"NDCLI_NO_UPDATE_CHECK":    "skip the version check (internal/update/checker.go)",
	"NDCLI_IGNORE_MIN_VERSION": "ignore the server's minimum version (internal/update/checker.go)",
	"NDCLI_TUI_REFRESH":        "TUI refresh interval (internal/tui/run.go)",
	"NDCLI_DEBUG":              "connect-session teardown goroutine dump (internal/pathfinder/teardown_debug.go)",
}

// envTestOnly are NDCLI_ variables consumed only by test files. They must not
// be warned about — a developer who sets one has not made a mistake — but they
// are not offered to users either, so they stay out of the printed list.
var envTestOnly = map[string]string{
	"NDCLI_KEYRING_LIVE_TEST": "opt in to the live keyring test (internal/storage)",
}

// bindEnvVars installs every binding in envBindings into viper.
func bindEnvVars() {
	for key, env := range envBindings {
		viper.BindEnv(key, env)
	}
}

// boundEnvVarNames returns the NDCLI_ variables worth telling a user about,
// sorted — the viper-bound ones and the directly-read ones, but not the
// test-only ones, which nobody running ndcli has any use for.
func boundEnvVarNames() []string {
	names := make([]string, 0, len(envBindings)+len(envDirectReaders))
	for _, env := range envBindings {
		names = append(names, env)
	}
	for env := range envDirectReaders {
		names = append(names, env)
	}
	sort.Strings(names)
	return names
}

// knownEnvVars is every NDCLI_ variable that must not be warned about — the
// printed set plus the test-only ones.
func knownEnvVars() map[string]bool {
	known := make(map[string]bool, len(envBindings)+len(envDirectReaders)+len(envTestOnly))
	for _, env := range envBindings {
		known[env] = true
	}
	for env := range envDirectReaders {
		known[env] = true
	}
	for env := range envTestOnly {
		known[env] = true
	}
	return known
}

// UnboundEnvVars returns the names of NDCLI_-prefixed variables in environ
// that nothing reads, sorted. environ takes os.Environ()'s "NAME=value" shape.
//
// A variable set to the empty string is not reported: viper does not treat one
// as an override either (AllowEmptyEnv is off), so it is genuinely inert
// rather than ignored, and warning about it would be noise in a shell that
// exports empty placeholders.
func UnboundEnvVars(environ []string) []string {
	known := knownEnvVars()
	// os.Environ can hand back the same name twice; naming it twice in one
	// warning would read like two problems.
	seen := map[string]bool{}

	var unbound []string
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		if !strings.HasPrefix(name, envPrefix) || known[name] || seen[name] {
			continue
		}
		seen[name] = true
		unbound = append(unbound, name)
	}
	sort.Strings(unbound)
	return unbound
}

// WarnUnboundEnvVars reports NDCLI_ variables that are set but do nothing.
//
// The point is that the failure stops being silent: before this, setting
// NDCLI_OAUTH2_DOMAIN or NDCLI_AUTH_STORAGE changed nothing and said nothing,
// and the user had no way to tell that from the value being honoured. It warns
// rather than fails, because erroring would break an existing script over a
// variable that never had any effect in the first place.
//
// Deliberately NOT called from Load: a config loader that prints is a loader
// that prints in the wrong places. Shell completion runs the whole startup
// path on every Tab press, and a multi-line warning there lands in the middle
// of the completion exchange. Each front-end calls this where its own output
// is appropriate instead — see cli/root.go, internal/mcp/server.go and
// internal/tui/run.go.
func WarnUnboundEnvVars(environ []string, w io.Writer) {
	unbound := UnboundEnvVars(environ)
	if len(unbound) == 0 {
		return
	}

	clause := "variable is set but has no effect"
	if len(unbound) > 1 {
		clause = "variables are set but have no effect"
	}
	fmt.Fprintf(w, "Warning: the following NDCLI_ environment %s:\n", clause)
	for _, name := range unbound {
		fmt.Fprintf(w, "    %s\n", name)
	}
	fmt.Fprintf(w, "  ndcli reads: %s\n", strings.Join(boundEnvVarNames(), ", "))
	fmt.Fprintln(w, "  Every other setting is config-file only; see 'ndcli config show' for the file in use.")
}
