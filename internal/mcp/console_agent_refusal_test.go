package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// A CONNECT task that failed because the relay refused the device's upgrade
// with 429 answers console_open with RATE_LIMITED and the relay's wait, as a
// refusal of ndcli's own dial does.
func TestConsoleOpenReadsTheDevicesRateLimitedRelay(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"task": "task-123", "status": "PENDING"}`)
			return
		}
		_, _ = io.WriteString(w, `{"task": "task-123", "status": "FAILED", "message": "Relay session ended: connect failed: dial failed: the relay answered HTTP 429 (Retry-After: 15): websocket: bad handshake"}`)
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
	data, _ := resp.Data.(map[string]interface{})
	if resp.Success || resp.Error == nil || resp.Error.Code != "RATE_LIMITED" || data["retry_after_seconds"] != float64(15) {
		t.Errorf("response %+v, data %v; want RATE_LIMITED with retry_after_seconds 15", resp.Error, resp.Data)
	}
}
