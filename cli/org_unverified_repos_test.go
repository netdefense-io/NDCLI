package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// org unverified-repos prints the keys ndcli.org.set_unverified_repos returns,
// and its line in the human formats.
func TestOrgUnverifiedReposPrintsOneJSONDocument(t *testing.T) {
	keys := []string{"organization", "allow_unverified_repos", "action"}
	checkActionCases(t, []actionCase{
		{path: []string{"org", "unverified-repos"}, args: []string{"allow"}, answer: `{"organization_name":"acme","allow_unverified_repos":true}`,
			json: map[string]string{"organization": "acme", "action": "allowed"}, values: map[string]interface{}{"allow_unverified_repos": true},
			mcp: keys, line: "✓ Unverified repositories allowed in acme"},
		{path: []string{"org", "unverified-repos"}, args: []string{"forbid"}, answer: `{"organization_name":"acme","allow_unverified_repos":false}`,
			json: map[string]string{"organization": "acme", "action": "forbidden"}, values: map[string]interface{}{"allow_unverified_repos": false},
			mcp: keys, line: "✓ Unverified repositories forbidden in acme"},
	})
}

// A word other than allow or forbid is refused before anything is sent, and
// the server's refusal of a caller who is not an SU is returned as sent.
func TestOrgUnverifiedReposRefusals(t *testing.T) {
	cmd := findCommand(t, "org", "unverified-repos")
	defer setFlags(t, cmd, map[string]string{"yes": "true"})()
	sent := 0
	refuse := func(w http.ResponseWriter, r *http.Request) {
		sent++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"SU role required to change allow_unverified_repos","code":"INSUFFICIENT_ROLE"}`))
	}
	var err error
	runStreams(t, refuse, "table", "", func() error { err = cmd.RunE(cmd, []string{"maybe"}); return nil })
	if err == nil || sent != 0 {
		t.Errorf("maybe: err %v, %d requests", err, sent)
	}
	stdout, _ := runStreams(t, refuse, "json", "", func() error { err = cmd.RunE(cmd, []string{"allow"}); return nil })
	if api.GetErrorCode(err) != "INSUFFICIENT_ROLE" || !strings.Contains(err.Error(), "SU role required to change allow_unverified_repos") || stdout != "" {
		t.Errorf("refusal: err %v, stdout %q", err, stdout)
	}
}

// org describe shows the setting in every format when it was read, and
// nothing of it when it was not.
func TestOrgDescribeShowsTheUnverifiedReposSetting(t *testing.T) {
	cmd := findCommand(t, "org", "describe")
	describe := func(status int, setting string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/allow-unverified-repos") {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(setting))
				return
			}
			_, _ = w.Write([]byte(`{"name":"acme","status":"ENABLED","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
		}
	}
	read := describe(http.StatusOK, `{"organization_name":"acme","allow_unverified_repos":false}`)
	unread := describe(http.StatusForbidden, `{"error":"no","code":"ACCESS_DENIED"}`)
	for _, format := range []string{"table", "simple", "detailed"} {
		if out, _ := runStreams(t, read, format, "", func() error { return cmd.RunE(cmd, []string{"acme"}) }); !strings.Contains(out, "Unverified repositories: forbidden\n") {
			t.Errorf("%s: %q", format, out)
		}
		if out, _ := runStreams(t, unread, format, "", func() error { return cmd.RunE(cmd, []string{"acme"}) }); strings.Contains(out, "Unverified") {
			t.Errorf("%s, unread: %q", format, out)
		}
	}
	var doc map[string]interface{}
	decodeOne(t, func() string {
		out, _ := runStreams(t, read, "json", "", func() error { return cmd.RunE(cmd, []string{"acme"}) })
		return out
	}(), &doc)
	if doc["allow_unverified_repos"] != false {
		t.Errorf("json %v", doc)
	}
	doc = nil
	decodeOne(t, func() string {
		out, _ := runStreams(t, unread, "json", "", func() error { return cmd.RunE(cmd, []string{"acme"}) })
		return out
	}(), &doc)
	if _, ok := doc["allow_unverified_repos"]; ok {
		t.Errorf("unread json %v", doc)
	}
}
