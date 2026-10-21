package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpBackupFixture = `{
  "type": "BACKUP", "organization": "acme", "total": 2,
  "tasks": [
    {"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01", "status": "PENDING", "expires_at": "2026-10-20T18:21:07Z"},
    {"task": "hK3mZp0a", "device_uuid": "11111111-2222-3333-4444-555555555555", "device_name": "fw-branch-03", "status": "PENDING", "expires_at": "2026-10-20T18:21:08Z"}
  ],
  "skipped": [
    {"device_uuid": "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"},
    {"device_uuid": "99999999-8888-7777-6666-555555555555", "device_name": "fw-branch-04", "code": "SOME_FUTURE_CODE", "reason": "A reason this build has never heard of"}
  ]
}`

// backupToolCall is one ndcli.run.backup call against a fake control plane.
type backupToolCall struct {
	resp    ToolResponse
	isError bool

	mu                 sync.Mutex
	requests           int
	method, path, body string
}

// callRunBackup sends arguments through the registered handler, which does the
// auth check and the argument parsing, to a control plane answering status and
// body.
func callRunBackup(t *testing.T, status int, body, arguments string) *backupToolCall {
	t.Helper()
	call := &backupToolCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call.mu.Lock()
		call.requests++
		call.method, call.path, call.body = r.Method, r.URL.EscapedPath(), strings.TrimSpace(string(raw))
		call.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	s := newStaticAuthTestServerWithAPI(t, srv, "acme")
	result, err := s.handleRunBackup(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(arguments)},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	decodeToolResult(t, result, &call.resp)
	call.isError = result.IsError
	return call
}

// TestRunBackup_PreviewSendsNothing: without confirm=true the control plane is
// not asked for anything, not even a read, and the preview says what a
// confirmed call would do.
func TestRunBackup_PreviewSendsNothing(t *testing.T) {
	for _, tc := range []struct {
		args   string
		target string
	}{
		{`{"devices":["fw-branch-01"]}`, "fw-branch-01"},
		{`{"devices":["fw-a","fw-b"]}`, "2 devices"},
		{`{"ous":["edge"]}`, "OU edge"},
		{`{"ous":["edge","core"]}`, "2 OUs"},
		{`{"devices":["fw-a"],"ous":["edge"]}`, "1 device + 1 OU"},
		{`{"devices":["fw-a","fw-b"],"ous":["edge"]}`, "2 devices + 1 OU"},
		{`{"devices":["fw-a"],"ous":["edge","core","lab"]}`, "1 device + 3 OUs"},
		{`{"org":true}`, "every device that has backup enabled"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, tc.args)

			if call.requests != 0 {
				t.Fatalf("%d request(s) before confirm", call.requests)
			}
			if call.isError || !call.resp.Success {
				t.Fatalf("a preview is not an error: %+v", call.resp)
			}
			data, _ := call.resp.Data.(map[string]interface{})
			if data["preview"] != true || data["action"] != "back up" || data["target"] != tc.target {
				t.Errorf("preview data = %v", data)
			}
			note, _ := data["note"].(string)
			if !strings.Contains(note, "backup enabled") || !strings.Contains(note, "expires a minute") {
				t.Errorf("note = %q", note)
			}
			if !strings.Contains(call.resp.Message, "Set confirm=true to execute.") {
				t.Errorf("message = %q", call.resp.Message)
			}
		})
	}
}

// TestRunBackup_ConfirmedWire pins the route and the body, literally.
func TestRunBackup_ConfirmedWire(t *testing.T) {
	for _, tc := range []struct {
		args string
		body string
	}{
		{`{"devices":["fw-branch-01","fw-branch-02"],"confirm":true}`, `{"targets":{"devices":["fw-branch-01","fw-branch-02"]}}`},
		{`{"ous":["edge"],"confirm":true}`, `{"targets":{"ous":["edge"]}}`},
		{`{"devices":["fw-a"],"ous":["edge"],"confirm":true}`, `{"targets":{"devices":["fw-a"],"ous":["edge"]}}`},
		{`{"org":true,"confirm":true}`, `{"targets":{"all":true}}`},
		{`{"organization":"other-org","org":true,"confirm":true}`, `{"targets":{"all":true}}`},
	} {
		t.Run(tc.args, func(t *testing.T) {
			call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, tc.args)

			if call.isError || !call.resp.Success {
				t.Fatalf("failed: %+v", call.resp.Error)
			}
			wantPath := "/api/v1/organizations/acme/backups/run"
			if strings.Contains(tc.args, "other-org") {
				wantPath = "/api/v1/organizations/other-org/backups/run"
			}
			if call.requests != 1 || call.method != http.MethodPost || call.path != wantPath {
				t.Errorf("%d request(s), %s %s, want one POST %s", call.requests, call.method, call.path, wantPath)
			}
			if call.body != tc.body {
				t.Errorf("body = %s\nwant   %s", call.body, tc.body)
			}
		})
	}
}

// TestRunBackup_ResultIsNamedData: the tasks and the skipped devices are data
// fields, not prose, and an unknown skip code comes through with its reason.
func TestRunBackup_ResultIsNamedData(t *testing.T) {
	call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, `{"org":true,"confirm":true}`)

	data, _ := call.resp.Data.(map[string]interface{})
	if data["type"] != "BACKUP" || data["organization"] != "acme" || data["total"] != float64(2) {
		t.Fatalf("data = %v", data)
	}
	tasks, _ := data["tasks"].([]interface{})
	if len(tasks) != 2 {
		t.Fatalf("tasks = %v", data["tasks"])
	}
	first, _ := tasks[0].(map[string]interface{})
	if first["task"] != "tQSd9RCV" || first["device"] != "fw-branch-01" || first["status"] != "PENDING" ||
		first["expires_at"] != "2026-10-20T18:21:07Z" || first["device_uuid"] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("first task = %v", first)
	}
	skipped, _ := data["skipped"].([]interface{})
	if len(skipped) != 2 {
		t.Fatalf("skipped = %v", data["skipped"])
	}
	one, _ := skipped[0].(map[string]interface{})
	if one["device"] != "fw-branch-02" || one["code"] != "BACKUP_NOT_ENABLED" || one["reason"] != "Backup is not enabled for this device" {
		t.Errorf("first skipped = %v", one)
	}
	two, _ := skipped[1].(map[string]interface{})
	if two["code"] != "SOME_FUTURE_CODE" || two["reason"] != "A reason this build has never heard of" {
		t.Errorf("unknown code was not kept: %v", two)
	}
	if want := "2 backup task(s) started; 2 device(s) not backed up, the reason for each is in skipped"; call.resp.Message != want {
		t.Errorf("message = %q, want %q", call.resp.Message, want)
	}
}

func TestRunBackup_NothingSkippedKeepsTheEmptyList(t *testing.T) {
	call := callRunBackup(t, http.StatusCreated,
		`{"type":"BACKUP","organization":"acme","total":1,"tasks":[{"task":"T1","device_uuid":"u","device_name":"fw-a","status":"PENDING","expires_at":"2026-10-20T18:21:07Z"}],"skipped":[]}`,
		`{"devices":["fw-a"],"confirm":true}`)

	data, _ := call.resp.Data.(map[string]interface{})
	if skipped, ok := data["skipped"].([]interface{}); !ok || len(skipped) != 0 {
		t.Errorf("skipped = %#v, want an empty list", data["skipped"])
	}
	if call.resp.Message != "1 backup task(s) started" {
		t.Errorf("message = %q", call.resp.Message)
	}
}

// TestRunBackup_RefusesDeferral: every sibling ndcli.run.* tool takes `at`, so
// a caller may send one here. Ignoring it would start the backup now when a
// deferred one was asked for, so it is refused, before anything is sent, with
// or without confirm.
func TestRunBackup_RefusesDeferral(t *testing.T) {
	for _, tc := range []struct {
		args string
		name string
	}{
		{`{"devices":["fw-a"],"at":"2h","confirm":true}`, "at"},
		{`{"devices":["fw-a"],"at":"2h"}`, "at"},
		{`{"devices":["fw-a"],"at":"2026-12-01 03:00","timezone":"America/Sao_Paulo","confirm":true}`, "at"},
		{`{"devices":["fw-a"],"schedule":"nightly","confirm":true}`, "schedule"},
		{`{"devices":["fw-a"],"scheduled_at":"2026-12-01T03:00:00Z","confirm":true}`, "scheduled_at"},
		{`{"devices":["fw-a"],"payload":{"encryption_key":"hunter2"},"confirm":true}`, "payload"},
		{`{"devices":["fw-a"],"at":"2h","schedule":"nightly","confirm":true}`, "at, schedule"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, tc.args)

			if !call.isError || call.resp.Success || call.resp.Error == nil {
				t.Fatalf("expected a refusal, got %+v", call.resp)
			}
			if call.resp.Error.Code != "INVALID_INPUT" {
				t.Errorf("code = %q", call.resp.Error.Code)
			}
			if !strings.Contains(call.resp.Error.Message, "does not take "+tc.name+":") ||
				!strings.Contains(call.resp.Error.Message, "ndcli.backup.config_set_schedule") {
				t.Errorf("message = %q", call.resp.Error.Message)
			}
			if call.requests != 0 {
				t.Errorf("%d request(s) were sent", call.requests)
			}
		})
	}
}

// TestRunBackup_TimezoneAloneIsIgnored: with no `at` there is nothing for a
// timezone to interpret, so a stale one is not an error.
func TestRunBackup_TimezoneAloneIsIgnored(t *testing.T) {
	call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, `{"devices":["fw-a"],"timezone":"UTC","at":"","payload":null,"confirm":true}`)
	if !call.resp.Success || call.requests != 1 {
		t.Errorf("success=%v, %d request(s): %+v", call.resp.Success, call.requests, call.resp.Error)
	}
}

func TestRunBackup_NeedsATarget(t *testing.T) {
	for _, args := range []string{
		`{}`,
		`{"confirm":true}`,
		`{"org":true,"devices":["fw-a"],"confirm":true}`,
		`{"org":true,"ous":["edge"],"confirm":true}`,
		`{"devices":[" "],"confirm":true}`,
	} {
		t.Run(args, func(t *testing.T) {
			call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, args)

			if !call.isError || call.resp.Error == nil || call.resp.Error.Code != "INVALID_INPUT" {
				t.Fatalf("expected INVALID_INPUT, got %+v", call.resp)
			}
			if call.requests != 0 {
				t.Errorf("%d request(s) were sent", call.requests)
			}
			if strings.Contains(call.resp.Error.Message, "--") {
				t.Errorf("message names a CLI flag: %q", call.resp.Error.Message)
			}
		})
	}
}

// TestRunBackup_NoEligibleDevices: a request that starts nothing is an error
// that carries the per-device reasons as data, in the message, and the way
// forward in this surface's own words.
func TestRunBackup_NoEligibleDevices(t *testing.T) {
	call := callRunBackup(t, http.StatusConflict, `{
	  "error": "No backup was started: none of the targeted devices can be backed up right now",
	  "code": "BACKUP_NO_ELIGIBLE_DEVICES",
	  "skipped": [
	    {"device_uuid": "u2", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"}
	  ]}`, `{"devices":["fw-branch-02"],"confirm":true}`)

	if !call.isError || call.resp.Success || call.resp.Error == nil {
		t.Fatalf("expected an error, got %+v", call.resp)
	}
	if call.resp.Error.Code != "BACKUP_NO_ELIGIBLE_DEVICES" {
		t.Errorf("code = %q", call.resp.Error.Code)
	}
	for _, want := range []string{"No backup was started", "fw-branch-02: Backup is not enabled for this device", "ndcli.backup.enable"} {
		if !strings.Contains(call.resp.Error.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, call.resp.Error.Message)
		}
	}
	if strings.Contains(call.resp.Error.Message, "ndcli backup enable") {
		t.Errorf("message names the CLI command:\n%s", call.resp.Error.Message)
	}
	data, _ := call.resp.Data.(map[string]interface{})
	skipped, _ := data["skipped"].([]interface{})
	if len(skipped) != 1 {
		t.Fatalf("data = %v, want the skipped list", call.resp.Data)
	}
	one, _ := skipped[0].(map[string]interface{})
	if one["device"] != "fw-branch-02" || one["device_uuid"] != "u2" || one["code"] != "BACKUP_NOT_ENABLED" {
		t.Errorf("skipped[0] = %v", one)
	}
}

func TestRunBackup_NoDeviceHasBackup(t *testing.T) {
	call := callRunBackup(t, http.StatusConflict,
		`{"error":"No device in this organization has backup enabled","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[]}`,
		`{"org":true,"confirm":true}`)

	if call.resp.Error == nil || call.resp.Error.Code != "BACKUP_NO_ELIGIBLE_DEVICES" {
		t.Fatalf("got %+v", call.resp)
	}
	if !strings.Contains(call.resp.Error.Message, "Hint: Backup is enabled per device: ndcli.backup.enable.") {
		t.Errorf("message = %q", call.resp.Error.Message)
	}
	data, _ := call.resp.Data.(map[string]interface{})
	if skipped, ok := data["skipped"].([]interface{}); !ok || len(skipped) != 0 {
		t.Errorf("data = %#v, want an empty skipped list", call.resp.Data)
	}
}

// TestRunBackup_TargetsResolveToNothing: an OU with no members gives the same
// refusal with an empty list as an organization where no device has backup.
// The empty list stays in data, but only a request for every device with backup
// is told to enable it.
func TestRunBackup_TargetsResolveToNothing(t *testing.T) {
	body := `{"error":"The targets include no enabled device","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[]}`

	for _, tc := range []struct {
		name string
		args string
		hint bool
	}{
		{"an OU with no members", `{"ous":["empty-ou"],"confirm":true}`, false},
		{"a named device", `{"devices":["fw-a"],"confirm":true}`, false},
		{"devices and OUs", `{"devices":["fw-a"],"ous":["empty-ou"],"confirm":true}`, false},
		{"every device that has backup", `{"org":true,"confirm":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := callRunBackup(t, http.StatusConflict, body, tc.args)

			if call.resp.Error == nil || call.resp.Error.Code != "BACKUP_NO_ELIGIBLE_DEVICES" {
				t.Fatalf("got %+v", call.resp)
			}
			msg := call.resp.Error.Message
			if got := strings.Contains(msg, "ndcli.backup.enable"); got != tc.hint {
				t.Errorf("hint given = %v, want %v: %q", got, tc.hint, msg)
			}
			if !strings.HasPrefix(msg, "The targets include no enabled device") {
				t.Errorf("the server's sentence is not first: %q", msg)
			}
			data, _ := call.resp.Data.(map[string]interface{})
			if skipped, ok := data["skipped"].([]interface{}); !ok || len(skipped) != 0 {
				t.Errorf("data = %#v, want an empty skipped list", call.resp.Data)
			}
		})
	}
}

// TestRunBackup_ConfigDisabledWithAnEmptyList: NDManager sends skipped: [] with
// BACKUP_CONFIG_DISABLED as well. That is the configuration being off, not "no
// device has backup": the hint is the configuration's and the result carries no
// device list.
func TestRunBackup_ConfigDisabledWithAnEmptyList(t *testing.T) {
	call := callRunBackup(t, http.StatusConflict,
		`{"error":"The backup configuration is disabled","code":"BACKUP_CONFIG_DISABLED","skipped":[]}`, `{"org":true,"confirm":true}`)

	if call.resp.Error == nil || call.resp.Error.Code != "BACKUP_CONFIG_DISABLED" {
		t.Fatalf("got %+v", call.resp)
	}
	if !strings.Contains(call.resp.Error.Message, "Hint: Enable it first: ndcli.backup.config_enable.") ||
		strings.Contains(call.resp.Error.Message, "ndcli.backup.enable") {
		t.Errorf("message = %q", call.resp.Error.Message)
	}
	if call.resp.Data != nil {
		t.Errorf("data = %v; only the no-eligible-devices refusal carries a list", call.resp.Data)
	}
}

func TestRunBackup_ConfigurationRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
		hint   string
	}{
		{"not configured", http.StatusNotFound, `{"error":"This organization has no backup configuration","code":"BACKUP_NOT_CONFIGURED"}`,
			"BACKUP_NOT_CONFIGURED", "Hint: Create the backup configuration first: `ndcli backup config set` on the command line"},
		{"disabled", http.StatusConflict, `{"error":"The backup configuration is disabled","code":"BACKUP_CONFIG_DISABLED"}`,
			"BACKUP_CONFIG_DISABLED", "Hint: Enable it first: ndcli.backup.config_enable."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := callRunBackup(t, tc.status, tc.body, `{"org":true,"confirm":true}`)

			if call.resp.Error == nil || call.resp.Error.Code != tc.code {
				t.Fatalf("got %+v", call.resp)
			}
			if !strings.Contains(call.resp.Error.Message, tc.hint) {
				t.Errorf("message = %q, want it to contain %q", call.resp.Error.Message, tc.hint)
			}
			if call.resp.Data != nil {
				t.Errorf("data = %v; only the no-eligible-devices refusal carries a list", call.resp.Data)
			}
		})
	}
}

func TestRunBackup_AgainstAnOlderControlPlane(t *testing.T) {
	call := callRunBackup(t, http.StatusNotFound, `{"detail":"Not Found"}`, `{"org":true,"confirm":true}`)

	if call.resp.Error == nil || call.resp.Error.Code != "SERVER_TOO_OLD" {
		t.Fatalf("got %+v", call.resp)
	}
}

// TestRunBackup_Schema: the tool takes targets and confirm and nothing that
// defers, schedules or loads a backup.
func TestRunBackup_Schema(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.run.backup")
	props, _ := schema["properties"].(map[string]any)

	for _, want := range []string{"organization", "devices", "ous", "org", "confirm"} {
		if _, ok := props[want]; !ok {
			t.Errorf("schema lacks %q", want)
		}
	}
	for _, banned := range []string{"at", "timezone", "schedule", "scheduled_at", "payload", "encryption_key", "key"} {
		if _, ok := props[banned]; ok {
			t.Errorf("schema exposes %q", banned)
		}
	}
	if len(props) != 5 {
		t.Errorf("schema has %d properties, want 5", len(props))
	}
	if required, _ := schema["required"].([]any); len(required) != 0 {
		t.Errorf("required = %v", required)
	}
}

// TestRunBackup_Description: the description is what a model reads before it
// calls, so it must carry the rules a wrong call would break.
func TestRunBackup_Description(t *testing.T) {
	cs := connectTestClient(t)
	var description string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "ndcli.run.backup" {
			description = tool.Description
		}
	}
	if description == "" {
		t.Fatal("ndcli.run.backup is not registered")
	}
	for _, want := range []string{
		"Only devices that have backup enabled are backed up",
		"skipped",
		"BACKUP_NOT_ENABLED", "DEVICE_NOT_ENABLED", "NO_ENCRYPTION_KEY", "BACKUP_IN_FLIGHT", "TASK_CREATE_FAILED",
		"BACKUP_NO_ELIGIBLE_DEVICES",
		"cannot be deferred or scheduled",
		"ndcli.backup.config_set_schedule",
		"expires a minute",
		"Requires confirm=true.",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
