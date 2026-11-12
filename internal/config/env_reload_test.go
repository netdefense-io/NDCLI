package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// Tests keep themselves off the production relay with NDCLI_PATHFINDER_HOST
// (relayguard.Run as TestMain in mcp, cli, tui and service). That only works
// if a bound variable survives Load, which rebuilds the config and restores
// the defaults.
func TestAnEnvironmentHostSurvivesLoad(t *testing.T) {
	const relay = "ws://127.0.0.1:1"
	t.Setenv("NDCLI_PATHFINDER_HOST", relay)

	origCfg, origPath, origAuth := cfg, configFilePath, authFilePath
	t.Cleanup(func() {
		cfg, configFilePath, authFilePath = origCfg, origPath, origAuth
		viper.Reset()
	})

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("pathfinder:\n  host: "+DefaultPathfinderHost+"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := Get().Pathfinder.Host; got != relay {
		t.Errorf("pathfinder.host = %q after Load, want the environment's %q over the file and the default", got, relay)
	}
}
