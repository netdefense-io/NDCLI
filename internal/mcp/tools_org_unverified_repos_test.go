package mcp

import (
	"net/http"
	"strings"
	"testing"
)

// set_unverified_repos previews unless confirm=true, sends the value with it,
// and requires allow rather than reading a missing one as false.
func TestOrgSetUnverifiedReposPreviewsThenSends(t *testing.T) {
	cs, seen := firmwareClient(t, http.StatusOK, `{"organization_name":"acme","allow_unverified_repos":true}`)

	resp := callTool(t, cs, "ndcli.org.set_unverified_repos", `{"allow":true}`)
	if !resp.Success || seen.hits != 0 {
		t.Fatalf("preview: %+v, %d requests", resp, seen.hits)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["note"] == nil || !strings.Contains(resp.Message, "unsigned or non-https") {
		t.Errorf("the preview does not say what follows: %+v", resp)
	}

	resp = callTool(t, cs, "ndcli.org.set_unverified_repos", `{"allow":true,"confirm":true}`)
	if !resp.Success {
		t.Fatalf("%+v", resp)
	}
	if seen.method != http.MethodPut || seen.path != "/api/v1/organizations/acme/allow-unverified-repos" || seen.body["enabled"] != true {
		t.Errorf("sent %s %s %v", seen.method, seen.path, seen.body)
	}
	data, _ = resp.Data.(map[string]interface{})
	if data["organization"] != "acme" || data["allow_unverified_repos"] != true || data["action"] != "allowed" {
		t.Errorf("data %v", data)
	}

	hits := seen.hits
	resp = callTool(t, cs, "ndcli.org.set_unverified_repos", `{"confirm":true}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" || seen.hits != hits {
		t.Errorf("without allow: %+v, %d requests", resp, seen.hits-hits)
	}
}

// The server's refusal of a caller who is not an SU comes back as sent.
func TestOrgSetUnverifiedReposReturnsTheRefusalAsSent(t *testing.T) {
	cs, _ := firmwareClient(t, http.StatusForbidden, `{"error":"SU role required to change allow_unverified_repos","code":"INSUFFICIENT_ROLE"}`)
	resp := callTool(t, cs, "ndcli.org.set_unverified_repos", `{"allow":false,"confirm":true}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != "INSUFFICIENT_ROLE" ||
		!strings.Contains(resp.Error.Message, "SU role required to change allow_unverified_repos") {
		t.Errorf("%+v", resp.Error)
	}
}

// describe carries the setting, and null when it could not be read.
func TestOrgDescribeCarriesTheUnverifiedReposSetting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting string
		status  int
		want    interface{}
	}{
		{"allowed", `{"organization_name":"acme","allow_unverified_repos":true}`, http.StatusOK, true},
		{"forbidden", `{"organization_name":"acme","allow_unverified_repos":false}`, http.StatusOK, false},
		{"unread", `{"detail":"Not Found"}`, http.StatusNotFound, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := handlerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/allow-unverified-repos") {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.setting))
					return
				}
				_, _ = w.Write([]byte(`{"name":"acme","status":"ENABLED"}`))
			}))
			resp := callTool(t, cs, "ndcli.org.describe", `{"name":"acme"}`)
			data, _ := resp.Data.(map[string]interface{})
			org, _ := data["organization"].(map[string]interface{})
			got, present := org["allow_unverified_repos"]
			if !resp.Success || !present || got != tc.want {
				t.Errorf("%+v: allow_unverified_repos %v (present %v), want %v", resp, got, present, tc.want)
			}
		})
	}
}
