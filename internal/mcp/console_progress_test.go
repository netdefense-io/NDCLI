package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// consoleOpenWithProgress calls console_open over a real in-memory MCP
// connection against a control plane that completes the connect at once, and
// returns the progress notifications the client received.
func consoleOpenWithProgress(t *testing.T, token any, pendingPolls int) []*mcp.ProgressNotificationParams {
	t.Helper()
	var mu sync.Mutex
	polls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"task": "task-123", "status": "PENDING"})
			return
		}
		mu.Lock()
		polls++
		n := polls
		mu.Unlock()
		if n <= pendingPolls {
			_ = json.NewEncoder(w).Encode(map[string]any{"task": "task-123", "status": "PENDING"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"task": "task-123", "status": "COMPLETED", "payload": `{"jti":"j1","pathfinder_session":"sess-abc"}`})
	}))
	t.Cleanup(api.Close)

	// No relay host: console_open stops at "relay not configured" instead of
	// dialling the default (production) relay.
	origHost := config.Get().Pathfinder.Host
	config.Get().Pathfinder.Host = ""
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

	var got []*mcp.ProgressNotificationParams
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			got = append(got, req.Params)
			mu.Unlock()
		},
	})
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	params := &mcp.CallToolParams{Name: "ndcli.device.console_open", Arguments: map[string]any{"device": "fw-01"}}
	if token != nil {
		params.SetProgressToken(token)
	}
	if _, err := cs.CallTool(ctx, params); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// The relay step fails (no relay host); the notifications before it are
	// what is under test. They can arrive just after the result.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if token == nil || n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]*mcp.ProgressNotificationParams(nil), got...)
}

func TestConsoleOpenSendsProgressWhenAsked(t *testing.T) {
	got := consoleOpenWithProgress(t, "tok-1", 1)
	if len(got) < 2 {
		t.Fatalf("got %d progress notifications, want at least the request and the wait", len(got))
	}
	if !strings.HasPrefix(got[0].Message, "Requesting a connect task for fw-01") {
		t.Errorf("first message = %q", got[0].Message)
	}
	if !strings.HasPrefix(got[1].Message, "Waiting for fw-01 to pick up the connect task (PENDING") {
		t.Errorf("second message = %q", got[1].Message)
	}
	for i, p := range got {
		if p.ProgressToken != "tok-1" {
			t.Errorf("notification %d token = %v, want tok-1", i, p.ProgressToken)
		}
		if i > 0 && p.Progress <= got[i-1].Progress {
			t.Errorf("progress went %v → %v; it must increase", got[i-1].Progress, p.Progress)
		}
	}
}

func TestConsoleOpenSendsNoProgressUnasked(t *testing.T) {
	if got := consoleOpenWithProgress(t, nil, 1); len(got) != 0 {
		t.Errorf("got %d progress notifications without a progressToken, want none", len(got))
	}
}
