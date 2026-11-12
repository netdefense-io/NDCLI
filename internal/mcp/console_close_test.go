package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// consoleOpenSeeing runs console_open against a control plane that completes
// the connect at once (read_only as given) and against relayHost, and returns
// the response and every request the control plane received.
func consoleOpenSeeing(t *testing.T, readOnly bool, relayHost string) (ToolResponse, []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"task": "task-123", "status": "PENDING"})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Remote session closed"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"task": "task-123", "status": "COMPLETED", "read_only": readOnly,
				"payload": `{"jti":"j1","pathfinder_session":"sess-abc"}`})
		}
	}))
	t.Cleanup(api.Close)

	origHost := config.Get().Pathfinder.Host
	config.Get().Pathfinder.Host = relayHost
	t.Cleanup(func() { config.Get().Pathfinder.Host = origHost })

	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	t.Cleanup(s.consoleSessions.CloseAll)
	s.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "ndcli", Version: config.Version}, nil)
	s.registerAll()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	resp := callTool(t, cs, "ndcli.device.console_open", `{"device":"fw-01"}`)
	mu.Lock()
	defer mu.Unlock()
	return resp, append([]string(nil), seen...)
}

// A console_open that gives the completed CONNECT session up before handing it
// back cancels the task, so NDManager deletes the relay session and the
// device's leg is closed at once instead of waiting out its TTL (NDPathFinder
// #30). The tool's own error is what the caller gets.
func TestConsoleOpenClosesTheSessionItGivesUp(t *testing.T) {
	refusingRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(refusingRelay.Close)

	for _, tc := range []struct {
		name      string
		readOnly  bool
		relayHost string
		wantCode  string
	}{
		{"read-only session", true, refusingRelay.URL, "READ_ONLY_SESSION"},
		{"no relay configured", false, "", ""},
		{"relay refuses the upgrade", false, refusingRelay.URL, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, seen := consoleOpenSeeing(t, tc.readOnly, tc.relayHost)
			if resp.Success || resp.Error == nil {
				t.Fatalf("console_open succeeded: %+v", resp)
			}
			if tc.wantCode != "" && resp.Error.Code != tc.wantCode {
				t.Errorf("error code = %s, want %s", resp.Error.Code, tc.wantCode)
			}
			cancels := 0
			for _, r := range seen {
				if r == "PUT /api/v1/tasks/task-123/cancel" {
					cancels++
				}
			}
			if cancels != 1 {
				t.Errorf("control plane saw %v, want one cancel of task-123", seen)
			}
		})
	}
}
