package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ndcli.device.health asks for retained telemetry only when told, and its
// description says what the OPNsense fields and staleness mean (#256).
func TestDeviceHealthTakesRetained(t *testing.T) {
	body := `{"name":"fw-01","status":"ENABLED","online":false,"sync":{"state":"in_sync"},"opnsense_version":"26.7.8_2","opnsense_update":"minor","opnsense_latest":"26.7.9","stale":true,"snapshot_age_sec":7200,"as_of":1}`
	for _, tc := range []struct {
		args, query string
	}{
		{`{"device":"fw-01"}`, ""},
		{`{"device":"fw-01","retained":true}`, "retained=true"},
	} {
		cs, seen := firmwareClient(t, http.StatusOK, body)
		resp := callTool(t, cs, "ndcli.device.health", tc.args)
		if !resp.Success {
			t.Fatalf("%s: %+v", tc.args, resp)
		}
		if seen.path != "/api/v1/organizations/acme/devices/fw-01/telemetry" || seen.query.Encode() != tc.query {
			t.Errorf("%s: requested %s?%s, want ?%s", tc.args, seen.path, seen.query.Encode(), tc.query)
		}
		data, _ := resp.Data.(map[string]interface{})
		if data["opnsense_update"] != "minor" || data["stale"] != true {
			t.Errorf("%s: data %v", tc.args, data)
		}
	}

	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range result.Tools {
		if tool.Name != "ndcli.device.health" {
			continue
		}
		for _, want := range []string{deviceHealthOPNsense, deviceHealthRetained} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("description lacks %q", want)
			}
		}
		raw, _ := tool.InputSchema.(map[string]interface{})
		props, _ := raw["properties"].(map[string]interface{})
		if _, ok := props["retained"]; !ok {
			t.Errorf("schema has no retained: %v", tool.InputSchema)
		}
		return
	}
	t.Error("ndcli.device.health not registered")
}
