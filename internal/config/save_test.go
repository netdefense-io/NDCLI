package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// prepareSaveTest points the package at a throwaway config file holding the
// given YAML, with a fresh viper carrying the compiled defaults.
func prepareSaveTest(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origPath := configFilePath
	configFilePath = path

	// UpdateValue re-unmarshals into cfg after saving, and in production Load
	// has always populated it first.
	origCfg := cfg
	cfg = &Config{}

	viper.Reset()
	setDefaults()

	explicitMu.Lock()
	origExplicit := explicitlySet
	explicitlySet = map[string]bool{}
	explicitMu.Unlock()

	t.Cleanup(func() {
		configFilePath = origPath
		cfg = origCfg
		viper.Reset()
		explicitMu.Lock()
		explicitlySet = origExplicit
		explicitMu.Unlock()
	})

	return path
}

// TestSaveKeepsAnAuthAccountItNeverTouched is the regression guard for the
// interaction between the new auth.* defaults and Save's "explicitly cleared"
// branch.
//
// auth.account needs a default so its environment binding survives
// viper.Unmarshal. But viper.IsSet returns true as soon as any source —
// defaults included — provides a value, so the branch that deletes a cleared
// account would fire on every Save with an empty account and remove an
// on-disk account this process never touched.
func TestSaveKeepsAnAuthAccountItNeverTouched(t *testing.T) {
	path := prepareSaveTest(t, "auth:\n  account: someone@example.test\n  storage: file\n")

	if err := Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), "someone@example.test") {
		t.Errorf("Save deleted an auth.account it was never asked to touch:\n%s", got)
	}
}

// TestSaveRemovesAnExplicitlyClearedAccount is the other half: logout clears
// the account through UpdateValue, and that must still take it out of the file.
func TestSaveRemovesAnExplicitlyClearedAccount(t *testing.T) {
	path := prepareSaveTest(t, "auth:\n  account: someone@example.test\n")

	// What KeyringStorage.Clear does on logout.
	viper.Set("auth.account", "")
	markExplicitlySet("auth.account")

	if err := Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(got), "someone@example.test") {
		t.Errorf("an explicitly cleared account should have been removed:\n%s", got)
	}
}

func TestWasExplicitlySet(t *testing.T) {
	prepareSaveTest(t, "")

	if wasExplicitlySet("auth.account") {
		t.Error("nothing has been set yet")
	}
	// A default alone must not count as explicitly set — that is exactly the
	// distinction viper.IsSet could not make.
	if viper.GetString("auth.account") != "" {
		t.Fatal("expected the empty default")
	}
	if wasExplicitlySet("auth.account") {
		t.Error("a compiled default is not an explicit assignment")
	}

	markExplicitlySet("auth.account")
	if !wasExplicitlySet("auth.account") {
		t.Error("expected the key to be recorded after marking")
	}
}

// TestSaveDoesNotPersistEnvSourcedAuthFields is the guard for the sharpest
// consequence of giving auth.* environment bindings.
//
// Save runs from every UpdateValue — `config set output.timezone`, an org
// change, login and logout. If it wrote the *resolved* auth.storage, then
//
//	NDCLI_AUTH_STORAGE=file ndcli config set output.timezone America/New_York
//
// would bake `auth: {storage: file}` into config.yaml permanently. The user
// unsets the variable and credentials keep going to plaintext on disk instead
// of the keyring, silently, forever — a credential-storage downgrade caused by
// a command with nothing to do with authentication.
//
// An environment variable is a session override, not a persisted setting.
func TestSaveDoesNotPersistEnvSourcedAuthFields(t *testing.T) {
	t.Setenv("NDCLI_AUTH_STORAGE", "file")
	t.Setenv("NDCLI_AUTH_PATH", "/tmp/somewhere/auth.json")
	t.Setenv("NDCLI_AUTH_ACCOUNT", "someone@example.test")

	path := prepareSaveTest(t, "output:\n  timezone: UTC\n")

	// The override really is live for this process; the point is that it must
	// not outlive it.
	if got := viper.GetString("auth.storage"); got != "file" {
		t.Fatalf("expected the environment override to resolve, got %q", got)
	}

	// An unrelated setting changes, exactly as `config set` does.
	if err := UpdateValue("output.timezone", "America/New_York"); err != nil {
		t.Fatalf("UpdateValue: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "America/New_York") {
		t.Errorf("the setting the user actually changed was not written:\n%s", got)
	}
	for _, leaked := range []string{"storage", "file", "/tmp/somewhere/auth.json", "someone@example.test"} {
		if strings.Contains(got, leaked) {
			t.Errorf("an environment-sourced auth value was persisted (%q):\n%s", leaked, got)
		}
	}
	if strings.Contains(got, "auth:") {
		t.Errorf("no auth section should have been created at all:\n%s", got)
	}
}

// TestSavePersistsExplicitlySetAuthFields is the other half: a value this
// process really did assign still has to reach the file, or logging in would
// stop recording the account.
func TestSavePersistsExplicitlySetAuthFields(t *testing.T) {
	t.Setenv("NDCLI_AUTH_STORAGE", "file")

	path := prepareSaveTest(t, "")

	if err := UpdateValue("auth.account", "someone@example.test"); err != nil {
		t.Fatalf("UpdateValue: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "someone@example.test") {
		t.Errorf("an explicitly assigned account must be written:\n%s", got)
	}
	// The env-sourced sibling still must not ride along.
	if strings.Contains(got, "storage") {
		t.Errorf("auth.storage came from the environment and must not be written:\n%s", got)
	}
}

// TestSaveLeavesAnOnDiskAuthStorageAlone: a value already in the file stays
// put, and is not rewritten to whatever the environment happens to say.
func TestSaveLeavesAnOnDiskAuthStorageAlone(t *testing.T) {
	t.Setenv("NDCLI_AUTH_STORAGE", "file")

	path := prepareSaveTest(t, "auth:\n  storage: keyring\n")

	if err := UpdateValue("output.timezone", "UTC"); err != nil {
		t.Fatalf("UpdateValue: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "storage: keyring") {
		t.Errorf("the on-disk choice should be untouched:\n%s", got)
	}
	if strings.Contains(got, "storage: file") {
		t.Errorf("the environment override overwrote the file's own value:\n%s", got)
	}
}
