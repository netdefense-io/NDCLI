package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// TestFetchCLIConfig_SanitizesErrorBody is REVERT-SENSITIVE: it fails
// against the pre-fix FetchCLIConfig, which spliced the raw server body
// straight into the returned error string. FetchCLIConfig runs
// unauthenticated on every login (internal/auth/manager.go), before any
// trust has been established with the server at cfg.Controlplane.Host, and
// Cobra does not SilenceErrors — so a hostile or misbehaving NDManager
// returning a >=400 body containing a terminal escape sequence would have
// it interpreted by the operator's terminal the moment `ndcli auth login`
// (or any command that triggers an implicit login) fails.
func TestFetchCLIConfig_SanitizesErrorBody(t *testing.T) {
	evilBody := "evil" + testESC + "[2J" + testESC + "[Hmessage"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(evilBody))
	}))
	defer server.Close()

	cfg := config.Get()
	originalHost := cfg.Controlplane.Host
	cfg.Controlplane.Host = server.URL
	defer func() { cfg.Controlplane.Host = originalHost }()

	_, err := FetchCLIConfig(context.Background())
	if err == nil {
		t.Fatal("expected an error from FetchCLIConfig, got nil")
	}

	if strings.ContainsAny(err.Error(), testESC+testBEL) {
		t.Fatalf("FetchCLIConfig error still contains a raw control byte: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "evil") || !strings.Contains(err.Error(), "message") {
		t.Fatalf("surrounding printable text was not preserved: %q", err.Error())
	}
}
