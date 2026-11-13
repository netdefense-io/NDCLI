package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Under NDCLI_TOKEN, auth delete is refused before any request with the way
// forward, as token create and revoke are. It used to fail with "no cached
// user info for static-token auth — use 'auth me' instead", the wrong remedy:
// the server refuses a token there anyway (INTERACTIVE_LOGIN_REQUIRED).
func TestAuthDeleteUnderAStaticTokenNamesTheLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing may be sent: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	home, conf := confFor(t, srv)
	stdout, stderr, code := runNdcli(t, home, "", "--conf", conf, "auth", "delete", "--yes")
	if code != 1 || stdout != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing", code, stdout)
	}
	if !strings.Contains(stderr, "auth delete requires interactive authentication — unset NDCLI_TOKEN and log in with 'ndcli auth login'") {
		t.Errorf("stderr = %q", stderr)
	}
}
