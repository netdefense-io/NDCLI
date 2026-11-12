package mcp

import (
	"os"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// testRelayHost is a loopback address nothing listens on: a test that gets as
// far as dialling the relay fails at once instead of reaching the production
// relay, the config default.
const testRelayHost = "ws://127.0.0.1:1"

// TestMain sets the host through the environment, so it survives a
// config.Load, which restores the defaults (internal/config's
// TestAnEnvironmentHostSurvivesLoad), and reaches a child process that
// inherits the environment. A test that builds cmd.Env from scratch must
// carry NDCLI_PATHFINDER_HOST itself.
//
// Tests point the control plane at their own httptest server, through a
// config file the environment would override, so it is left alone.
func TestMain(m *testing.M) {
	_ = os.Setenv("NDCLI_PATHFINDER_HOST", testRelayHost)
	config.Get().Pathfinder.Host = testRelayHost
	os.Exit(m.Run())
}
