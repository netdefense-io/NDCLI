package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// A read-only session refused for the device's agent ends device connect with
// the control plane's code and message and the CLI's hint, and sends nothing
// after the refused POST: no task exists to poll or cancel.
func TestDeviceConnectExplainsAnAgentTooOldForReadOnly(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error": "This device's agent cannot open a read-only session. Update NDAgent on the device to 1.13.6 or later, or ask a member with read-write access to connect.", "code": "CONNECT_AGENT_TOO_OLD"}`)
	}))
	defer srv.Close()

	origAPI, origSvc, origOrg, origTerms, origSignals := apiClient, svc, orgName, connectTerminals, connectSignalContext
	t.Cleanup(func() {
		apiClient, svc, orgName, connectTerminals, connectSignalContext = origAPI, origSvc, origOrg, origTerms, origSignals
	})
	apiClient = api.NewClient(srv.URL, false, &noopAuthProvider{})
	svc = service.New(apiClient, nil, nil)
	orgName = "acme"
	connectTerminals = func() (bool, bool) { return true, true }
	connectSignalContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }

	err := runDeviceConnect(findCommand(t, "device", "connect"), []string{"fw-01"})
	if api.GetErrorCode(err) != service.APICodeConnectAgentTooOld {
		t.Fatalf("err %v, code %q; want CONNECT_AGENT_TOO_OLD", err, api.GetErrorCode(err))
	}
	if msg := err.Error(); !strings.Contains(msg, "Update NDAgent on the device to 1.13.6") || !strings.Contains(msg, "Hint: "+connectAgentTooOldHint("fw-01")) {
		t.Errorf("message %q lacks the control plane's text or the hint", msg)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "POST /api/v1/organizations/acme/devices/fw-01/connect" {
		t.Errorf("requests %v, want only the refused POST", seen)
	}
}
