package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// fakeAuthProvider satisfies api.AuthProvider with a static token, mirroring
// the pattern used by internal/service's own test helpers.
type fakeAuthProvider struct{}

func (f *fakeAuthProvider) GetAccessToken() (string, error) { return "test-token", nil }
func (f *fakeAuthProvider) ForceRefresh() error             { return nil }

// newTestServer builds a *Server backed by a real *api.Client pointing at
// srv, with an unauthenticated (nil) auth manager and org as the configured
// default organization.
//
// Note: service.New requires a concrete *auth.Manager for its authMgr
// parameter, which can't be faked from this package (it wraps a real
// OAuth2/keyring stack). So the Service built here always fails
// RequireAuth(). Handlers that gate on RequireAuth() are tested for that
// gate directly (e.g. TestHandleConsoleList_RequiresAuth); the confirm-gate
// and business logic beneath RequireAuth is tested via the internal "core"
// methods the RequireAuth-gated handlers delegate to (createToken,
// runCommand, firmwareUpgrade, syncApply, ...), which take an already-parsed
// input and never call RequireAuth themselves.
func newTestServer(t *testing.T, srv *httptest.Server, org string) *Server {
	t.Helper()
	client := api.NewClient(srv.URL, false, &fakeAuthProvider{})
	cfg := &config.Config{}
	cfg.Organization.Name = org
	return &Server{
		svc:             service.New(client, nil, cfg),
		consoleSessions: newConsoleSessionManager(),
	}
}

// connectTestClient builds a *Server with every tool registered, wires it to
// an in-memory client via mcp.NewInMemoryTransports, and returns the
// connected client session — the real-protocol round trip both
// registeredToolNames (tool_registry_test.go) and registeredToolSchema
// (tools_snippet_types_test.go) drive, so the connection setup lives in one
// place. Both sides are closed via t.Cleanup.
func connectTestClient(t *testing.T) *mcp.ClientSession {
	t.Helper()

	s := &Server{
		mcpServer: mcp.NewServer(&mcp.Implementation{Name: "ndcli", Version: config.Version}, nil),
	}
	s.registerAll()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	return cs
}

// decodeToolResult unmarshals the JSON text content of a CallToolResult into
// dst (typically *ToolResponse).
func decodeToolResult(t *testing.T, result *mcp.CallToolResult, dst interface{}) {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	if err := json.Unmarshal([]byte(tc.Text), dst); err != nil {
		t.Fatalf("failed to decode tool result %q: %v", tc.Text, err)
	}
}
