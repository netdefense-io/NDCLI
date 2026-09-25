package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP variable tools refuse to write secret values: the whole reason
// `ndcli variable ... --value-stdin` and the interactive no-echo prompt
// exist is that a value typed into an MCP tool call argument instead
// sits in the calling LLM's context and transcript for the life of the
// session.

// TestVariableCreateDescription_DoesNotContradictTheHandler guards the
// tool description against telling the caller `secret=true` sometimes
// works (it never does, over MCP) — the handler refuses it
// unconditionally, at every scope, so the schema must say that rather
// than the stale "honoured only at org scope" wording that predates the
// refusal.
func TestVariableCreateDescription_DoesNotContradictTheHandler(t *testing.T) {
	cs := connectTestClient(t)
	ctx := context.Background()

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if tool.Name != "ndcli.variable.create" {
			continue
		}
		if strings.Contains(tool.Description, "honoured only at org scope") {
			t.Errorf("description still claims secret=true sometimes works, got: %s", tool.Description)
		}
		if !strings.Contains(tool.Description, "Always refuses") {
			t.Errorf("description should state the unconditional refusal, got: %s", tool.Description)
		}
		return
	}
	t.Fatal("ndcli.variable.create is not registered")
}

func TestHandleVariableCreate_RefusesSecretFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused create must never reach the API, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"AD_BIND_PW","value":"hunter2","secret":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the secret create to be refused")
	}
	if !strings.Contains(resp.Error.Message, "value-stdin") {
		t.Errorf("refusal must point at the CLI path, got: %s", resp.Error)
	}
}

func TestHandleVariableCreate_RefusesOverrideOfAlreadySecretOrgVariable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"ORG","secret":true}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused override create must never reach the API, got %s %s", r.Method, r.URL.Path)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"device","entity":"e2e-a","name":"AD_BIND_PW","value":"hunter2"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the override create to be refused")
	}
}

// TestHandleVariableCreate_FailsClosedOnAmbiguousLookup guards the
// resolveVariableSecretStatus fail-closed rule: a lookup failure that
// isn't a confirmed 404 (a timeout, a 5xx, ...) must refuse the write
// rather than silently proceeding as if the variable were known not to
// be secret — the one failure mode where failing open would leak a
// plaintext value onto the MCP transport for a variable that might
// actually be secret.
func TestHandleVariableCreate_FailsClosedOnAmbiguousLookup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom","code":"INTERNAL_ERROR"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a failed-closed create must never reach the API, got %s %s", r.Method, r.URL.Path)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"device","entity":"e2e-a","name":"AD_BIND_PW","value":"hunter2"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the create to be refused when the secret-status lookup fails ambiguously")
	}
}

func TestHandleVariableCreate_AllowsNonSecretOverride(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/PLAIN_VAR", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found","code":"NOT_FOUND"}`))
	})
	mux.HandleFunc("POST /api/v1/organizations/acme/devices/e2e-a/variables", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"PLAIN_VAR","value":"hello","scope":"DEVICE","scope_name":"e2e-a","secret":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"device","entity":"e2e-a","name":"PLAIN_VAR","value":"hello"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success for a non-secret override, got error: %v", resp.Error)
	}
}

func TestHandleVariableSet_RefusesValueWriteToSecretVariable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"ORG","secret":true}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a refused value write must never reach PATCH")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"AD_BIND_PW","value":"hunter2","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the value write to be refused")
	}
	if !strings.Contains(resp.Error.Message, "value-stdin") {
		t.Errorf("refusal must point at the CLI path, got: %s", resp.Error)
	}
}

// TestHandleVariableSet_FailsClosedOnAmbiguousLookup is the set-side
// mirror of the create test above.
func TestHandleVariableSet_FailsClosedOnAmbiguousLookup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		w.Write([]byte(`{"error":"timeout"}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a failed-closed value write must never reach PATCH")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"AD_BIND_PW","value":"hunter2","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the value write to be refused when the secret-status lookup fails ambiguously")
	}
}

// TestHandleVariableSet_RefusesValueWriteToADeviceOverrideOfASecretVariable
// is the override-scope mirror of TestHandleVariableSet_RefusesValueWriteToSecretVariable:
// secret status is inherited by device/OU/template overrides of an
// org-scope secret variable, so a value write must be refused there too,
// not only at org scope.
func TestHandleVariableSet_RefusesValueWriteToADeviceOverrideOfASecretVariable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/devices/e2e-a/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"DEVICE","scope_name":"e2e-a","secret":true}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/devices/e2e-a/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a refused value write must never reach PATCH")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"device","entity":"e2e-a","name":"AD_BIND_PW","value":"hunter2","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the device-override value write to be refused")
	}
	if !strings.Contains(resp.Error.Message, "value-stdin") {
		t.Errorf("refusal must point at the CLI path, got: %s", resp.Error)
	}
}

// TestHandleVariableSet_PreviewRunsSecretCheckBeforeConfirmGate guards the
// deliberate ordering the code comment above the check describes: a
// confirm=false value-set call is no longer a pure local preview once
// Value is set — the secret-status GET still runs, so an about-to-be-
// refused call is refused before it ever reaches the preview response.
// Non-secret case: the GET fires and the call still returns a preview,
// never a PATCH.
func TestHandleVariableSet_PreviewRunsSecretCheckBeforeConfirmGate(t *testing.T) {
	getCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/PLAIN_VAR", func(w http.ResponseWriter, r *http.Request) {
		getCalled = true
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found","code":"NOT_FOUND"}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/variables/PLAIN_VAR", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a confirm=false call must never reach PATCH")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"PLAIN_VAR","value":"hello","confirm":false}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected a preview response, got error: %v", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got: %T", resp.Data)
	}
	if preview, _ := data["preview"].(bool); !preview {
		t.Errorf("expected data.preview=true, got: %v", data)
	}
	if !getCalled {
		t.Error("expected the secret-status GET to run ahead of the confirm gate")
	}
}

// TestHandleVariableSet_PreviewRefusesValueWriteToSecretVariable is the
// confirm=false mirror of TestHandleVariableSet_RefusesValueWriteToSecretVariable:
// the refusal fires even in preview mode, so a caller never sees "this
// would work" for a write the confirmed call would reject.
func TestHandleVariableSet_PreviewRefusesValueWriteToSecretVariable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"ORG","secret":true}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a refused preview must never reach PATCH")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"AD_BIND_PW","value":"hunter2","confirm":false}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the preview to be refused for a secret value write")
	}
	if !strings.Contains(resp.Error.Message, "value-stdin") {
		t.Errorf("refusal must point at the CLI path, got: %s", resp.Error)
	}
}

func TestHandleVariableSet_AllowsDescriptionOnlyUpdateOnSecretVariable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"ORG","secret":true}`))
	})
	mux.HandleFunc("PATCH /api/v1/organizations/acme/variables/AD_BIND_PW", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"AD_BIND_PW","value":"[REDACTED]","scope":"ORG","secret":true,"description":"updated"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleVariableSet(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
			`{"scope":"org","name":"AD_BIND_PW","description":"updated","confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected a description-only update on a secret variable to succeed, got error: %v", resp.Error)
	}
}
