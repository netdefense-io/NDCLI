package mcp

import (
	"encoding/json"
	"testing"
)

// TestNetPrefixInputAcceptsValue guards the MCP half of the same silent
// failure mode: handleNetworkPrefixAdd branches on len(input.Value), so if the
// JSON tag stopped matching the "value" property the schema advertises, a
// caller passing CIDRs would have them dropped and get the old
// variable-must-exist behaviour with no error anywhere.
//
// This covers the struct side. The schema property name itself is declared in
// the AddTool call and is not reachable from a test without standing up a
// server, so the two are kept adjacent in tools_network.go on purpose.
func TestNetPrefixInputAcceptsValue(t *testing.T) {
	var input netPrefixKeyInput
	raw := `{"network":"hq","device":"fw01","variable":"lan","value":["10.20.0.0/24","10.21.0.0/24"]}`

	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(input.Value) != 2 {
		t.Fatalf("value = %v, want the two CIDRs the caller sent; the provisioning branch keys on this being non-empty", input.Value)
	}
	if input.Value[0] != "10.20.0.0/24" || input.Value[1] != "10.21.0.0/24" {
		t.Errorf("value = %v", input.Value)
	}
}
