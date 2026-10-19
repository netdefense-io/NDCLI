package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// An NDManager 1.100 task (who asked, from which client, the run, dispatch and
// completion) and a 1.99 one (where it came from, how its device stands).
const (
	mcpTask1100 = `{"task": "Pq3Lm8Xa", "device_uuid": "7d0c1f9e-3b2a-4c5d-8e6f-0a1b2c3d4e5f",
		"device_name": "e2e-a", "device_deleted": false, "organization": "acme-corp", "type": "PING",
		"status": "COMPLETED", "message": "pong", "created_at": "2026-10-18T14:02:11Z",
		"updated_at": "2026-10-18T14:04:17Z", "expires_at": "2026-10-18T14:07:11Z", "scheduled_at": null,
		"origin": "MANUAL", "schedule": null, "device_online": null, "device_last_seen_at": null,
		"last_response_at": "2026-10-18T14:04:17Z",
		"requested_by": {"email": "ana@acme.com", "name": "Ana Lima"}, "client": "CLI", "run_id": "R7x2Kp9Q",
		"dispatched_at": "2026-10-18T14:02:12Z", "completed_at": "2026-10-18T14:04:17Z"}`

	mcpTask199 = `{"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"device_name": "fw-branch-01", "device_deleted": false, "organization": "acme-corp", "type": "BACKUP",
		"status": "PENDING", "message": null, "created_at": "2026-10-18T03:00:04Z",
		"updated_at": "2026-10-18T03:00:04Z", "expires_at": "2026-10-19T03:00:04Z", "scheduled_at": null,
		"origin": "SCHEDULE",
		"schedule": {"name": "nightly-backup", "spec_code": "Bk7Qm2Zp", "kind": "BACKUP",
			"next_run_at": "2026-10-19T03:00:00Z", "firmware_track": null},
		"device_online": false, "device_last_seen_at": "2026-10-17T22:41:10Z", "last_response_at": null}`
)

// taskToolData runs a task tool handler against a fake API answering body and
// returns its data.
func taskToolData(t *testing.T, handler func(*Server) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error), args, body string) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme-corp")
	result, err := handler(s)(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("tool failed: %+v", resp.Error)
	}
	// Through JSON, as the caller reads it.
	raw, _ := json.Marshal(resp.Data)
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func assertTaskProvenance1100(t *testing.T, task map[string]any) {
	t.Helper()
	for key, want := range map[string]any{
		"origin": "MANUAL", "client": "CLI", "run_id": "R7x2Kp9Q", "device_deleted": false,
		"dispatched_at": "2026-10-18T14:02:12Z", "completed_at": "2026-10-18T14:04:17Z",
		"duration_seconds": float64(125), "last_response_at": "2026-10-18T14:04:17Z",
	} {
		if task[key] != want {
			t.Errorf("%s = %#v, want %#v", key, task[key], want)
		}
	}
	if r, _ := task["requested_by"].(map[string]any); r["email"] != "ana@acme.com" || r["name"] != "Ana Lima" {
		t.Errorf("requested_by = %#v", task["requested_by"])
	}
	for _, key := range []string{"schedule", "device_online", "device_last_seen_at"} {
		if v, ok := task[key]; !ok || v != nil {
			t.Errorf("%s = %#v (present %v), want null", key, v, ok)
		}
	}
	// started_at was never sent by NDManager; dispatched_at replaces it.
	if _, ok := task["started_at"]; ok {
		t.Errorf("started_at = %#v, want no such key", task["started_at"])
	}
}

func TestTaskToolsReturnWhoAskedAndWhenItRan(t *testing.T) {
	list := taskToolData(t, func(s *Server) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleTaskList
	}, `{}`, `{"items": [`+mcpTask1100+`, `+mcpTask199+`], "total": 2, "page": 1, "per_page": 30, "pages": 1}`)
	tasks, _ := list["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %#v", list["tasks"])
	}
	assertTaskProvenance1100(t, tasks[0].(map[string]any))

	pending := tasks[1].(map[string]any)
	if pending["origin"] != "SCHEDULE" || pending["device_online"] != false ||
		pending["device_last_seen_at"] != "2026-10-17T22:41:10Z" {
		t.Errorf("a 1.99 waiting task = %#v", pending)
	}
	if s, _ := pending["schedule"].(map[string]any); s["name"] != "nightly-backup" || s["spec_code"] != "Bk7Qm2Zp" ||
		s["kind"] != "BACKUP" || s["next_run_at"] != "2026-10-19T03:00:00Z" || s["firmware_track"] != nil {
		t.Errorf("schedule = %#v", pending["schedule"])
	}
	for _, key := range []string{"requested_by", "client", "run_id", "dispatched_at", "completed_at", "duration_seconds"} {
		if v, ok := pending[key]; !ok || v != nil {
			t.Errorf("1.99 %s = %#v (present %v), want null", key, v, ok)
		}
	}

	described := taskToolData(t, func(s *Server) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleTaskDescribe
	}, `{"task":"Pq3Lm8Xa"}`, mcpTask1100)
	assertTaskProvenance1100(t, described["task"].(map[string]any))
}

func TestTaskToolDescriptionsExplainTheProvenanceFields(t *testing.T) {
	for _, tool := range []string{"ndcli.task.list", "ndcli.task.describe"} {
		description := registeredToolDescription(t, tool)
		for _, want := range []string{
			"origin (MANUAL, SCHEDULE or FIRMWARE)", "spec_code", "device_online", "device_last_seen_at",
			"requested_by", "both null once the account is deleted", "client (WEB, CLI, MCP or API",
			"run_id", "dispatched_at", "completed_at", "duration_seconds", "last_response_at",
		} {
			if !strings.Contains(description, want) {
				t.Errorf("%s description is missing %q", tool, want)
			}
		}
	}
}

// TestTaskToolsFromAnOlderNDManager: a task without any of the new fields
// still lists, with each of the new keys null.
func TestTaskToolsFromAnOlderNDManager(t *testing.T) {
	old := `{"task": "Ab12Cd34", "device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01",
		"organization": "acme-corp", "type": "PING", "status": "COMPLETED", "created_at": "2026-10-18T03:00:04"}`
	described := taskToolData(t, func(s *Server) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleTaskDescribe
	}, `{"task":"Ab12Cd34"}`, old)
	task := described["task"].(map[string]any)
	for _, key := range []string{"origin", "schedule", "requested_by", "client", "run_id", "dispatched_at",
		"completed_at", "duration_seconds", "device_online", "device_last_seen_at", "last_response_at"} {
		if v, ok := task[key]; !ok || v != nil {
			t.Errorf("%s = %#v (present %v), want null", key, v, ok)
		}
	}
	if task["created_at"] != "2026-10-18T03:00:04Z" {
		t.Errorf("created_at = %#v", task["created_at"])
	}
}

func TestTaskListToolSchemaHasTheNewFilters(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.task.list")
	if got := schemaPropertyItemsEnum(t, schema, "origin"); !reflect.DeepEqual(got, models.TaskOrigins) {
		t.Errorf("origin enum = %v, want models.TaskOrigins %v", got, models.TaskOrigins)
	}
	if got := schemaPropertyItemsEnum(t, schema, "exclude_type"); !reflect.DeepEqual(got, models.TaskTypes) {
		t.Errorf("exclude_type enum = %v, want models.TaskTypes %v", got, models.TaskTypes)
	}
	props := schema["properties"].(map[string]any)
	for name, kind := range map[string]string{
		"schedule": "string", "device_exact": "array", "search": "string", "scheduled_after": "string",
		"scheduled_before": "string", "include_active": "boolean", "run_id": "string", "requested_by": "string",
	} {
		prop, ok := props[name].(map[string]any)
		if !ok || prop["type"] != kind {
			t.Errorf("%s = %#v, want a %s property", name, props[name], kind)
		}
	}
}

// filteredTaskListCall runs ndcli.task.list with args against a control plane
// echoing filters, and returns the query and the response.
func filteredTaskListCall(t *testing.T, filters map[string]any, args string) (map[string][]string, ToolResponse) {
	t.Helper()
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		body := map[string]any{"items": []any{}, "total": 0, "page": 1, "per_page": 30, "pages": 0}
		if filters != nil {
			body["filters"] = filters
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme-corp")
	result, err := s.handleTaskList(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	return query, resp
}

func TestTaskListToolSendsTheNewFilters(t *testing.T) {
	echo := map[string]any{}
	for _, key := range []string{"exclude_type", "origin", "schedule", "device", "q", "scheduled_after",
		"scheduled_before", "include_active", "run_id", "requested_by"} {
		echo[key] = nil
	}
	query, resp := filteredTaskListCall(t, echo, `{"exclude_type":"CONNECT","origin":["schedule"],"schedule":"nightly-backup",
		"device_exact":["e2e-a","e2e-b"],"search":"Pq3L","scheduled_after":"2026-10-18T09:00:00","scheduled_before":"+1h",
		"include_active":true,"run_id":"R7x2Kp9Q","requested_by":"ana@acme.com"}`)
	if !resp.Success {
		t.Fatalf("tool failed: %+v", resp.Error)
	}
	for key, want := range map[string][]string{
		"exclude_type": {"CONNECT"}, "origin": {"SCHEDULE"}, "schedule": {"nightly-backup"},
		"device": {"e2e-a", "e2e-b"}, "q": {"Pq3L"}, "include_active": {"true"},
		"run_id": {"R7x2Kp9Q"}, "requested_by": {"ana@acme.com"},
		// A time without an offset is UTC.
		"scheduled_after": {"2026-10-18T09:00:00Z"},
	} {
		if got := query[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s sent as %v, want %v", key, got, want)
		}
	}
	if len(query["scheduled_before"]) != 1 || !strings.HasSuffix(query["scheduled_before"][0], "Z") {
		t.Errorf("scheduled_before sent as %v, want one UTC instant", query["scheduled_before"])
	}
}

func TestTaskListToolRefusesWhatAnOlderNDManagerIgnored(t *testing.T) {
	_, resp := filteredTaskListCall(t, map[string]any{"organization": "acme-corp", "sort_by": "created_at:desc"},
		`{"origin":["MANUAL"],"run_id":"R7x2Kp9Q"}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != "SERVER_TOO_OLD" ||
		resp.Error.Message != "this NDManager predates the task list filters origin, run_id: it did not apply them, so no list is shown" {
		t.Errorf("response = %+v, want SERVER_TOO_OLD naming origin and run_id", resp)
	}
	if resp.Data != nil {
		t.Errorf("a list came back with the refusal: %#v", resp.Data)
	}
}
