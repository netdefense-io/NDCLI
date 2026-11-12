package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConsoleSessionManager_ReserveCapsOpenAndConnecting(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()
	for i := range 5 {
		m.Add(makeStubSession(fmt.Sprintf("s%d", i), "acme", "fw"))
	}
	var releases []func()
	for range maxConsoleSessions - 5 {
		release, ok := m.reserve()
		if !ok {
			t.Fatal("reserve refused below the cap")
		}
		releases = append(releases, release)
	}
	if _, ok := m.reserve(); ok {
		t.Fatalf("reserve allowed a session past %d (5 open, 3 connecting)", maxConsoleSessions)
	}

	releases[0]()
	releases[0]()
	if _, ok := m.reserve(); !ok {
		t.Fatal("a released slot was not given back")
	}
	if _, ok := m.reserve(); ok {
		t.Fatal("releasing the same slot twice gave back two slots")
	}

	m.Remove("s0")
	if _, ok := m.reserve(); !ok {
		t.Fatal("closing a session did not free its slot")
	}
}

func TestConsoleSessionManager_ConcurrentReservesStopAtTheCap(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()
	var granted int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := m.reserve(); ok {
				atomic.AddInt32(&granted, 1)
			}
		}()
	}
	wg.Wait()
	if granted != maxConsoleSessions {
		t.Errorf("50 concurrent reserves granted %d slots, want %d", granted, maxConsoleSessions)
	}
}

// At the cap, console_open must refuse before POST /connect, which would
// create a CONNECT task on the device.
func TestConsoleOpenRefusesAtTheCapBeforeConnecting(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	defer s.consoleSessions.CloseAll()
	for i := range maxConsoleSessions {
		s.consoleSessions.Add(makeStubSession(fmt.Sprintf("s%d", i), "acme", "fw"))
	}

	result, err := s.handleConsoleOpen(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"device":"fw-01"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success || resp.Error == nil || resp.Error.Code != "CONSOLE_LIMIT" {
		t.Fatalf("want a CONSOLE_LIMIT error, got %+v", resp)
	}
	if !strings.Contains(resp.Error.Message, "console_close") {
		t.Errorf("message %q does not say how to free a slot", resp.Error.Message)
	}
	data, _ := resp.Data.(map[string]interface{})
	if got, _ := data["max_sessions"].(float64); int(got) != maxConsoleSessions {
		t.Errorf("data.max_sessions = %v, want %d", resp.Data, maxConsoleSessions)
	}
	if n := atomic.LoadInt32(&requests); n != 0 {
		t.Errorf("console_open sent %d request(s) to the control plane at the cap, want none", n)
	}
}

func TestConsoleOpenDescriptionStatesTheCap(t *testing.T) {
	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	for _, tool := range result.Tools {
		if tool.Name == "ndcli.device.console_open" {
			if want := fmt.Sprintf("at most %d sessions", maxConsoleSessions); !strings.Contains(tool.Description, want) {
				t.Errorf("console_open description does not say %q: %s", want, tool.Description)
			}
			return
		}
	}
	t.Fatal("console_open tool not found")
}
