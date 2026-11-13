package mcp

import (
	"net/http"
	"testing"
)

// With nothing pending, ndcli.device.approve_all answers the same four keys,
// with the same types, as when it approves something: failed is a list, never
// a count, and `ndcli device approve-all -f json` prints the same shape.
func TestDeviceApproveAllWithNothingPendingKeepsItsShape(t *testing.T) {
	cs, _ := firmwareClient(t, http.StatusOK, `{"items":[],"total":0}`)
	resp := callTool(t, cs, "ndcli.device.approve_all", `{}`)
	if !resp.Success {
		t.Fatalf("%+v", resp)
	}
	data, _ := resp.Data.(map[string]interface{})
	names, okNames := data["approved_names"].([]interface{})
	failed, okFailed := data["failed"].([]interface{})
	if data["approved"] != float64(0) || data["failed_count"] != float64(0) || !okNames || !okFailed || len(names)+len(failed) != 0 {
		t.Errorf("data %v", data)
	}
	if _, present := data["devices"]; present {
		t.Errorf("data %v still carries devices", data)
	}
}
