package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

const pingRunResponse = `{"type":"PING","organization":"acme","total":1,"tasks":[{"task":"T1","device_name":"fw1","status":"PENDING"}]}`

// pingTool runs ndcli.run.ping's handler logic on the raw JSON arguments a
// client would send, against a fake control plane, and returns the tool
// response and the payload of every task-creating POST.
func pingTool(t *testing.T, args string) (ToolResponse, []map[string]interface{}) {
	t.Helper()
	var payloads []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Payload map[string]interface{} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body %s: %v", raw, err)
		}
		payloads = append(payloads, body.Payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, pingRunResponse)
	}))
	defer srv.Close()

	input, err := parseInput[runInput]([]byte(args))
	if err != nil {
		t.Fatalf("parse %s: %v", args, err)
	}
	s := newTestServer(t, srv, "acme")
	result, err := s.runCommand(context.Background(), "ping", models.TaskTypePing, pingPayload, input)
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	return resp, payloads
}

// A count outside 1..100 is refused before the preview and before anything is
// sent, 0 included: an explicit 0 used to read as "absent" and ping 4 times.
func TestRunPingRefusesACountOutOfRange(t *testing.T) {
	for _, count := range []string{"0", "-1", "101", "1000"} {
		for _, confirm := range []bool{false, true} {
			args := fmt.Sprintf(`{"devices":["fw1"],"host":"1.1.1.1","count":%s,"confirm":%t}`, count, confirm)
			t.Run(args, func(t *testing.T) {
				resp, payloads := pingTool(t, args)

				if resp.Success || resp.Error == nil {
					t.Fatalf("count %s accepted: %+v", count, resp)
				}
				if resp.Error.Code != "INVALID_INPUT" || resp.Error.Message != "count must be between 1 and 100" {
					t.Errorf("error = %+v, want INVALID_INPUT \"count must be between 1 and 100\"", resp.Error)
				}
				if len(payloads) != 0 {
					t.Errorf("%d task(s) created, want none", len(payloads))
				}
			})
		}
	}
}

// A count in range is sent as given; absent, or the agent's default of 4, it
// is left out of the payload.
func TestRunPingSendsACountInRange(t *testing.T) {
	for _, tc := range []struct {
		count string
		want  map[string]interface{}
	}{
		{"", map[string]interface{}{"target": "1.1.1.1"}},
		{`,"count":4`, map[string]interface{}{"target": "1.1.1.1"}},
		{`,"count":1`, map[string]interface{}{"target": "1.1.1.1", "count": float64(1)}},
		{`,"count":100`, map[string]interface{}{"target": "1.1.1.1", "count": float64(100)}},
	} {
		args := `{"devices":["fw1"],"host":"1.1.1.1"` + tc.count + `,"confirm":true}`
		t.Run(args, func(t *testing.T) {
			resp, payloads := pingTool(t, args)

			if !resp.Success {
				t.Fatalf("refused: %+v", resp.Error)
			}
			if len(payloads) != 1 || !reflect.DeepEqual(payloads[0], tc.want) {
				t.Errorf("payloads = %v, want [%v]", payloads, tc.want)
			}
		})
	}
}

// The schema and the description both state the range, so a model reads it
// before it calls.
func TestRunPingAdvertisesTheCountRange(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.run.ping")
	count, ok := schema["properties"].(map[string]any)["count"].(map[string]any)
	if !ok {
		t.Fatalf("no count property: %#v", schema["properties"])
	}
	if count["minimum"] != float64(1) || count["maximum"] != float64(100) {
		t.Errorf("count minimum/maximum = %v/%v, want 1/100", count["minimum"], count["maximum"])
	}
	if desc, _ := count["description"].(string); !strings.Contains(desc, "1 to 100") {
		t.Errorf("count description %q does not state 1 to 100", desc)
	}

	cs := connectTestClient(t)
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if tool.Name == "ndcli.run.ping" {
			if !strings.Contains(tool.Description, "`count` is 1 to 100") {
				t.Errorf("tool description %q does not state the count range", tool.Description)
			}
			return
		}
	}
	t.Fatal("ndcli.run.ping not registered")
}
