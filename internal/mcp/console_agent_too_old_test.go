package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// console_open refused for the device's agent returns the control plane's code
// and message with console_open's hint, which says an update would not give
// a read-only caller commands, and sends nothing after the refused POST.
func TestConsoleOpenExplainsAnAgentTooOldForReadOnly(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error": "This device's agent cannot open a read-only session.", "code": "CONNECT_AGENT_TOO_OLD"}`)
	}))
	t.Cleanup(api.Close)

	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	t.Cleanup(s.consoleSessions.CloseAll)
	s.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "ndcli", Version: config.Version}, nil)
	s.registerAll()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	resp := callTool(t, cs, "ndcli.device.console_open", `{"device":"fw-01"}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != service.APICodeConnectAgentTooOld {
		t.Fatalf("response %+v, want CONNECT_AGENT_TOO_OLD", resp)
	}
	if !strings.Contains(resp.Error.Message, "cannot open a read-only session") || !strings.Contains(resp.Error.Message, "Hint: "+consoleAgentTooOldHint) {
		t.Errorf("message %q lacks the control plane's text or the hint", resp.Error.Message)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "POST /api/v1/organizations/acme/devices/fw-01/connect" {
		t.Errorf("requests %v, want only the refused POST", seen)
	}
}
