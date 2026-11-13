package mcp

import (
	"net/http"
	"testing"
)

// ndcli.org.invite_send reports the role it sent, canonical, not the spelling
// given: `readonly` goes out as RO, and the result says RO.
func TestOrgInviteSendReportsTheCanonicalRole(t *testing.T) {
	cs, seen := firmwareClient(t, http.StatusOK, `{}`)
	resp := callTool(t, cs, "ndcli.org.invite_send", `{"email":"bob@acme.test","role":"readonly"}`)
	if !resp.Success {
		t.Fatalf("%+v", resp)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["role"] != "RO" {
		t.Errorf("data role %v, want RO", data["role"])
	}
	if seen.body["role"] != "RO" {
		t.Errorf("sent role %v, want RO", seen.body["role"])
	}
}
