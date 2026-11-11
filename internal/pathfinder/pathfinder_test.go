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
		// The relay is named by its role; the config key stays literal.
		if !strings.Contains(buf.String(), "for the connection relay (pathfinder.ssl_verify=false)") || strings.Contains(buf.String(), "Pathfinder") {
			t.Errorf("expected warning to name the connection relay and its config key, got: %q", buf.String())
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

func TestRelayWebSocketURL(t *testing.T) {
	for _, tc := range []struct {
		name, host, want string
		upgraded         bool
	}{
		{"wss stays", "wss://pathfinder.example.test", "wss://pathfinder.example.test/ws", false},
		{"wss with path stays", "wss://pathfinder.example.test/ws", "wss://pathfinder.example.test/ws", false},
		{"https becomes wss", "https://pathfinder.example.test/", "wss://pathfinder.example.test/ws", false},
		{"no scheme is wss", "pathfinder.example.test", "wss://pathfinder.example.test/ws", false},
		{"no scheme with port is wss", "pathfinder.example.test:8443", "wss://pathfinder.example.test:8443/ws", false},

		{"ws is rewritten", "ws://dev-pathfinder.example.test", "wss://dev-pathfinder.example.test/ws", true},
		{"ws with path is rewritten", "ws://dev-pathfinder.example.test/ws", "wss://dev-pathfinder.example.test/ws", true},
		{"http is rewritten", "http://dev-pathfinder.example.test/", "wss://dev-pathfinder.example.test/ws", true},
		{"ws keeps its port", "ws://dev-pathfinder.example.test:8080", "wss://dev-pathfinder.example.test:8080/ws", true},
		{"userinfo is not the host", "ws://127.0.0.1@relay.example.test", "wss://127.0.0.1@relay.example.test/ws", true},
		{"localhost prefix is not localhost", "ws://localhost.example.test", "wss://localhost.example.test/ws", true},

		{"loopback ws stays", "ws://127.0.0.1:8080", "ws://127.0.0.1:8080/ws", false},
		{"loopback range stays", "ws://127.1.2.3", "ws://127.1.2.3/ws", false},
		{"localhost http stays plaintext", "http://localhost:8080", "ws://localhost:8080/ws", false},
		{"localhost in any case", "ws://LocalHost:9000", "ws://LocalHost:9000/ws", false},
		{"ipv6 loopback stays", "ws://[::1]:9000", "ws://[::1]:9000/ws", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, upgraded := relayWebSocketURL(tc.host)
			if got != tc.want || upgraded != tc.upgraded {
				t.Errorf("relayWebSocketURL(%q) = %q, %v; want %q, %v", tc.host, got, upgraded, tc.want, tc.upgraded)
			}
		})
	}
}

func TestWarnIfPathfinderPlaintext(t *testing.T) {
	t.Run("a rewritten host prints one line", func(t *testing.T) {
		var buf bytes.Buffer
		warnIfPathfinderPlaintext(&buf, "ws://dev-pathfinder.example.test")
		out := buf.String()
		if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
			t.Fatalf("want exactly one line, got %q", out)
		}
		for _, want := range []string{"pathfinder.host", "ws://dev-pathfinder.example.test", "wss://dev-pathfinder.example.test/ws", "wss://"} {
			if !strings.Contains(out, want) {
				t.Errorf("warning %q does not mention %q", out, want)
			}
		}
	})

	for _, host := range []string{"wss://pathfinder.example.test", "pathfinder.example.test", "ws://127.0.0.1:8080", "http://localhost:8080"} {
		t.Run("nothing for "+host, func(t *testing.T) {
			var buf bytes.Buffer
			warnIfPathfinderPlaintext(&buf, host)
			if buf.Len() != 0 {
				t.Errorf("want no output for %q, got %q", host, buf.String())
			}
		})
	}
}

func TestNewClientDialsAPlaintextHostOverTLS(t *testing.T) {
	origHost := config.Get().Pathfinder.Host
	defer func() { config.Get().Pathfinder.Host = origHost }()
	config.Get().Pathfinder.Host = "ws://dev-pathfinder.example.test"

	client, err := NewClient(ClientConfig{SessionID: "test-session"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := client.buildWebSocketURL(); got != "wss://dev-pathfinder.example.test/ws" {
		t.Errorf("buildWebSocketURL() = %q, want the wss:// URL", got)
	}
}
