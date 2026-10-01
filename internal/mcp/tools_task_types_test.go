package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// schemaPropertyItemsEnum is schemaPropertyEnum for an array property: it
// returns schema.properties[property].items.enum as a []string.
func schemaPropertyItemsEnum(t *testing.T, schema map[string]any, property string) []string {
	t.Helper()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties object: %#v", schema)
	}
	prop, ok := props[property].(map[string]any)
	if !ok {
		t.Fatalf("schema has no %q property: %#v", property, props)
	}
	if prop["type"] != "array" {
		t.Fatalf("%q property type = %v, want array", property, prop["type"])
	}
	items, ok := prop["items"].(map[string]any)
	if !ok {
		t.Fatalf("%q property has no items schema: %#v", property, prop)
	}
	rawEnum, ok := items["enum"].([]any)
	if !ok {
		t.Fatalf("%q items have no enum: %#v", property, items)
	}
	out := make([]string, len(rawEnum))
	for i, v := range rawEnum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%q enum value %v is not a string", property, v)
		}
		out[i] = s
	}
	return out
}

// TestTaskListEnumsUseCanonicalLists guards the ndcli.task.list "type" and
// "status" items against drifting from models.TaskTypes / models.TaskStatuses.
func TestTaskListEnumsUseCanonicalLists(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.task.list")

	if got := schemaPropertyItemsEnum(t, schema, "type"); !reflect.DeepEqual(got, models.TaskTypes) {
		t.Errorf("ndcli.task.list type enum = %v, want models.TaskTypes %v", got, models.TaskTypes)
	}
	if got := schemaPropertyItemsEnum(t, schema, "status"); !reflect.DeepEqual(got, models.TaskStatuses) {
		t.Errorf("ndcli.task.list status enum = %v, want models.TaskStatuses %v", got, models.TaskStatuses)
	}
}

// TestTaskListInputReadsAnArrayOrABareString: the schema now says array, but a
// client (or a saved prompt) written against the string-typed one still sends
// a bare value, and that must keep working.
func TestTaskListInputReadsAnArrayOrABareString(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		want    stringList
		wantErr bool
	}{
		{name: "array", raw: `{"type":["SYNC","BACKUP"]}`, want: stringList{"SYNC", "BACKUP"}},
		{name: "bare string", raw: `{"type":"SYNC"}`, want: stringList{"SYNC"}},
		{name: "empty array", raw: `{"type":[]}`, want: stringList{}},
		{name: "null", raw: `{"type":null}`, want: nil},
		{name: "absent", raw: `{}`, want: nil},
		{name: "a number", raw: `{"type":5}`, wantErr: true},
		{name: "an object", raw: `{"type":{"a":"b"}}`, wantErr: true},
		{name: "a non-string element", raw: `{"type":["SYNC",5]}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := parseInput[taskListInput](json.RawMessage(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseInput(%s) accepted %v", tc.raw, input.Type)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInput(%s): %v", tc.raw, err)
			}
			if !reflect.DeepEqual(input.Type, tc.want) {
				t.Errorf("type = %#v, want %#v", input.Type, tc.want)
			}
		})
	}
}

// taskListToolCall runs the ndcli.task.list handler against a fake API and
// returns the query it received (nil when no request was made) and the tool's
// response.
func taskListToolCall(t *testing.T, args string) (query map[string][]string, resp ToolResponse) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []any{map[string]any{"task": "T1", "type": "SYNC", "status": "FAILED"}},
			"total": 1, "page": 1, "per_page": 30, "pages": 1,
		})
	}))
	defer srv.Close()

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	result, err := s.handleTaskList(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	decodeToolResult(t, result, &resp)
	return query, resp
}

func TestTaskListToolSendsRepeatedFilters(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		wantType   []string
		wantStatus []string
	}{
		{"arrays", `{"type":["SYNC","backup"],"status":["FAILED","EXPIRED"]}`,
			[]string{"SYNC", "BACKUP"}, []string{"FAILED", "EXPIRED"}},
		{"bare strings still work", `{"type":"SYNC","status":"cancelled"}`,
			[]string{"SYNC"}, []string{"CANCELED"}},
		{"a comma list in one element", `{"type":["SYNC,BACKUP"]}`,
			[]string{"SYNC", "BACKUP"}, nil},
		{"no filters", `{}`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, resp := taskListToolCall(t, tc.args)
			if !resp.Success {
				t.Fatalf("tool failed: %+v", resp.Error)
			}
			if !reflect.DeepEqual(query["type"], tc.wantType) {
				t.Errorf("type sent as %v, want %v", query["type"], tc.wantType)
			}
			if !reflect.DeepEqual(query["status"], tc.wantStatus) {
				t.Errorf("status sent as %v, want %v", query["status"], tc.wantStatus)
			}
		})
	}
}

// TestTaskListToolRefusesAnUnknownFilter: the SDK never validates arguments
// against the schema's enum, so the refusal has to come from the service.
func TestTaskListToolRefusesAnUnknownFilter(t *testing.T) {
	for _, args := range []string{`{"type":"BOGUS"}`, `{"status":["FAILED","nope"]}`} {
		query, resp := taskListToolCall(t, args)
		if resp.Success || resp.Error == nil || resp.Error.Code != service.CodeInvalidInput {
			t.Errorf("%s: response = %+v, want an %s error", args, resp, service.CodeInvalidInput)
		}
		if query != nil {
			t.Errorf("%s: a rejected filter still reached the API: %v", args, query)
		}
	}
}

func TestTaskListToolRejectsAMalformedFilter(t *testing.T) {
	query, resp := taskListToolCall(t, `{"type":5}`)
	if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" {
		t.Errorf("response = %+v, want INVALID_INPUT", resp)
	}
	if query != nil {
		t.Errorf("a malformed request still reached the API: %v", query)
	}
}
