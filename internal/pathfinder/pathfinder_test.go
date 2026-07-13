package pathfinder

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

func TestWarnIfPathfinderSSLVerifyDisabled(t *testing.T) {
	original := config.Get().Pathfinder.SSLVerify
	defer func() { config.Get().Pathfinder.SSLVerify = original }()

	t.Run("ssl verify disabled prints warning", func(t *testing.T) {
		config.Get().Pathfinder.SSLVerify = false

		var buf bytes.Buffer
		warnIfPathfinderSSLVerifyDisabled(&buf)

		if buf.Len() == 0 {
			t.Fatal("expected a warning to be printed when pathfinder.ssl_verify is disabled, got no output")
		}
		if !strings.Contains(buf.String(), "TLS") {
			t.Errorf("expected warning to mention TLS, got: %q", buf.String())
		}
	})

	t.Run("ssl verify enabled prints nothing", func(t *testing.T) {
		config.Get().Pathfinder.SSLVerify = true

		var buf bytes.Buffer
		warnIfPathfinderSSLVerifyDisabled(&buf)

		if buf.Len() != 0 {
			t.Errorf("expected no output when pathfinder.ssl_verify is enabled, got: %q", buf.String())
		}
	})
}

func TestNewClientWarnsOnDisabledSSLVerify(t *testing.T) {
	origHost := config.Get().Pathfinder.Host
	origVerify := config.Get().Pathfinder.SSLVerify
	defer func() {
		config.Get().Pathfinder.Host = origHost
		config.Get().Pathfinder.SSLVerify = origVerify
	}()

	config.Get().Pathfinder.Host = "wss://pathfinder.example.test"
	config.Get().Pathfinder.SSLVerify = false

	// NewClient itself writes to os.Stderr rather than an injectable writer,
	// so this only exercises the wiring (no panic, client still built); the
	// warning content is covered by TestWarnIfPathfinderSSLVerifyDisabled.
	client, err := NewClient(ClientConfig{SessionID: "test-session"})
	if err != nil {
		t.Fatalf("NewClient returned unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a non-nil client")
	}
	if client.sslVerify {
		t.Error("expected client.sslVerify to reflect the disabled config")
	}
}
