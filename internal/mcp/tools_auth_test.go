package mcp

import "testing"

// TestMcpTokenSummary_StripsSubject verifies that mcpTokenSummary removes
// the OAuth2 "subject" claim while preserving the fields agents need
// (email/name/expires_at). Fails to even compile against pre-fix code,
// which has no mcpTokenSummary function and passes the raw
// AuthTokenSummary() map (subject included) straight into the MCP response.
func TestMcpTokenSummary_StripsSubject(t *testing.T) {
	raw := map[string]interface{}{
		"subject":    "auth0|abc123",
		"email":      "user@example.com",
		"name":       "User Name",
		"expires_at": "2026-08-01T00:00:00Z",
	}
	got := mcpTokenSummary(raw)

	if _, ok := got["subject"]; ok {
		t.Error("subject key present in MCP token summary, want stripped")
	}
	for _, key := range []string{"email", "name", "expires_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("expected %q to survive stripping, got %v", key, got)
		}
	}

	// mcpTokenSummary must not mutate its input.
	if _, ok := raw["subject"]; !ok {
		t.Error("mcpTokenSummary mutated the input map (subject removed from original)")
	}
}

// TestMcpTokenSummary_NilInput verifies the nil-safe fallback (e.g. when the
// caller has no stored tokens at all).
func TestMcpTokenSummary_NilInput(t *testing.T) {
	if got := mcpTokenSummary(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}
