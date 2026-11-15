package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// The two transport tools return the keys of the CLI's -f json, and the
// write previews: nothing is sent without confirm.
func TestOrgConnectTransportTools(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"organization_name":"acme","connect_transport":"direct-peer","connect_relay":"eu","relays":[{"id":"eu","label":"Europe","hostname":"relay-eu.example.net","derp_port":443,"stun_port":3478}],"direct_peer_available":true}`)
	}))
	t.Cleanup(api.Close)
	s := newStaticAuthTestServerWithAPI(t, api, "acme")
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
	requests := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(seen)
	}
	keys := []string{"organization_name", "connect_transport", "connect_relay", "relays", "direct_peer_available"}
	hasKeys := func(what string, data interface{}, want ...string) {
		t.Helper()
		m, _ := data.(map[string]interface{})
		for _, k := range want {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: no %q in %v", what, k, m)
			}
		}
	}

	read := callTool(t, cs, "ndcli.org.connect_transport", `{}`)
	if !read.Success {
		t.Fatalf("read: %+v", read)
	}
	hasKeys("read", read.Data, keys...)

	before := requests()
	preview := callTool(t, cs, "ndcli.org.set_connect_transport", `{"transport":"direct-peer","relay":"eu"}`)
	if !preview.Success || requests() != before {
		t.Errorf("preview: %+v, %d requests sent; want none", preview, requests()-before)
	}
	for _, bad := range []string{`{"transport":"fastest","confirm":true}`, `{"transport":"direct-peer","relay":"eu","clear_relay":true,"confirm":true}`} {
		if resp := callTool(t, cs, "ndcli.org.set_connect_transport", bad); resp.Success || requests() != before {
			t.Errorf("%s: %+v, want a refusal with nothing sent", bad, resp)
		}
	}

	set := callTool(t, cs, "ndcli.org.set_connect_transport", `{"transport":"direct-peer","relay":"eu","confirm":true}`)
	if !set.Success {
		t.Fatalf("set: %+v", set)
	}
	hasKeys("set", set.Data, append(keys, "action")...)
	mu.Lock()
	defer mu.Unlock()
	if last := seen[len(seen)-1]; last != "PUT /api/v1/organizations/acme/connect-transport" {
		t.Errorf("last request %s", last)
	}
}
