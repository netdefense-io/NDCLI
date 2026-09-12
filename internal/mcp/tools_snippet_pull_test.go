package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestSnippetPullInputAcceptsSnippetName is the MCP half of the wiring: the
// handler passes input.SnippetName straight through, so a JSON tag that stopped
// matching the "snippet_name" property the schema advertises would drop the
// caller's name silently and let the server derive one instead.
func TestSnippetPullInputAcceptsSnippetName(t *testing.T) {
	var input snippetPullInput
	raw := `{"device":"fw01","name":"Allow HTTPS from LAN to DMZ","snippet_name":"allow-https-lan-dmz","config_type":"RULE"}`

	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if input.Name != "Allow HTTPS from LAN to DMZ" {
		t.Errorf("name = %q, want the match key with its spaces intact", input.Name)
	}
	if input.SnippetName != "allow-https-lan-dmz" {
		t.Errorf("snippet_name = %q, want the caller's chosen name", input.SnippetName)
	}
}

// TestSnippetPullInputOmitsSnippetName: absent means absent, so the service
// leaves the parameter off and the server derives a name.
func TestSnippetPullInputOmitsSnippetName(t *testing.T) {
	var input snippetPullInput
	if err := json.Unmarshal([]byte(`{"device":"fw01","name":"web servers"}`), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if input.SnippetName != "" {
		t.Errorf("snippet_name = %q, want empty when the caller did not supply one", input.SnippetName)
	}
}

// TestNamedSnippetNameParam is the MCP counterpart: the marker becomes the
// parameter an MCP caller can set, and the concrete type stays *service.Error
// so errorResult's type switch still finds the code.
func TestNamedSnippetNameParam(t *testing.T) {
	underivable := &service.Error{
		Code:    service.CodeInvalidInput,
		Message: "Could not derive a snippet name from '@@@###': ... Pass " + service.SnippetNameHint + " to set the snippet name explicitly.",
	}

	got := namedSnippetNameParam(underivable)

	svcErr, ok := got.(*service.Error)
	if !ok {
		t.Fatalf("errorResult switches on the concrete type; got %T", got)
	}
	if strings.Contains(svcErr.Message, service.SnippetNameHint) {
		t.Errorf("the placeholder reached the caller:\n%s", svcErr.Message)
	}
	if !strings.Contains(svcErr.Message, "Pass snippet_name to set") {
		t.Errorf("expected the MCP parameter to be named:\n%s", svcErr.Message)
	}
	if strings.Contains(svcErr.Message, "--name") {
		t.Errorf("an MCP caller has no --name to pass:\n%s", svcErr.Message)
	}
	if svcErr.Code != service.CodeInvalidInput {
		t.Errorf("code = %q, want %q", svcErr.Code, service.CodeInvalidInput)
	}
}
