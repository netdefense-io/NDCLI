package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

const weakKeyMessage = "'crt' has an RSA key of 1024 bits: use 2048 to 4096 bits"

func answering(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func describeSnippetVia(t *testing.T, srv *httptest.Server) ToolResponse {
	t.Helper()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	result, err := s.handleSnippetDescribe(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"name":"corp-ca"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success || resp.Error == nil {
		t.Fatalf("expected an error result, got %+v", resp)
	}
	return resp
}

// TestErrorResult_CarriesTheServerCode: an NDManager error used to reach the
// caller as API_ERROR, whatever code the server sent.
func TestErrorResult_CarriesTheServerCode(t *testing.T) {
	srv := answering(http.StatusUnprocessableEntity, `{"error":"`+weakKeyMessage+`","code":"TRUST_KEY_WEAK"}`)
	defer srv.Close()

	resp := describeSnippetVia(t, srv)

	if resp.Error.Code != "TRUST_KEY_WEAK" {
		t.Errorf("error.code = %q, want TRUST_KEY_WEAK", resp.Error.Code)
	}
	if resp.Error.Message != weakKeyMessage {
		t.Errorf("error.message = %q, want the server's message unchanged", resp.Error.Message)
	}
}

func TestErrorResult_UncodedServerErrorKeepsAPIError(t *testing.T) {
	srv := answering(http.StatusInternalServerError, `{"error":"Internal server error"}`)
	defer srv.Close()

	if resp := describeSnippetVia(t, srv); resp.Error.Code != service.CodeAPIError {
		t.Errorf("error.code = %q, want %s", resp.Error.Code, service.CodeAPIError)
	}
}

// TestDeviceApproveAll_FailedEntryCarriesTheCode: a device that could not be
// approved is reported inside a successful result, so its code has to travel
// in the entry.
func TestDeviceApproveAll_FailedEntryCarriesTheCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/approve") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"Device limit reached for your plan","code":"PLAN_LIMIT_EXCEEDED"}`)
			return
		}
		_, _ = io.WriteString(w, `{"devices":[{"name":"fw01","status":"PENDING"}],"total":1}`)
	}))
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")

	result, err := s.handleDeviceApproveAll(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"confirm":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp struct {
		Data struct {
			Failed []map[string]string `json:"failed"`
		} `json:"data"`
	}
	decodeToolResult(t, result, &resp)
	if len(resp.Data.Failed) != 1 {
		t.Fatalf("expected one failed device, got %+v", resp.Data.Failed)
	}
	got := resp.Data.Failed[0]
	if got["code"] != "PLAN_LIMIT_EXCEEDED" || !strings.HasPrefix(got["error"], "Device limit reached for your plan") {
		t.Errorf("failed entry = %v", got)
	}
}

const uuidInUseMessage = "'uuid' is already used by snippet 'corp-web' in this organization; omit it and NetDefense generates one"

// TestSnippetWriteTools_ShowSNIPPET_UUID_IN_USE: a create or a content update
// whose content carries a uuid another snippet holds is refused with 409
// SNIPPET_UUID_IN_USE. Both tools report that code and NDManager's message,
// which tells the caller to omit the uuid.
func TestSnippetWriteTools_ShowSNIPPET_UUID_IN_USE(t *testing.T) {
	srv := answering(http.StatusConflict,
		`{"error":"`+uuidInUseMessage+`","code":"SNIPPET_UUID_IN_USE","blocking_resources":["corp-web"]}`)
	defer srv.Close()
	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	content := `{\"name\":\"corp-web\",\"type\":\"host\",\"content\":\"10.0.0.1\",\"uuid\":\"221f3268-aaaa-4aaa-8aaa-aaaaaaaaaaaa\"}`

	for name, call := range map[string]func() (*mcp.CallToolResult, error){
		"ndcli.snippet.create": func() (*mcp.CallToolResult, error) {
			return s.handleSnippetCreate(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
					`{"name":"corp-web-copy","type":"ALIAS","content":"` + content + `"}`)},
			})
		},
		"ndcli.snippet.update_content": func() (*mcp.CallToolResult, error) {
			return s.handleSnippetUpdateContent(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(
					`{"name":"corp-web-copy","content":"` + content + `","confirm":true}`)},
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := call()
			if err != nil {
				t.Fatalf("unexpected transport error: %v", err)
			}
			var resp ToolResponse
			decodeToolResult(t, result, &resp)
			if resp.Success || resp.Error == nil {
				t.Fatalf("expected an error result, got %+v", resp)
			}
			if resp.Error.Code != "SNIPPET_UUID_IN_USE" || resp.Error.Message != uuidInUseMessage {
				t.Errorf("error = %+v, want code SNIPPET_UUID_IN_USE and NDManager's message", resp.Error)
			}
		})
	}
}

// TestSyncApply_CarriesSNIPPET_UUID_CONFLICT: NDManager refuses to build a
// device whose snippets share a uuid, with SNIPPET_UUID_CONFLICT on that
// device's entry in the sync response. The tool keeps the code and the
// message, which names the uuid, the snippets and the templates.
func TestSyncApply_CarriesSNIPPET_UUID_CONFLICT(t *testing.T) {
	const message = "More than one snippet on this device carries the same uuid: " +
		"221f3268-aaaa-4aaa-8aaa-aaaaaaaaaaaa (snippets: corp-web, corp-web-copy; templates: edge). " +
		"Keep one snippet per uuid: delete the others and create them again without a uuid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/sync" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Sync triggered for 0 device(s); 1 error(s)",
			"tasks":   []interface{}{},
			"errors": []map[string]interface{}{
				{"device_name": "e2e-a", "error": message, "code": "SNIPPET_UUID_CONFLICT"},
			},
		})
	}))
	defer srv.Close()

	result, err := newTestServer(t, srv, "acme").syncApply(context.Background(), &syncApplyInput{Confirm: true})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp struct {
		Data struct {
			Errors []map[string]interface{} `json:"errors"`
		} `json:"data"`
	}
	decodeToolResult(t, result, &resp)
	if len(resp.Data.Errors) != 1 {
		t.Fatalf("expected one device error, got %v", resp.Data.Errors)
	}
	if got := resp.Data.Errors[0]; got["code"] != "SNIPPET_UUID_CONFLICT" || got["error"] != message {
		t.Errorf("errors[0] = %v", got)
	}
}

// TestErrorResult_ServerCodeWinsOverAnyServiceCode pins the precedence
// errorResult applies: when the error carries NDManager's code, that is
// error.code whatever code the service gave its wrapper, because it is the
// code the CLI prints for the same failure.
func TestErrorResult_ServerCodeWinsOverAnyServiceCode(t *testing.T) {
	apiErr := &api.APIError{StatusCode: http.StatusConflict, Code: "SNIPPET_UUID_IN_USE", Message: uuidInUseMessage}
	for _, serviceCode := range []string{service.CodeAPIError, service.CodeInvalidInput, service.CodeAuthFailed} {
		result, err := (&Server{}).errorResult(&service.Error{Code: serviceCode, Message: apiErr.Error(), Err: apiErr})
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		var resp ToolResponse
		decodeToolResult(t, result, &resp)
		if resp.Error.Code != "SNIPPET_UUID_IN_USE" {
			t.Errorf("service code %s: error.code = %q, want SNIPPET_UUID_IN_USE", serviceCode, resp.Error.Code)
		}
	}
}

// TestErrorResult_CodeSurvivesWrapping: a handler that wraps an error to say
// which step failed keeps its code. errorResult used to read the code with a
// type switch, which sees only the outermost type, so every such error went
// out uncoded.
func TestErrorResult_CodeSurvivesWrapping(t *testing.T) {
	mismatch := &service.Error{Code: service.CodeVariableValueMismatch, Message: "the value differs"}
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"service error wrapped with %w", fmt.Errorf("step 2: %w", &service.Error{Code: service.CodeInvalidInput, Message: "bad"}), service.CodeInvalidInput},
		{"tool error wrapped with %w", fmt.Errorf("step 2: %w", &ToolError{Code: "CONFIRM_REQUIRED", Message: "confirm"}), "CONFIRM_REQUIRED"},
		{"service error in a wrapper type", &service.NetworkPrefixProvisionError{Step: "creating the org variable", Err: mismatch}, service.CodeVariableValueMismatch},
		{"server code under two wrappers", fmt.Errorf("step 2: %w", &service.Error{Code: service.CodeAPIError, Err: &api.APIError{StatusCode: http.StatusConflict, Code: "SNIPPET_UUID_IN_USE", Message: uuidInUseMessage}}), "SNIPPET_UUID_IN_USE"},
		{"nothing coded", errors.New("plain"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (&Server{}).errorResult(tc.err)
			if err != nil {
				t.Fatalf("unexpected transport error: %v", err)
			}
			var resp ToolResponse
			decodeToolResult(t, result, &resp)
			if resp.Error.Code != tc.code {
				t.Errorf("error.code = %q, want %q", resp.Error.Code, tc.code)
			}
			if resp.Error.Message != tc.err.Error() {
				t.Errorf("error.message = %q, want the wrapped message %q", resp.Error.Message, tc.err.Error())
			}
		})
	}
}
