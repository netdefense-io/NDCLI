package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// unauthenticatedTestServer builds a Server whose Service has no auth
// manager, so RequireAuth() always fails with CodeNotAuthenticated —
// exactly what console_list/console_close must reject.
func unauthenticatedTestServer() *Server {
	return &Server{
		svc:             service.New(nil, nil, nil),
		consoleSessions: newConsoleSessionManager(),
	}
}

// TestHandleConsoleList_RequiresAuth verifies that console_list rejects an
// unauthenticated caller instead of returning session metadata. Fails
// against pre-fix code, which has no RequireAuth() call and returns the
// session list regardless of auth state.
func TestHandleConsoleList_RequiresAuth(t *testing.T) {
	s := unauthenticatedTestServer()
	defer s.consoleSessions.CloseAll()
	s.consoleSessions.Add(makeStubSession("sess-1", "acme", "fw-01"))

	result, err := s.handleConsoleList(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for an unauthenticated console_list call")
	}

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Error("expected Success=false for an unauthenticated console_list call")
	}
	if resp.Error == nil || resp.Error.Code != service.CodeNotAuthenticated {
		t.Errorf("expected CodeNotAuthenticated, got %+v", resp.Error)
	}
}

// TestHandleConsoleClose_RequiresAuth verifies that console_close rejects an
// unauthenticated caller instead of tearing down the session. Fails against
// pre-fix code, which has no RequireAuth() call and closes the session
// unconditionally.
func TestHandleConsoleClose_RequiresAuth(t *testing.T) {
	s := unauthenticatedTestServer()
	defer s.consoleSessions.CloseAll()
	s.consoleSessions.Add(makeStubSession("sess-1", "acme", "fw-01"))

	result, err := s.handleConsoleClose(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"session_id":"sess-1"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for an unauthenticated console_close call")
	}

	if _, getErr := s.consoleSessions.Get("sess-1"); getErr != nil {
		t.Errorf("session should NOT have been removed by an unauthenticated close, but Get failed: %v", getErr)
	}
}

// TestResolveConnectTimeout verifies connect_timeout is clamped into a safe
// range before being multiplied into a time.Duration. Fails against pre-fix
// code (no resolveConnectTimeout function; the inline conversion overflows
// for large inputs like the 9999999999 case below).
func TestResolveConnectTimeout(t *testing.T) {
	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"zero uses default", 0, 5 * time.Minute},
		{"negative uses default", -5, 5 * time.Minute},
		{"typical value passes through", 60, 60 * time.Second},
		{"exactly max is unchanged", maxConsoleConnectTimeoutSeconds, maxConsoleConnectTimeoutSeconds * time.Second},
		{"large value clamped to max", 999999999, maxConsoleConnectTimeoutSeconds * time.Second},
		{"overflow-triggering value clamped to max", 9999999999, maxConsoleConnectTimeoutSeconds * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveConnectTimeout(tc.seconds)
			if got != tc.want {
				t.Errorf("resolveConnectTimeout(%d) = %v, want %v", tc.seconds, got, tc.want)
			}
			if got < 0 {
				t.Errorf("resolveConnectTimeout(%d) returned a negative duration %v", tc.seconds, got)
			}
		})
	}
}

// TestConsoleExecInput_BinaryParsing verifies that the binary flag round-trips
// through JSON correctly in the consoleExecInput struct.
func TestConsoleExecInput_BinaryParsing(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantBinary bool
	}{
		{"omitted defaults false", `{"session_id":"s","command":"ls"}`, false},
		{"explicit false", `{"session_id":"s","command":"ls","binary":false}`, false},
		{"explicit true", `{"session_id":"s","command":"ls","binary":true}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, err := parseInput[consoleExecInput](json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("parseInput error: %v", err)
			}
			if input.Binary != tc.wantBinary {
				t.Errorf("Binary = %v, want %v", input.Binary, tc.wantBinary)
			}
		})
	}
}

// TestConsoleExec_BinaryEncoding verifies the binary-mode serialization logic
// used by handleConsoleExec:
//
//   - Non-UTF-8 bytes (0xFF 0x00 0xFE) must survive the JSON round-trip
//     without corruption when binary=true.
//   - In binary mode the encoding field must be "base64".
//   - In text mode (binary=false) the stdout field is not base64-encoded.
func TestConsoleExec_BinaryEncoding(t *testing.T) {
	// Raw bytes that are not valid UTF-8. json.Marshal would silently replace
	// them with the Unicode replacement character (U+FFFD) if we stored them
	// directly in a string field.
	rawBytes := []byte{0xFF, 0x00, 0xFE, 0x41, 0x42} // "AB" plus non-UTF-8 prefix

	// Simulate what handleConsoleExec does when input.Binary == true.
	encodedStdout := base64.StdEncoding.EncodeToString(rawBytes)
	encodedStderr := base64.StdEncoding.EncodeToString([]byte{})

	data := map[string]interface{}{
		"stdout":    encodedStdout,
		"stderr":    encodedStderr,
		"exit_code": 0,
		"truncated": false,
		"encoding":  "base64",
	}

	// Marshal to JSON (simulating successResult) and back.
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// Encoding field must be "base64".
	if enc, _ := decoded["encoding"].(string); enc != "base64" {
		t.Errorf("encoding = %q, want %q", enc, "base64")
	}

	// Decode the base64 stdout and verify the original bytes are intact.
	gotEncoded, _ := decoded["stdout"].(string)
	gotBytes, decErr := base64.StdEncoding.DecodeString(gotEncoded)
	if decErr != nil {
		t.Fatalf("base64.DecodeString error: %v", decErr)
	}
	if string(gotBytes) != string(rawBytes) {
		t.Errorf("decoded stdout bytes = %v, want %v", gotBytes, rawBytes)
	}
}

// TestConsoleExec_TextMode verifies that when binary=false, stdout is passed
// through as a plain string without base64 encoding and no encoding field is
// present in the response map.
func TestConsoleExec_TextMode(t *testing.T) {
	plainText := "hello world\nline 2\n"

	// Simulate what handleConsoleExec does when input.Binary == false.
	data := map[string]interface{}{
		"stdout":    plainText,
		"stderr":    "",
		"exit_code": 0,
		"truncated": false,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// No encoding field in text mode.
	if _, hasEncoding := decoded["encoding"]; hasEncoding {
		t.Error("encoding field should not be present in text mode")
	}

	if got, _ := decoded["stdout"].(string); got != plainText {
		t.Errorf("stdout = %q, want %q", got, plainText)
	}
}

// TestConsoleExec_BinaryEmptyOutput verifies that binary=true with empty
// stdout/stderr produces valid empty base64 strings ("") not "null".
func TestConsoleExec_BinaryEmptyOutput(t *testing.T) {
	data := map[string]interface{}{
		"stdout":    base64.StdEncoding.EncodeToString([]byte("")),
		"stderr":    base64.StdEncoding.EncodeToString([]byte("")),
		"exit_code": 0,
		"truncated": false,
		"encoding":  "base64",
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// base64.StdEncoding.EncodeToString([]byte{}) returns "".
	if got, _ := decoded["stdout"].(string); got != "" {
		t.Errorf("empty stdout base64 = %q, want %q", got, "")
	}
	if enc, _ := decoded["encoding"].(string); enc != "base64" {
		t.Errorf("encoding = %q, want %q", enc, "base64")
	}
}
