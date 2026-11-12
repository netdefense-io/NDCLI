package mcp

import (
	"context"
	"encoding/json"
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
