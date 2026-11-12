package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Both device tools return `online`, tri-state as NDManager sends it: null is
// "the control plane could not tell", never offline (#314).
func TestDeviceToolsReturnOnline(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name   string
		online *bool
		want   string
	}{
		{"online", &yes, `"online":true`},
		{"offline", &no, `"online":false`},
		{"unknown", nil, `"online":null`},
	} {
		d := &models.Device{Name: "fw-01", Online: tc.online}
		for tool, m := range map[string]map[string]interface{}{"deviceSummary": deviceSummary(d), "deviceFull": deviceFull(d)} {
			if _, ok := m["online"]; !ok {
				t.Errorf("%s/%s: no online key", tool, tc.name)
				continue
			}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("%s/%s: %v", tool, tc.name, err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Errorf("%s/%s: %s does not contain %s", tool, tc.name, raw, tc.want)
			}
		}
	}
}

func TestDeviceToolDescriptionsExplainOnline(t *testing.T) {
	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{"ndcli.device.list": true, "ndcli.device.describe": true, "ndcli.ou.device_list": true}
	seen := 0
	for _, tool := range result.Tools {
		if want[tool.Name] {
			seen++
			if !strings.Contains(tool.Description, deviceOnlineDescription) {
				t.Errorf("%s description does not explain online", tool.Name)
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("found %d of the %d tools that return online", seen, len(want))
	}
}

// ndcli.device.health keeps a null `online`: the telemetry model no longer
// drops it, so "could not tell" stays apart from "not sent".
func TestDeviceHealthKeepsANullOnline(t *testing.T) {
	raw, err := json.Marshal(models.DeviceTelemetryResponse{Name: "fw-01"})
	if err != nil || !strings.Contains(string(raw), `"online":null`) {
		t.Errorf("telemetry %s (%v), want online null", raw, err)
	}
	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range result.Tools {
		if tool.Name == "ndcli.device.health" {
			found = true
			if !strings.Contains(tool.Description, deviceOnlineMeaning) {
				t.Errorf("ndcli.device.health description does not explain online")
			}
		}
	}
	if !found {
		t.Error("ndcli.device.health not registered")
	}
}

// The ndcli://org/{name}/devices resource carries `online` like the device
// tools, null included, and its description says what it means.
func TestOrgDevicesResourceReturnsOnline(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/devices" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("fetched %s?%s, want the org's devices at per_page=100", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"devices": [{"name": "fw-01", "online": true}, {"name": "fw-02", "online": null}], "total": 2}`)
	}))
	t.Cleanup(api.Close)
	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	res, err := s.handleOrgDevicesResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "ndcli://org/acme/devices"}})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Devices []map[string]json.RawMessage `json:"devices"`
	}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &body); err != nil || len(body.Devices) != 2 {
		t.Fatalf("resource %s: %v", res.Contents[0].Text, err)
	}
	for i, want := range []string{"true", "null"} {
		if got, ok := body.Devices[i]["online"]; !ok || string(got) != want {
			t.Errorf("device %d: online %s (present %v), want %s", i, got, ok, want)
		}
	}

	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	templates, err := session.ListResourceTemplates(context.Background(), &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	found := false
	for _, tmpl := range templates.ResourceTemplates {
		if tmpl.URITemplate == "ndcli://org/{name}/devices" {
			found = true
			if !strings.Contains(tmpl.Description, deviceOnlineDescription) {
				t.Errorf("devices resource description does not explain online: %q", tmpl.Description)
			}
		}
	}
	if !found {
		t.Error("devices resource template not listed")
	}
}
