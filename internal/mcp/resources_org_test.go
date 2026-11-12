package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The ndcli://org/{name} resource reads the organization through the service
// layer (#416), as ndcli.org.describe does, so a server built with only a
// service answers it.
func TestOrgResourceReadsThroughTheService(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme" {
			t.Errorf("fetched %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name": "acme", "status": "ENABLED", "default_ou": "edge", "device_count": 3}`)
	}))
	t.Cleanup(api.Close)
	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	res, err := s.handleOrgResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "ndcli://org/acme"}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "acme" || body["status"] != "ENABLED" || body["default_ou"] != "edge" || body["device_count"] != float64(3) {
		t.Errorf("resource = %v", body)
	}
}
