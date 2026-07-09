package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func newJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestParseResponse_SanitizesTopLevelStringField verifies that a
// server-supplied name containing terminal escape sequences (encoded as
// JSON \u unicode escapes, since raw control bytes are not valid inside
// a JSON string literal per RFC 8259) is scrubbed before it reaches the
// decoded struct. Without the sanitize.Struct pass wired into
// ParseResponse, the ESC byte flows straight through the JSON decode
// into the returned struct, ready to be rendered verbatim by an output
// formatter.
func TestParseResponse_SanitizesTopLevelStringField(t *testing.T) {
	resp := newJSONResponse(`{"name":"evil\u001b[2J\u001b[Hname"}`)

	var target struct {
		Name string `json:"name"`
	}
	if err := ParseResponse(resp, &target); err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}

	if strings.ContainsRune(target.Name, '\x1b') {
		t.Fatalf("Name still contains ESC byte: %q", target.Name)
	}
	if !strings.Contains(target.Name, "evil") || !strings.Contains(target.Name, "name") {
		t.Fatalf("surrounding printable text was not preserved: %q", target.Name)
	}
}

// TestParseResponse_SanitizesNestedSliceField proves the reflect walk
// recurses into nested slices of structs, not just top-level fields.
func TestParseResponse_SanitizesNestedSliceField(t *testing.T) {
	resp := newJSONResponse(`{"items":[{"name":"\u001b]0;pwned\u0007x"}]}`)

	var target struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := ParseResponse(resp, &target); err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}

	if len(target.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(target.Items))
	}
	name := target.Items[0].Name
	if strings.ContainsRune(name, '\x1b') || strings.ContainsRune(name, '\x07') {
		t.Fatalf("nested item name still contains control bytes: %q", name)
	}
	if !strings.Contains(name, "x") {
		t.Fatalf("surrounding printable text was not preserved: %q", name)
	}
}

// TestParseResponseWithStatus_Sanitizes mirrors the ParseResponse case
// for the WithStatus variant used by callers that need the HTTP status
// code alongside the decoded body.
func TestParseResponseWithStatus_Sanitizes(t *testing.T) {
	resp := newJSONResponse(`{"name":"evil\u001b[2Jname"}`)

	var target struct {
		Name string `json:"name"`
	}
	status, err := ParseResponseWithStatus(resp, &target)
	if err != nil {
		t.Fatalf("ParseResponseWithStatus returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if strings.ContainsRune(target.Name, '\x1b') {
		t.Fatalf("Name still contains ESC byte: %q", target.Name)
	}
}

// TestParseResponse_CapsBodySize proves ParseResponse bounds how much of
// the response body it reads. A well-formed JSON array that decodes fine
// at full size is truncated mid-structure once it crosses a (test-lowered)
// maxResponseBodyBytes, so the decode must fail instead of succeeding
// with the whole array buffered in memory.
func TestParseResponse_CapsBodySize(t *testing.T) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < 100; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('1')
	}
	sb.WriteByte(']')
	body := sb.String()

	original := maxResponseBodyBytes
	maxResponseBodyBytes = 64
	defer func() { maxResponseBodyBytes = original }()

	var target []int
	if err := ParseResponse(newJSONResponse(body), &target); err == nil {
		t.Fatal("expected decode error when body exceeds maxResponseBodyBytes, got nil")
	}
}
