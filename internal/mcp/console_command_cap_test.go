package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// A console command over 32 KiB is refused as INVALID_INPUT before the session
// is looked up or anything is sent: its exec request would not fit in one
// relay frame.
func TestConsoleExecRefusesACommandOverTheCap(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(api.Close)
	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	t.Cleanup(s.consoleSessions.CloseAll)

	args, _ := json.Marshal(map[string]any{"session_id": "no-such-session", "command": strings.Repeat("a", service.MaxConsoleCommandBytes+1)})
	res, err := s.handleConsoleExec(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
	if err != nil {
		t.Fatal(err)
	}
	tr := decodeToolResponse(t, res)
	if tr.Error == nil || tr.Error.Code != "INVALID_INPUT" || !strings.Contains(tr.Error.Message, "at most 32768 bytes") {
		t.Errorf("error %+v, want INVALID_INPUT naming the 32 KiB cap", tr.Error)
	}
}
