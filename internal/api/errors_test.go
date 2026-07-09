package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func newErrorResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

// Terminal control bytes used to craft hostile response bodies below.
// Built via rune(...) rather than a Go escape literal so the test source
// itself never contains a raw control byte.
var (
	testESC = string(rune(0x1b)) // ESC
	testBEL = string(rune(0x07)) // BEL
)

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// TestParseError_SanitizesMessage is REVERT-SENSITIVE: it fails against the
// pre-amendment ParseError, which built APIError.Message straight from the
// response body's "error" field and returned it unsanitized. APIError.Error()
// is what Cobra prints to stderr on a failing command (Cobra does not
// SilenceErrors), so a hostile/misbehaving NDManager returning a 4xx/5xx body
// with a terminal escape sequence in "error" would have it interpreted by the
// operator's terminal.
func TestParseError_SanitizesMessage(t *testing.T) {
	body := mustJSON(t, map[string]string{
		"error": "evil" + testESC + "[2J" + testESC + "[Hmessage",
	})
	resp := newErrorResponse(http.StatusBadRequest, body)

	apiErr := ParseError(resp)

	if strings.ContainsAny(apiErr.Error(), testESC+testBEL) {
		t.Fatalf("APIError.Error() still contains a control byte: %q", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "evil") || !strings.Contains(apiErr.Error(), "message") {
		t.Fatalf("surrounding printable text was not preserved: %q", apiErr.Error())
	}
}

// TestParseError_SanitizesAllFields proves every string-bearing field
// ParseError can populate from a hostile body — Code, Detail,
// BlockingResources, Conflicts (nested struct slice) — is scrubbed, not
// just the top-level Message.
func TestParseError_SanitizesAllFields(t *testing.T) {
	body := mustJSON(t, map[string]interface{}{
		"code":   "VARIABLE_CONFLICT" + testESC + "[2J",
		"error":  "conflict detected",
		"detail": "extra" + testBEL + "context",
		"blocking_resources": []string{
			"fw-a" + testESC + "[31m",
			"fw-b",
		},
		"conflicts": []map[string]string{
			{"variable": "VAR1" + testESC + "[2J", "message": "conflict on VAR1"},
		},
	})
	resp := newErrorResponse(http.StatusConflict, body)

	apiErr := ParseError(resp)

	for name, v := range map[string]string{
		"Code":    apiErr.Code,
		"Message": apiErr.Message,
		"Detail":  apiErr.Detail,
	} {
		if strings.ContainsAny(v, testESC+testBEL) {
			t.Errorf("%s still contains a control byte: %q", name, v)
		}
	}
	for _, r := range apiErr.BlockingResources {
		if strings.ContainsAny(r, testESC+testBEL) {
			t.Errorf("BlockingResources entry still contains a control byte: %q", r)
		}
	}
	for _, c := range apiErr.Conflicts {
		if strings.ContainsAny(c.Variable, testESC+testBEL) || strings.ContainsAny(c.Message, testESC+testBEL) {
			t.Errorf("Conflict still contains a control byte: %+v", c)
		}
	}

	// The VARIABLE_CONFLICT rendering path (Error()) concatenates
	// Message and every Conflicts[].Message — confirm the composed string
	// is clean too, since that's what actually reaches the terminal.
	if strings.ContainsAny(apiErr.Error(), testESC+testBEL) {
		t.Fatalf("APIError.Error() still contains a control byte: %q", apiErr.Error())
	}
}

// TestParseError_SanitizesUndefinedVariables covers the UNDEFINED_VARIABLES
// rendering path, which is a separate branch of Error().
func TestParseError_SanitizesUndefinedVariables(t *testing.T) {
	body := mustJSON(t, map[string]interface{}{
		"code":                "UNDEFINED_VARIABLES",
		"error":               "undefined variables",
		"undefined_variables": []string{"BAD_VAR" + testESC + "[2J", "OTHER_VAR"},
	})
	resp := newErrorResponse(http.StatusUnprocessableEntity, body)

	apiErr := ParseError(resp)

	for _, v := range apiErr.UndefinedVariables {
		if strings.ContainsAny(v, testESC+testBEL) {
			t.Errorf("UndefinedVariables entry still contains a control byte: %q", v)
		}
	}
	if strings.ContainsAny(apiErr.Error(), testESC+testBEL) {
		t.Fatalf("APIError.Error() still contains a control byte: %q", apiErr.Error())
	}
}

// TestParseError_CapsBodySize mirrors TestParseResponse_CapsBodySize: an
// oversized error body must not be buffered without bound. A body larger
// than a (test-lowered) maxResponseBodyBytes gets truncated mid-JSON, so
// json.Unmarshal fails and ParseError falls back to the generic
// status-based message rather than parsing the full body.
func TestParseError_CapsBodySize(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"error":"`)
	for i := 0; i < 200; i++ {
		sb.WriteString("x")
	}
	sb.WriteString(`"}`)
	body := []byte(sb.String())

	original := maxResponseBodyBytes
	maxResponseBodyBytes = 16
	defer func() { maxResponseBodyBytes = original }()

	apiErr := ParseError(newErrorResponse(http.StatusInternalServerError, body))

	if apiErr.Message != statusMessage(http.StatusInternalServerError) {
		t.Fatalf("expected fallback status message for a truncated body, got %q", apiErr.Message)
	}
}
