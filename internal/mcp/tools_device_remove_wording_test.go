package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestDeviceRemoveToolDescription_CarriesTheConsequence reads the description
// the way a client does — an in-memory session and tools/list. Device removal
// is final and makes the box wipe itself, so the description has to state
// that: it is the only thing an agent sees before deciding to call the tool.
func TestDeviceRemoveToolDescription_CarriesTheConsequence(t *testing.T) {
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
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	var description string
	var found bool
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if tool.Name == "ndcli.device.remove" {
			description, found = tool.Description, true
		}
	}
	if !found {
		t.Fatal("ndcli.device.remove is not registered")
	}
	if !strings.Contains(description, service.DeviceRemoveConsequence) {
		t.Errorf("description must carry the shared consequence wording, got: %s", description)
	}
	if !strings.Contains(description, "ask the user to confirm") {
		t.Errorf("description must tell the caller to ask the user first, got: %s", description)
	}
}

// TestDeviceRemovePreview_CarriesTheConsequenceAsData: the preview is what the
// model restates to the user before confirming, so the consequences have to
// arrive as a named field rather than only in the tool description, and the
// unconfirmed call must not reach the endpoint.
func TestDeviceRemovePreview_CarriesTheConsequenceAsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an unconfirmed removal must not reach the endpoint")
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleDeviceRemove(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"device":"clarence"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected a preview, got %v", resp.Data)
	}
	if data["consequence"] != service.DeviceRemoveConsequence {
		t.Errorf("preview consequence = %v, want the shared wording", data["consequence"])
	}
}

// TestDeviceRemoveSuccess_CarriesTheConsequenceAsData: a caller that asked the
// user out of band and went straight to confirm=true never saw the preview, so
// the executed call has to carry the consequence as well.
func TestDeviceRemoveSuccess_CarriesTheConsequenceAsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("removal must use DELETE, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleDeviceRemove(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"device":"clarence","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["action"] != "removed" {
		t.Fatalf("expected a removal result, got %v", resp.Data)
	}
	if data["consequence"] != service.DeviceRemoveConsequence {
		t.Errorf("success consequence = %v, want the shared wording", data["consequence"])
	}
}
