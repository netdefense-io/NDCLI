package mcp

import (
	"net/http"
	"testing"
)

// ttl_seconds is checked before it becomes a duration: 18446744074 used to
// wrap to about 0.29 s and go out as ttl_seconds 0.
func TestRebindTokenRefusesATTLOutOfRange(t *testing.T) {
	for _, args := range []string{`"ttl_seconds":-1,"confirm":true`, `"ttl_seconds":604801,"confirm":true`, `"ttl_seconds":18446744074,"confirm":true`, `"ttl_seconds":18446744074`} {
		ttl := args
		cs, seen := firmwareClient(t, http.StatusOK, `{"token":"t","expires_at":"2026-11-13T00:00:00Z"}`)
		resp := callTool(t, cs, "ndcli.device.rebind_token", `{"device":"fw-01",`+args+`}`)
		if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" || resp.Error.Message != "ttl_seconds must be between 1 and 604800 (omit it for 86400)" {
			t.Errorf("ttl_seconds %s: %+v, want INVALID_INPUT", ttl, resp)
		}
		if seen.hits != 0 {
			t.Errorf("ttl_seconds %s: sent %s %s", ttl, seen.method, seen.path)
		}
	}
	cs, seen := firmwareClient(t, http.StatusOK, `{"token":"t","expires_at":"2026-11-13T00:00:00Z"}`)
	callTool(t, cs, "ndcli.device.rebind_token", `{"device":"fw-01","ttl_seconds":604800,"confirm":true}`)
	if seen.hits != 1 || seen.body["ttl_seconds"] != float64(604800) {
		t.Errorf("ttl_seconds 604800: %d request(s), body %v; want it sent", seen.hits, seen.body)
	}
}
