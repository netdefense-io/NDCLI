package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

func TestWarnIfTLSVerifyDisabled(t *testing.T) {
	original := config.Get().Controlplane.SSLVerify
	defer func() { config.Get().Controlplane.SSLVerify = original }()

	t.Run("ssl verify disabled prints warning", func(t *testing.T) {
		config.Get().Controlplane.SSLVerify = false

		var buf bytes.Buffer
		warnIfTLSVerifyDisabled(&buf)

		if buf.Len() == 0 {
			t.Fatal("expected a warning to be printed when ssl_verify is disabled, got no output")
		}
		if !strings.Contains(buf.String(), "TLS") {
			t.Errorf("expected warning to mention TLS, got: %q", buf.String())
		}
	})

	t.Run("ssl verify enabled prints nothing", func(t *testing.T) {
		config.Get().Controlplane.SSLVerify = true

		var buf bytes.Buffer
		warnIfTLSVerifyDisabled(&buf)

		if buf.Len() != 0 {
			t.Errorf("expected no output when ssl_verify is enabled, got: %q", buf.String())
		}
	})
}
