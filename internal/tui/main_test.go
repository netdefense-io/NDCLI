package tui

import (
	"os"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/relayguard"
)

func TestMain(m *testing.M) {
	os.Exit(relayguard.Run(m))
}
