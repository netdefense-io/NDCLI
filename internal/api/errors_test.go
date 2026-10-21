package api

import (
	"encoding/json"
	"errors"
	"fmt"
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
// is what a failing command prints to stderr (cli.printCommandError), so a
// hostile/misbehaving NDManager returning a 4xx/5xx body with a terminal
// escape sequence in "error" would have it interpreted by the operator's
// terminal.
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

// TestGetErrorCode_LooksThroughWrapping: the service layer hands every API
// error back wrapped, so a type assertion on the outer error never found the
// code NDManager sent.
func TestGetErrorCode_LooksThroughWrapping(t *testing.T) {
	apiErr := ParseError(newErrorResponse(http.StatusUnprocessableEntity, mustJSON(t, map[string]string{
		"error": "'crt' has an RSA key of 1024 bits: use 2048 to 4096 bits",
		"code":  "TRUST_KEY_WEAK",
	})))

	for name, err := range map[string]error{
		"bare":    apiErr,
		"wrapped": fmt.Errorf("create snippet: %w", apiErr),
	} {
		if got := GetErrorCode(err); got != "TRUST_KEY_WEAK" {
			t.Errorf("%s: GetErrorCode = %q, want TRUST_KEY_WEAK", name, got)
		}
	}

	if got := GetErrorCode(errors.New("no API error here")); got != "" {
		t.Errorf("an error without an API error has no code, got %q", got)
	}
	if got := GetErrorCode(nil); got != "" {
		t.Errorf("GetErrorCode(nil) = %q, want empty", got)
	}
}

// TestIsHelpers_LookThroughWrapping: like GetErrorCode, every Is* helper has
// to find the API error inside the service's wrapping. org create passes the
// service error straight to IsRegistrationRestrictedError, so its message
// for a closed registration never showed.
func TestIsHelpers_LookThroughWrapping(t *testing.T) {
	wrapped := func(status int, code string) error {
		return fmt.Errorf("service: %w", &APIError{StatusCode: status, Code: code, Message: "m"})
	}
	for _, tc := range []struct {
		name  string
		is    func(error) bool
		match error
		other error
	}{
		{"IsAuthError", IsAuthError, wrapped(http.StatusUnauthorized, "AUTH_INVALID"), wrapped(http.StatusForbidden, "ACCESS_DENIED")},
		{"IsNotFoundError", IsNotFoundError, wrapped(http.StatusNotFound, "SNIPPET_NOT_FOUND"), wrapped(http.StatusConflict, "ALREADY_EXISTS")},
		{"IsConflictError", IsConflictError, wrapped(http.StatusConflict, "SNIPPET_UUID_IN_USE"), wrapped(http.StatusNotFound, "SNIPPET_NOT_FOUND")},
		{"IsPlanLimitError", IsPlanLimitError, wrapped(http.StatusForbidden, "PLAN_LIMIT_EXCEEDED"), wrapped(http.StatusForbidden, "ACCESS_DENIED")},
		{"IsFeatureNotAvailableError", IsFeatureNotAvailableError, wrapped(http.StatusForbidden, "FEATURE_NOT_AVAILABLE"), wrapped(http.StatusForbidden, "PLAN_LIMIT_EXCEEDED")},
		{"IsRegistrationRestrictedError", IsRegistrationRestrictedError, wrapped(http.StatusForbidden, "REGISTRATION_RESTRICTED"), wrapped(http.StatusForbidden, "ACCESS_DENIED")},
	} {
		if !tc.is(tc.match) {
			t.Errorf("%s missed a wrapped match", tc.name)
		}
		if tc.is(tc.other) || tc.is(errors.New("no API error")) || tc.is(nil) {
			t.Errorf("%s matched an error it should not", tc.name)
		}
	}
}

// TestParseError_CapturesSkippedDevices: a BACKUP_NO_ELIGIBLE_DEVICES refusal
// says, device by device, why nothing was backed up. The list reaches the
// message (so every front-end shows it) and the typed field (so MCP can return
// it as data).
func TestParseError_CapturesSkippedDevices(t *testing.T) {
	body := mustJSON(t, map[string]interface{}{
		"error": "No backup was started: none of the targeted devices can be backed up right now",
		"code":  "BACKUP_NO_ELIGIBLE_DEVICES",
		"skipped": []map[string]string{
			{"device_uuid": "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"},
			{"device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01", "code": "BACKUP_IN_FLIGHT", "reason": "A backup of this device is already waiting to be sent or running"},
		},
	})

	apiErr := ParseError(newErrorResponse(http.StatusConflict, body))

	if apiErr.Code != "BACKUP_NO_ELIGIBLE_DEVICES" {
		t.Fatalf("code = %q", apiErr.Code)
	}
	if len(apiErr.Skipped) != 2 {
		t.Fatalf("skipped = %+v, want 2 entries", apiErr.Skipped)
	}
	if got := apiErr.Skipped[0]; got.DeviceName != "fw-branch-02" || got.Code != "BACKUP_NOT_ENABLED" ||
		got.Reason != "Backup is not enabled for this device" || got.DeviceUUID != "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b" {
		t.Errorf("first entry = %+v", got)
	}
	want := "No backup was started: none of the targeted devices can be backed up right now\n" +
		"  • fw-branch-02: Backup is not enabled for this device\n" +
		"  • fw-branch-01: A backup of this device is already waiting to be sent or running"
	if got := apiErr.Error(); got != want {
		t.Errorf("Error() =\n%s\nwant\n%s", got, want)
	}
}

// TestParseError_EmptySkippedLeavesTheMessageAlone: no device in the
// organization has backup enabled, so the list is empty and the server's own
// sentence stands.
func TestParseError_EmptySkippedLeavesTheMessageAlone(t *testing.T) {
	body := mustJSON(t, map[string]interface{}{
		"error":   "No device in this organization has backup enabled",
		"code":    "BACKUP_NO_ELIGIBLE_DEVICES",
		"skipped": []interface{}{},
	})

	apiErr := ParseError(newErrorResponse(http.StatusConflict, body))

	if len(apiErr.Skipped) != 0 {
		t.Fatalf("skipped = %+v, want none", apiErr.Skipped)
	}
	if got := apiErr.Error(); got != "No device in this organization has backup enabled" {
		t.Errorf("Error() = %q", got)
	}
}

// TestParseError_SanitizesSkippedDevices: the reasons are printed to the
// operator's terminal as sent, so they get the same scrub as every other field.
func TestParseError_SanitizesSkippedDevices(t *testing.T) {
	body := mustJSON(t, map[string]interface{}{
		"error": "No backup was started",
		"code":  "BACKUP_NO_ELIGIBLE_DEVICES",
		"skipped": []map[string]string{{
			"device_uuid": "u1",
			"device_name": "fw" + testESC + "[2J-a",
			"code":        "BACKUP_NOT_ENABLED" + testBEL,
			"reason":      "off" + testESC + "[31m",
		}},
	})

	apiErr := ParseError(newErrorResponse(http.StatusConflict, body))

	if strings.ContainsAny(apiErr.Error(), testESC+testBEL) {
		t.Fatalf("Error() still contains a control byte: %q", apiErr.Error())
	}
	for _, d := range apiErr.Skipped {
		if strings.ContainsAny(d.DeviceName+d.Code+d.Reason, testESC+testBEL) {
			t.Fatalf("skipped entry still contains a control byte: %+v", d)
		}
	}
}
