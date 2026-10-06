package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestTaskDescribeToolExplainsSyncResults: the CLI renders a warning item as
// ⚠ and a moved local rule with both of its sequences. The tool says what
// those items mean, so a model does not report a completed SYNC with a
// placement warning as a failure.
func TestTaskDescribeToolExplainsSyncResults(t *testing.T) {
	description := registeredToolDescription(t, "ndcli.task.describe")
	for _, want := range []string{
		"status warning is informational",
		"error and blocked results are failures",
		"action renumbered",
		"before/after",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("ndcli.task.describe description is missing %q:\n%s", want, description)
		}
	}
}

// TestTaskDescribeReturnsTheSyncResultItems: a SYNC task's message reaches the
// caller with every field of its result items, the warning status, the
// renumbered action and its before/after sequences included, along with the
// local-rule and placement warnings and errors.
func TestTaskDescribeReturnsTheSyncResultItems(t *testing.T) {
	envelope := `{"message": "Rules +1; Local rules ~1 (2 errors)", "results": [` +
		`{"type": "rule", "uuid": "221f3268-0000-4000-8000-0000000000a1", "name": "Allow-HTTPS-from-SOC", "action": "created", "status": "success"},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e01", "name": "Default allow LAN to any rule", "action": "renumbered", "status": "success",` +
		` "code": "RULE_LOCAL_RENUMBERED", "before": ["1"], "after": ["2"]},` +
		`{"type": "rule_placement", "uuid": "221f3268-0000-4000-8000-0000000000a1", "name": "Allow-HTTPS-from-SOC", "action": "warning", "status": "warning",` +
		` "code": "RULE_PREPEND_AFTER_LOCAL", "error": "the local rule \"Floating DNS\" is evaluated first"},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e02", "name": "Allow LAN DNS", "action": "renumber", "status": "warning",` +
		` "code": "RULE_LOCAL_VANISHED", "error": "Local rule \"Allow LAN DNS\" was deleted on the device during this SYNC, so it was not moved"},` +
		`{"type": "rule_local", "uuid": "5f0e64a1-3c3b-4d63-9a51-0d5b6c1f2e03", "name": "Allow LAN NTP", "action": "renumber", "status": "error",` +
		` "code": "RULE_LOCAL_RENUMBER_UNVERIFIED", "error": "Local rule \"Allow LAN NTP\" changed on the device while NetDefense moved it"},` +
		`{"type": "rule_placement", "uuid": "221f3268-0000-4000-8000-0000000000a4", "name": "Allow-VPN-mgmt", "action": "warning", "status": "warning",` +
		` "code": "RULE_SECTION_MISMATCH", "error": "OPNsense ranks it among the floating rules"},` +
		`{"type": "rule_apply", "uuid": "", "name": "apply", "action": "skipped", "status": "error",` +
		` "code": "RULE_APPLY_WITHHELD", "error": "Firewall rules were not applied: \"Allow LAN NTP\" may have been re-created as a pass rule by a renumber and could not be confirmed removed; check the device, then SYNC again"}` +
		`], "validation_errors": []}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/T1" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task": "T1", "type": "SYNC", "status": "COMPLETED", "device_uuid": "dev-uuid",
			"organization": "acme", "message": envelope,
		})
	}))
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	result, err := s.handleTaskDescribe(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"task":"T1"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("tool failed: %+v", resp.Error)
	}
	task := resp.Data.(map[string]any)["task"].(map[string]any)
	message, _ := task["message"].(string)

	var got struct {
		Message string `json:"message"`
		Results []struct {
			Type   string   `json:"type"`
			Action string   `json:"action"`
			Status string   `json:"status"`
			Code   string   `json:"code"`
			Error  string   `json:"error"`
			Before []string `json:"before"`
			After  []string `json:"after"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(message), &got); err != nil {
		t.Fatalf("task message is not the SYNC envelope: %v\n%s", err, message)
	}
	if got.Message != "Rules +1; Local rules ~1 (2 errors)" || len(got.Results) != 7 {
		t.Fatalf("envelope changed on the way through: %+v", got)
	}
	moved, warned := got.Results[1], got.Results[2]
	if moved.Action != "renumbered" || moved.Code != "RULE_LOCAL_RENUMBERED" ||
		strings.Join(moved.Before, ",") != "1" || strings.Join(moved.After, ",") != "2" {
		t.Errorf("renumbered local rule = %+v, want action renumbered, before [1], after [2]", moved)
	}
	if warned.Status != "warning" || warned.Code != "RULE_PREPEND_AFTER_LOCAL" || warned.Error == "" {
		t.Errorf("placement warning = %+v, want status warning with its code and message", warned)
	}
	for i, want := range []struct{ status, code string }{
		{"warning", "RULE_LOCAL_VANISHED"},
		{"error", "RULE_LOCAL_RENUMBER_UNVERIFIED"},
		{"warning", "RULE_SECTION_MISMATCH"},
		{"error", "RULE_APPLY_WITHHELD"},
	} {
		if item := got.Results[3+i]; item.Status != want.status || item.Code != want.code || item.Error == "" {
			t.Errorf("results[%d] = %+v, want status %s, code %s and its message", 3+i, item, want.status, want.code)
		}
	}
}
