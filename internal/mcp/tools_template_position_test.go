package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callTemplateUpdate(t *testing.T, arguments string) (ToolResponse, []string) {
	t.Helper()
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = append(sent, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(raw)))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"edge","position":"APPEND"}`)
	}))
	defer srv.Close()

	result, err := newStaticAuthTestServerWithAPI(t, srv, "acme").handleTemplateUpdate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(arguments)},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	return resp, sent
}

func TestTemplateUpdate_PositionInAnyCase(t *testing.T) {
	preview, sent := callTemplateUpdate(t, `{"name":"edge","position":"append"}`)
	if len(sent) != 0 || !strings.Contains(preview.Message, "position → APPEND") {
		t.Errorf("preview = %q, sent %v", preview.Message, sent)
	}

	resp, sent := callTemplateUpdate(t, `{"name":"edge","position":"append","confirm":true}`)
	if !resp.Success || len(sent) != 1 || sent[0] != `PATCH /api/v1/organizations/acme/templates/edge {"position":"APPEND"}` {
		t.Errorf("resp = %+v, sent %v", resp, sent)
	}
}

func TestTemplateUpdate_UnknownPositionIsRefused(t *testing.T) {
	resp, sent := callTemplateUpdate(t, `{"name":"edge","position":"middle","confirm":true}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" || len(sent) != 0 {
		t.Errorf("resp = %+v, sent %v", resp, sent)
	}
}
