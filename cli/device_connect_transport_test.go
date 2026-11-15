package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// connectWithPreflight runs device connect against a control plane that
// answers the preflight with info (a bare 404 when empty) and fails the
// connect request, and returns what the command wrote to stderr.
func connectWithPreflight(t *testing.T, info string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/connect-info") && info != "":
			_, _ = io.WriteString(w, info)
		case strings.HasSuffix(r.URL.Path, "/connect-info"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"detail":"Not Found"}`)
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"no","code":"ACCESS_DENIED"}`)
		}
	}))
	t.Cleanup(srv.Close)

	origAPI, origSvc, origOrg, origTerms, origSignals, origStderr := apiClient, svc, orgName, connectTerminals, connectSignalContext, os.Stderr
	t.Cleanup(func() {
		apiClient, svc, orgName, connectTerminals, connectSignalContext, os.Stderr = origAPI, origSvc, origOrg, origTerms, origSignals, origStderr
	})
	apiClient = api.NewClient(srv.URL, false, &noopAuthProvider{})
	svc = service.New(apiClient, nil, nil)
	orgName = "acme"
	connectTerminals = func() (bool, bool) { return true, true }
	connectSignalContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	runErr := runDeviceConnect(findCommand(t, "device", "connect"), []string{"fw-01"})
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if api.GetErrorCode(runErr) != "ACCESS_DENIED" {
		t.Fatalf("err = %v, want the connect request's refusal", runErr)
	}
	return string(out)
}

// Only an organization set to direct-peer hears about transports.
func TestDeviceConnectSaysNothingNewOnTheWebsocketRelay(t *testing.T) {
	for name, info := range map[string]string{
		"a server that predates the preflight":   "",
		"an organization on the websocket relay": `{"transport":"relay-legacy","reason":"org_setting","org_transport":"relay-legacy"}`,
	} {
		if out := connectWithPreflight(t, info); out != "" {
			t.Errorf("%s: stderr = %q, want nothing", name, out)
		}
	}
}

func TestDeviceConnectReportsAFallback(t *testing.T) {
	out := connectWithPreflight(t, `{"transport":"relay-legacy","reason":"protocol_mismatch","org_transport":"direct-peer"}`)
	want := "Note: this session runs on the websocket relay, not on direct-peer + encrypted relay as the organization is set to: this program and the device's agent are on different generations of direct-peer + encrypted relay, and the older one needs an update.\n"
	if out != want {
		t.Errorf("stderr = %q\nwant %q", out, want)
	}
}
