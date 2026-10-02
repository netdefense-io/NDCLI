package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSnippetCreateEnum_IncludesAUTHTypes guards ndcli.snippet.create's
// "type" enum for the two AUTH types by name — the structural subset test
// in tools_snippet_types_test.go compares only against
// models.SnippetCreatableTypes itself, so it would keep passing even if
// both were silently dropped from that list again.
func TestSnippetCreateEnum_IncludesAUTHTypes(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.snippet.create")
	got := schemaPropertyEnum(t, schema, "type")
	for _, want := range []string{"AUTH_SERVER", "AUTH_ORDER"} {
		found := false
		for _, ty := range got {
			if ty == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ndcli.snippet.create type enum = %v, missing %q", got, want)
		}
	}
}

// TestSnippetPullEnum_ExcludesAUTHTypes is the pull-side mirror: there is
// no AUTH PULL, so AUTH_SERVER/AUTH_ORDER must never reach the
// config_type enum. Named explicitly rather than only via the generic
// "pullable is a subset of creatable" check.
func TestSnippetPullEnum_ExcludesAUTHTypes(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.snippet.pull")
	got := schemaPropertyEnum(t, schema, "config_type")
	for _, ty := range got {
		if ty == "AUTH_SERVER" || ty == "AUTH_ORDER" {
			t.Errorf("ndcli.snippet.pull config_type enum contains %q, which must never be pullable", ty)
		}
	}
}

// TestSnippetToolDescriptions_StateOrgSuForAUTHTypes guards the create,
// update_content and delete tool descriptions against losing the org:su
// note — the only signal an LLM caller has, before calling the tool,
// about the elevated clearance AUTH_SERVER/AUTH_ORDER content needs
// regardless of its usual access.
func TestSnippetToolDescriptions_StateOrgSuForAUTHTypes(t *testing.T) {
	cs := connectTestClient(t)
	ctx := context.Background()

	wantOrgSu := map[string]bool{
		"ndcli.snippet.create":         true,
		"ndcli.snippet.update_content": true,
		"ndcli.snippet.rename":         true,
		"ndcli.snippet.set_priority":   true,
		"ndcli.snippet.delete":         true,
	}
	seen := map[string]bool{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if !wantOrgSu[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		if !strings.Contains(tool.Description, "org:su") {
			t.Errorf("%s description does not mention org:su, got: %s", tool.Name, tool.Description)
		}
		if !strings.Contains(tool.Description, "AUTH_SERVER") || !strings.Contains(tool.Description, "AUTH_ORDER") {
			t.Errorf("%s description does not name AUTH_SERVER/AUTH_ORDER, got: %s", tool.Name, tool.Description)
		}
	}
	for name := range wantOrgSu {
		if !seen[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

// TestSnippetToolDescriptions_StateInteractiveOrgSuForTRUSTTypes is the
// TRUST_CA/TRUST_CERT counterpart: all five write tools must say these
// types need org:su from an interactive login — a server started with
// NDCLI_TOKEN runs on a personal access token, which never carries org:su.
func TestSnippetToolDescriptions_StateInteractiveOrgSuForTRUSTTypes(t *testing.T) {
	cs := connectTestClient(t)
	ctx := context.Background()

	want := []string{
		"ndcli.snippet.create",
		"ndcli.snippet.update_content",
		"ndcli.snippet.rename",
		"ndcli.snippet.set_priority",
		"ndcli.snippet.delete",
	}
	descriptions := map[string]string{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		descriptions[tool.Name] = tool.Description
	}
	for _, name := range want {
		desc, ok := descriptions[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		for _, phrase := range []string{"TRUST_CA", "TRUST_CERT", "org:su from an interactive login", "personal access token"} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s description does not mention %q, got: %s", name, phrase, desc)
			}
		}
	}
}

// TestSnippetEnums_TRUSTTypesCreatableNeverPullable guards the TRUST types
// by name in the create enum, and their absence from the pull enum — the
// structural tests in tools_snippet_types_test.go compare only against
// the models lists, so they would keep passing if TRUST_CA/TRUST_CERT
// were dropped from SnippetCreatableTypes or added to SnippetPullableTypes.
func TestSnippetEnums_TRUSTTypesCreatableNeverPullable(t *testing.T) {
	create := schemaPropertyEnum(t, registeredToolSchema(t, "ndcli.snippet.create"), "type")
	pull := schemaPropertyEnum(t, registeredToolSchema(t, "ndcli.snippet.pull"), "config_type")
	for _, ty := range []string{"TRUST_CA", "TRUST_CERT"} {
		if !slices.Contains(create, ty) {
			t.Errorf("ndcli.snippet.create type enum = %v, missing %q", create, ty)
		}
		if slices.Contains(pull, ty) {
			t.Errorf("ndcli.snippet.pull config_type enum contains %q, which must never be pullable", ty)
		}
	}
}

// TestHandleSnippetCreate_SendsAUTHServerType is a plumbing smoke test: the
// MCP create handler must actually carry type=AUTH_SERVER through to
// NDManager. Content-shape validation (the field table, the secret
// reference shape, ...) is NDManager's job, not this client's — the fake
// server here accepts whatever it is given, and this test only proves the
// type value survives the MCP → service → API path unmodified.
func TestHandleSnippetCreate_SendsAUTHServerType(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotType, _ = body["type"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"corp-ad","type":"AUTH_SERVER","priority":1000}`))
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleSnippetCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"name":"corp-ad","type":"AUTH_SERVER","content":"{}"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	if gotType != "AUTH_SERVER" {
		t.Errorf("request sent type=%q, want AUTH_SERVER", gotType)
	}
}

// TestHandleSnippetCreate_SendsTRUSTCertContentUnmodified is the TRUST
// counterpart of the smoke test above, and also checks the content: a PEM
// body's newlines and the ${FW_KEY} reference must reach NDManager exactly
// as given. The content shape and the org:su gate are NDManager's to
// enforce; the fake server here accepts anything.
func TestHandleSnippetCreate_SendsTRUSTCertContentUnmodified(t *testing.T) {
	content := `{"name":"fw-wildcard","crt":"-----BEGIN CERTIFICATE-----\nMIIBfDCCASOg\n-----END CERTIFICATE-----\n","key":"${FW_KEY}"}`
	var gotType, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotType, _ = body["type"].(string)
		gotContent, _ = body["content"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"fw-wildcard","type":"TRUST_CERT","priority":1000}`))
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	args, err := json.Marshal(map[string]string{"name": "fw-wildcard", "type": "TRUST_CERT", "content": content})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.handleSnippetCreate(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	if gotType != "TRUST_CERT" {
		t.Errorf("request sent type=%q, want TRUST_CERT", gotType)
	}
	if gotContent != content {
		t.Errorf("request content changed in transit:\ngot:  %q\nwant: %q", gotContent, content)
	}
}
