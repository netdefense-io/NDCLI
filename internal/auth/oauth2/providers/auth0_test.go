package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testESC/testBEL are built via rune(...) rather than a Go escape literal
// so the test source itself never contains a raw control byte.
var (
	testESC = string(rune(0x1b)) // ESC
	testBEL = string(rune(0x07)) // BEL
)

// newTestProvider builds an Auth0Provider pointed at a local TLS test
// server, reusing the server's own client (which trusts its self-signed
// cert) so the provider's real HTTPS code paths run unmodified.
func newTestProvider(srv *httptest.Server) *Auth0Provider {
	return &Auth0Provider{
		domain:     strings.TrimPrefix(srv.URL, "https://"),
		clientID:   "test-client",
		httpClient: srv.Client(),
	}
}

// TestGetUserInfo_SanitizesResponse is REVERT-SENSITIVE: it fails against
// the pre-amendment GetUserInfo, which decoded resp.Body directly via
// json.NewDecoder with no size cap or sanitize.Struct pass. UserInfo.Name/
// Email are printed raw by cli/auth.go's login/delete flows, so a hostile
// or MITM'd Auth0 response embedding a terminal escape sequence in either
// field must be scrubbed before GetUserInfo returns.
func TestGetUserInfo_SanitizesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sub":   "auth0|123",
			"email": "evil" + testESC + "[2Juser@example.com",
			"name":  "boom" + testBEL + "name",
		})
	})
	srv := httptest.NewTLSServer(handler)
	defer srv.Close()

	p := newTestProvider(srv)
	userInfo, err := p.GetUserInfo("test-token")
	if err != nil {
		t.Fatalf("GetUserInfo returned error: %v", err)
	}
	if strings.ContainsAny(userInfo.Email, testESC+testBEL) {
		t.Errorf("Email still contains a control byte: %q", userInfo.Email)
	}
	if strings.ContainsAny(userInfo.Name, testESC+testBEL) {
		t.Errorf("Name still contains a control byte: %q", userInfo.Name)
	}
}

// TestGetUserInfo_ErrorBodyIsSanitized covers the non-200 branch, which
// embeds the raw response body text in the returned error — also printed
// on the terminal (Cobra does not SilenceErrors).
func TestGetUserInfo_ErrorBodyIsSanitized(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("denied" + testESC + "[2Jreason"))
	})
	srv := httptest.NewTLSServer(handler)
	defer srv.Close()

	p := newTestProvider(srv)
	_, err := p.GetUserInfo("test-token")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if strings.ContainsAny(err.Error(), testESC+testBEL) {
		t.Errorf("error still contains a control byte: %q", err.Error())
	}
}

// TestRequestDeviceAuthorization_SanitizesResponse covers the device-code
// flow response; VerificationURIComplete/UserCode are printed to the
// terminal during interactive login (internal/auth/oauth2/interactive.go).
func TestRequestDeviceAuthorization_SanitizesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"device_code":               "dc-1",
			"user_code":                 "ABCD" + testESC + "[2J1234",
			"verification_uri":          "https://example.com/activate",
			"verification_uri_complete": "https://example.com/activate?code=ABCD" + testBEL + "1234",
			"expires_in":                900,
			"interval":                  5,
		})
	})
	srv := httptest.NewTLSServer(handler)
	defer srv.Close()

	p := newTestProvider(srv)
	authResp, err := p.RequestDeviceAuthorization("openid profile")
	if err != nil {
		t.Fatalf("RequestDeviceAuthorization returned error: %v", err)
	}
	if strings.ContainsAny(authResp.UserCode, testESC+testBEL) {
		t.Errorf("UserCode still contains a control byte: %q", authResp.UserCode)
	}
	if strings.ContainsAny(authResp.VerificationURIComplete, testESC+testBEL) {
		t.Errorf("VerificationURIComplete still contains a control byte: %q", authResp.VerificationURIComplete)
	}
}

// TestPollForToken_SanitizesErrorDescription covers the pending/error
// probe branch, whose error_description is folded into the returned error
// text via fmt.Errorf and can reach the terminal.
func TestPollForToken_SanitizesErrorDescription(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":             "invalid_request",
			"error_description": "bad" + testESC + "[2Jdescription",
		})
	})
	srv := httptest.NewTLSServer(handler)
	defer srv.Close()

	p := newTestProvider(srv)
	_, err := p.PollForToken("device-code", 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.ContainsAny(err.Error(), testESC+testBEL) {
		t.Errorf("error still contains a control byte: %q", err.Error())
	}
}
