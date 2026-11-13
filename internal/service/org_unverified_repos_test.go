package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func unverifiedReposServer(t *testing.T, status int, body string) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/allow-unverified-repos" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv)
}

// A write is done only when the server echoes the value sent.
func TestOrgSetUnverifiedReposNeedsTheEcho(t *testing.T) {
	ctx := context.Background()
	if err := unverifiedReposServer(t, http.StatusOK, `{"organization_name":"acme","allow_unverified_repos":true}`).OrgSetUnverifiedRepos(ctx, "acme", true); err != nil {
		t.Errorf("echoed: %v", err)
	}
	for _, body := range []string{`{"organization_name":"acme","allow_unverified_repos":false}`, `{"organization_name":"acme"}`} {
		if err := unverifiedReposServer(t, http.StatusOK, body).OrgSetUnverifiedRepos(ctx, "acme", true); err == nil {
			t.Errorf("%s: no error", body)
		}
	}
}

// A server without the route answers a bare 404: SERVER_TOO_OLD. A coded 404
// is the server's own refusal, kept as it is.
func TestOrgUnverifiedReposOnAServerWithoutTheRoute(t *testing.T) {
	ctx := context.Background()
	_, err := unverifiedReposServer(t, http.StatusNotFound, `{"detail":"Not Found"}`).OrgUnverifiedRepos(ctx, "acme")
	if ErrorCode(err) != CodeServerTooOld {
		t.Errorf("bare 404: %v", err)
	}
	err = unverifiedReposServer(t, http.StatusNotFound, `{"error":"Organization not found","code":"ORG_NOT_FOUND"}`).OrgSetUnverifiedRepos(ctx, "acme", false)
	if ErrorCode(err) != "ORG_NOT_FOUND" {
		t.Errorf("coded 404: %v", err)
	}
}
