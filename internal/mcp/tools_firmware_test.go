package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// firmwareTools is every ndcli.firmware.* tool. The CLI package pins the same
// names from its side (cli/firmware_test.go, firmwareCommandTools): every
// `ndcli firmware` command has a tool here and every tool a command, since
// none of the four MCP-parity exemptions applies to firmware tracks.
var firmwareTools = []string{
	"ndcli.firmware.config_show",
	"ndcli.firmware.config_create",
	"ndcli.firmware.config_update",
	"ndcli.firmware.config_enable",
	"ndcli.firmware.config_disable",
	"ndcli.firmware.config_delete",
	"ndcli.firmware.track_show",
	"ndcli.firmware.track_set",
	"ndcli.firmware.track_schedule",
	"ndcli.firmware.device_set",
	"ndcli.firmware.status",
	"ndcli.firmware.history",
	"ndcli.firmware.rollout",
	"ndcli.firmware.version_halt",
	"ndcli.firmware.version_unhalt",
	"ndcli.firmware.version_promote",
	"ndcli.firmware.version_halts",
	"ndcli.firmware.failure_clear",
}

func TestFirmwareToolsAreRegistered(t *testing.T) {
	var got []string
	for _, name := range registeredToolNames(t) {
		if strings.HasPrefix(name, "ndcli.firmware.") {
			got = append(got, name)
		}
	}
	want := append([]string(nil), firmwareTools...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("firmware tools =\n%v\nwant\n%v", got, want)
	}
}

// toolDefinitions lists the registered tools by name, as a client sees them.
func toolDefinitions(t *testing.T) map[string]*mcp.Tool {
	t.Helper()
	cs := connectTestClient(t)
	out := map[string]*mcp.Tool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		out[tool.Name] = tool
	}
	return out
}

func schemaProps(t *testing.T, tool *mcp.Tool) (map[string]interface{}, []string) {
	t.Helper()
	schema, ok := tool.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("%s: InputSchema is %T", tool.Name, tool.InputSchema)
	}
	props, _ := schema["properties"].(map[string]interface{})
	var required []string
	if raw, ok := schema["required"].([]interface{}); ok {
		for _, r := range raw {
			required = append(required, r.(string))
		}
	}
	return props, required
}

func enumOf(t *testing.T, props map[string]interface{}, name string) []string {
	t.Helper()
	prop, ok := props[name].(map[string]interface{})
	if !ok {
		t.Fatalf("no %q property", name)
	}
	raw, ok := prop["enum"].([]interface{})
	if !ok {
		t.Fatalf("%q has no enum: %v", name, prop)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i] = v.(string)
	}
	return out
}

func TestFirmwareToolSchemas(t *testing.T) {
	tools := toolDefinitions(t)

	// Tools that change what a device will do preview unless confirm=true.
	gated := map[string]bool{
		"ndcli.firmware.config_update": true, "ndcli.firmware.config_disable": true, "ndcli.firmware.config_delete": true,
		"ndcli.firmware.track_set": true, "ndcli.firmware.track_schedule": true, "ndcli.firmware.device_set": true,
		"ndcli.firmware.version_halt": true, "ndcli.firmware.version_unhalt": true, "ndcli.firmware.version_promote": true,
		"ndcli.firmware.failure_clear": true,
	}
	required := map[string][]string{
		"ndcli.firmware.config_update":   {"reboot"},
		"ndcli.firmware.track_set":       {"track"},
		"ndcli.firmware.track_schedule":  {"track"},
		"ndcli.firmware.device_set":      {"devices"},
		"ndcli.firmware.history":         {"device"},
		"ndcli.firmware.version_halt":    {"version", "reason"},
		"ndcli.firmware.version_unhalt":  {"version"},
		"ndcli.firmware.version_promote": {"version", "track"},
		"ndcli.firmware.failure_clear":   {"version"},
	}
	for _, name := range firmwareTools {
		tool := tools[name]
		props, req := schemaProps(t, tool)
		if _, ok := props["organization"]; !ok {
			t.Errorf("%s has no organization parameter", name)
		}
		_, hasConfirm := props["confirm"]
		if hasConfirm != gated[name] {
			t.Errorf("%s: confirm parameter = %v, want %v", name, hasConfirm, gated[name])
		}
		if gated[name] && !strings.Contains(tool.Description, "confirm=true") {
			t.Errorf("%s: the description should say it requires confirm=true", name)
		}
		sort.Strings(req)
		want := append([]string(nil), required[name]...)
		sort.Strings(want)
		if len(req) != len(want) || (len(want) > 0 && !reflect.DeepEqual(req, want)) {
			t.Errorf("%s: required = %v, want %v", name, req, want)
		}
		// No OU, no pattern: a track is chosen device by device.
		for _, banned := range []string{"ou", "ous", "regex", "pattern"} {
			if _, ok := props[banned]; ok {
				t.Errorf("%s has a %q parameter", name, banned)
			}
		}
	}

	// The enums are the CLI's lists.
	for tool, tc := range map[string]struct {
		param string
		want  []string
	}{
		"ndcli.firmware.track_show":      {"track", models.FirmwareTracks},
		"ndcli.firmware.track_set":       {"track", models.FirmwareTracks},
		"ndcli.firmware.track_schedule":  {"track", models.FirmwareTracks},
		"ndcli.firmware.device_set":      {"track", models.FirmwareAssignTracks},
		"ndcli.firmware.status":          {"track", models.FirmwareAssignTracks},
		"ndcli.firmware.version_promote": {"track", models.FirmwarePromoteTracks},
		"ndcli.firmware.failure_clear":   {"track", models.FirmwareTracks},
	} {
		props, _ := schemaProps(t, tools[tool])
		if got := enumOf(t, props, tc.param); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s enum = %v, want %v", tool, tc.param, got, tc.want)
		}
	}
	props, _ := schemaProps(t, tools["ndcli.firmware.status"])
	if got := enumOf(t, props, "state"); !reflect.DeepEqual(got, models.FirmwareStates) {
		t.Errorf("status state enum = %v", got)
	}

	// A major upgrade is the same tools with a kind, not more tools.
	for _, name := range []string{"version_halt", "version_unhalt", "version_promote", "failure_clear"} {
		props, _ := schemaProps(t, tools["ndcli.firmware."+name])
		if got := enumOf(t, props, "kind"); !reflect.DeepEqual(got, models.FirmwareKinds) {
			t.Errorf("%s kind enum = %v, want %v", name, got, models.FirmwareKinds)
		}
	}
	haltsProps, _ := schemaProps(t, tools["ndcli.firmware.version_halts"])
	if got := enumOf(t, haltsProps, "kind"); !reflect.DeepEqual(got, models.FirmwareHaltKinds) {
		t.Errorf("version_halts kind enum = %v, want %v", got, models.FirmwareHaltKinds)
	}
	for tool, param := range map[string]string{"ndcli.firmware.track_set": "auto_apply_major", "ndcli.firmware.device_set": "boot_environment"} {
		props, _ := schemaProps(t, tools[tool])
		prop, ok := props[param].(map[string]interface{})
		if !ok || prop["type"] != "boolean" {
			t.Errorf("%s: %s = %v, want a boolean", tool, param, props[param])
		}
	}

	// A caller reads held_reason and reason without guessing: every code is
	// explained once, in status, and the other tools that return them say where.
	for _, code := range models.FirmwareReasons {
		if !strings.Contains(tools["ndcli.firmware.status"].Description, code+" (") {
			t.Errorf("ndcli.firmware.status does not explain the reason %s", code)
		}
	}
	for _, name := range []string{"ndcli.firmware.history", "ndcli.task.describe"} {
		if !strings.Contains(tools[name].Description, "ndcli.firmware.status") {
			t.Errorf("%s should send the caller to ndcli.firmware.status for the reason codes", name)
		}
	}
	if d := tools["ndcli.task.describe"].Description; !strings.Contains(d, "firmware_track_result") || !strings.Contains(d, "FIRMWARE_TRACK") {
		t.Errorf("ndcli.task.describe should explain firmware_track_result: %s", d)
	}
	if d := tools["ndcli.run.firmware_upgrade"].Description; !strings.Contains(d, "FIRMWARE_DEVICE_MANAGED") || !strings.Contains(d, "ndcli.firmware.device_set") {
		t.Errorf("ndcli.run.firmware_upgrade should explain the managed-device refusal: %s", d)
	}
	for _, name := range []string{"ndcli.schedule.tasks.enable", "ndcli.schedule.tasks.disable", "ndcli.schedule.tasks.remove", "ndcli.schedule.delete"} {
		if d := tools[name].Description; !strings.Contains(d, "FIRMWARE_SPEC_MANAGED") || !strings.Contains(d, "ndcli.firmware.track_schedule") {
			t.Errorf("%s should explain the firmware spec refusal: %s", name, d)
		}
	}
}

// ── calling the tools ────────────────────────────────────────────────────────

type mcpSeen struct {
	method string
	path   string
	query  url.Values
	body   map[string]interface{}
	hits   int
}

// firmwareClient connects an MCP client to a server whose API is a fake that
// answers status and body to everything, so a tool is exercised through the
// protocol: schema, argument decoding and handler.
func firmwareClient(t *testing.T, status int, body string) (*mcp.ClientSession, *mcpSeen) {
	t.Helper()
	seen := &mcpSeen{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.hits++
		seen.method, seen.path, seen.query, seen.body = r.Method, r.URL.EscapedPath(), r.URL.Query(), nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &seen.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(api.Close)

	s := newStaticAuthTestServerWithAPI(t, api, "acme")
	s.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "ndcli", Version: config.Version}, nil)
	s.registerAll()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, seen
}

func callTool(t *testing.T, cs *mcp.ClientSession, name, args string) ToolResponse {
	t.Helper()
	var arguments interface{}
	if err := json.Unmarshal([]byte(args), &arguments); err != nil {
		t.Fatalf("bad test arguments: %v", err)
	}
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	return resp
}

const (
	mcpConfig  = `{"organization_name":"acme","status":"ENABLED","reboot":true,"plan_enabled":true,"tracks":[{"track":"fast","soak_days":1,"min_success":0,"scheduled":false,"devices":{"total":0}}],"created_at":"2026-10-08T12:00:00Z","message":"Firmware configuration created"}`
	mcpTrack   = `{"track":"regular","soak_days":7,"min_success":2,"schedule":"night","scheduled":true,"devices":{"total":1,"managed":1}}`
	mcpAssign  = `{"organization_name":"acme","track":"fast","results":[{"device_name":"fw-a","result":"added","track":"fast","state":"enrolling","notes":[]}],"cancelled_tasks":0}`
	mcpDevices = `{"items":[{"device_name":"fw-a","device_uuid":"u1","device_status":"ENABLED","track":"fast","state":"managed","pinned_version":"26.7.3"}],"total":1,"page":2,"per_page":20,"pages":1}`
	mcpHistory = `{"device_name":"fw-a","items":[{"completed_at":"2026-10-08T12:00:00Z","track":"fast","action":"advance","outcome":"success","applied":true}],"total":1,"page":1,"per_page":50,"pages":1}`
	mcpRollout = `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[],"failures":[],"halts":[]}`
	mcpHalt    = `{"halt":{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"},"cancelled_tasks":2}`
	mcpHalts   = `{"items":[{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"}],"total":1,"page":1,"per_page":50,"pages":1}`
	mcpPromote = `{"version":"26.7.5","results":[{"track":"fast","result":"approved","target":"26.7.5"}]}`
	mcpCleared = `{"version":"26.7.5","cleared":2}`
	mcpDeleted = `{"message":"Firmware configuration deleted"}`
)

// The tools that do not change anything run on the first call.
func TestFirmwareReadTools(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		args     string
		response string
		method   string
		path     string
		query    map[string]string
		dataKey  string
	}{
		{"ndcli.firmware.config_show", `{}`, mcpConfig, "GET", "/api/v1/organizations/acme/firmware/config", nil, "config"},
		{"ndcli.firmware.track_show", `{}`, mcpConfig, "GET", "/api/v1/organizations/acme/firmware/config", nil, "tracks"},
		{"ndcli.firmware.track_show", `{"track":"Regular"}`, mcpTrack, "GET", "/api/v1/organizations/acme/firmware/tracks/regular", nil, "track"},
		{"ndcli.firmware.status", `{"track":"fast","state":"managed","name":"fw","page":2,"per_page":20}`, mcpDevices, "GET",
			"/api/v1/organizations/acme/firmware/devices", map[string]string{"track": "fast", "state": "managed", "name": "fw", "page": "2", "per_page": "20"}, "devices"},
		{"ndcli.firmware.history", `{"device":"fw-a"}`, mcpHistory, "GET", "/api/v1/organizations/acme/firmware/devices/fw-a/history",
			map[string]string{"page": "1", "per_page": "50"}, "history"},
		{"ndcli.firmware.rollout", `{}`, mcpRollout, "GET", "/api/v1/organizations/acme/firmware/status", nil, "tracks"},
		{"ndcli.firmware.version_halts", `{"all":true}`, mcpHalts, "GET", "/api/v1/organizations/acme/firmware/halts", map[string]string{"active": "false"}, "halts"},
		// Not previewed: creating and enabling change no device.
		{"ndcli.firmware.config_create", `{"reboot":false}`, mcpConfig, "POST", "/api/v1/organizations/acme/firmware/config", nil, "config"},
		{"ndcli.firmware.config_enable", `{}`, mcpConfig, "PUT", "/api/v1/organizations/acme/firmware/config/status", nil, "config"},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.response)
			resp := callTool(t, cs, tc.tool, tc.args)
			if !resp.Success {
				t.Fatalf("failed: %+v", resp.Error)
			}
			if seen.method != tc.method || seen.path != tc.path {
				t.Errorf("sent %s %s, want %s %s", seen.method, seen.path, tc.method, tc.path)
			}
			for k, want := range tc.query {
				if got := seen.query.Get(k); got != want {
					t.Errorf("query %s = %q, want %q", k, got, want)
				}
			}
			data, _ := resp.Data.(map[string]interface{})
			if _, ok := data[tc.dataKey]; !ok {
				t.Errorf("data has no %q: %v", tc.dataKey, resp.Data)
			}
			if tc.tool == "ndcli.firmware.config_create" && seen.body["reboot"] != false {
				t.Errorf("config_create body = %v, want reboot false", seen.body)
			}
		})
	}

	t.Run("lists carry pagination", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpDevices)
		resp := callTool(t, cs, "ndcli.firmware.status", `{}`)
		if resp.Pagination == nil || resp.Pagination.Page != 2 || resp.Pagination.PerPage != 20 || resp.Pagination.Total != 1 {
			t.Errorf("pagination = %+v", resp.Pagination)
		}
	})
}

// Every tool that changes what a device will do shows what it would do first,
// sends nothing, and runs only when confirm=true.
func TestFirmwareWriteToolsPreviewThenRun(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		args     string // without confirm
		response string
		method   string
		path     string
		body     map[string]interface{}
		preview  string // a fragment of the preview message
	}{
		{"ndcli.firmware.config_update", `{"reboot":false}`, mcpConfig, "PUT", "/api/v1/organizations/acme/firmware/config",
			map[string]interface{}{"reboot": false}, "default reboot flag"},
		{"ndcli.firmware.config_disable", `{}`, mcpConfig, "PUT", "/api/v1/organizations/acme/firmware/config/status",
			map[string]interface{}{"status": "DISABLED"}, "disable firmware tracks"},
		{"ndcli.firmware.config_delete", `{}`, mcpDeleted, "DELETE", "/api/v1/organizations/acme/firmware/config", nil,
			"delete the firmware configuration"},
		{"ndcli.firmware.track_set", `{"track":"regular","soak_days":7,"min_success":2}`, mcpTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/regular",
			map[string]interface{}{"soak_days": float64(7), "min_success": float64(2)}, "regular (soak_days=7, min_success=2)"},
		{"ndcli.firmware.track_schedule", `{"track":"fast","schedule":"night"}`, mcpTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/fast/schedule", map[string]interface{}{"schedule": "night"}, "fast → night"},
		{"ndcli.firmware.track_schedule", `{"track":"fast","no_schedule":true}`, mcpTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/fast/schedule", map[string]interface{}{"schedule": nil}, "fast → (detached)"},
		{"ndcli.firmware.device_set", `{"devices":["fw-a","fw-b"],"track":"fast"}`, mcpAssign, "PUT",
			"/api/v1/organizations/acme/firmware/devices", map[string]interface{}{"devices": []interface{}{"fw-a", "fw-b"}, "track": "fast"},
			"fw-a, fw-b → fast"},
		{"ndcli.firmware.version_halt", `{"version":"26.7.5","reason":"regression"}`, mcpHalt, "POST",
			"/api/v1/organizations/acme/firmware/halts", map[string]interface{}{"version": "26.7.5", "reason": "regression"}, "halt firmware release"},
		{"ndcli.firmware.version_unhalt", `{"version":"26.7.5"}`, mcpHalt, "DELETE",
			"/api/v1/organizations/acme/firmware/halts/26.7.5", nil, "lift the halt on firmware release"},
		{"ndcli.firmware.version_promote", `{"version":"26.7.5","track":"all"}`, mcpPromote, "POST",
			"/api/v1/organizations/acme/firmware/promote", map[string]interface{}{"version": "26.7.5", "track": "all"}, "26.7.5 → all three tracks"},
		{"ndcli.firmware.failure_clear", `{"version":"26.7.5","track":"stable"}`, mcpCleared, "POST",
			"/api/v1/organizations/acme/firmware/failures/clear", map[string]interface{}{"version": "26.7.5", "track": "stable"},
			"26.7.5 (stable track)"},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.response)

			preview := callTool(t, cs, tc.tool, tc.args)
			data, _ := preview.Data.(map[string]interface{})
			if !preview.Success || data["preview"] != true {
				t.Fatalf("without confirm the tool must preview: %+v", preview)
			}
			if !strings.Contains(preview.Message, tc.preview) || !strings.Contains(preview.Message, "confirm=true") {
				t.Errorf("preview message = %q, want it to say %q and ask for confirm=true", preview.Message, tc.preview)
			}
			if data["organization"] != "acme" {
				t.Errorf("preview data carries no organization: %v", data)
			}
			if seen.hits != 0 {
				t.Fatalf("a preview sent %d request(s)", seen.hits)
			}

			confirmed := strings.TrimSuffix(tc.args, "}") + `,"confirm":true}`
			if tc.args == `{}` {
				confirmed = `{"confirm":true}`
			}
			resp := callTool(t, cs, tc.tool, confirmed)
			if !resp.Success {
				t.Fatalf("confirmed call failed: %+v", resp.Error)
			}
			if d, _ := resp.Data.(map[string]interface{}); d["preview"] == true {
				t.Error("a confirmed call must not preview")
			}
			if seen.method != tc.method || seen.path != tc.path || !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("sent %s %s %v, want %s %s %v", seen.method, seen.path, seen.body, tc.method, tc.path, tc.body)
			}
		})
	}
}

func TestFirmwareToolsRefuseBadInputBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args string
		want string
	}{
		{"ndcli.firmware.config_update", `{"confirm":true}`, "reboot is required"},
		{"ndcli.firmware.track_show", `{"track":"slow"}`, `unknown track "slow"`},
		{"ndcli.firmware.track_set", `{"track":"fast","confirm":true}`, "provide soak_days and/or min_success"},
		{"ndcli.firmware.track_set", `{"track":"unmanaged","soak_days":1,"confirm":true}`, `unknown track "unmanaged"`},
		{"ndcli.firmware.track_schedule", `{"track":"fast","confirm":true}`, "give the schedule to attach"},
		{"ndcli.firmware.track_schedule", `{"track":"fast","schedule":"night","no_schedule":true,"confirm":true}`, "not both"},
		{"ndcli.firmware.device_set", `{"devices":[],"track":"fast","confirm":true}`, "at least one device name is required"},
		{"ndcli.firmware.device_set", `{"devices":["fw-a"],"track":"all","confirm":true}`, `unknown track "all"`},
		{"ndcli.firmware.status", `{"state":"asleep"}`, `unknown state "asleep"`},
		{"ndcli.firmware.history", `{"device":" "}`, "device name is required"},
		{"ndcli.firmware.version_halt", `{"version":"26.7.5","confirm":true}`, "reason is required"},
		{"ndcli.firmware.version_promote", `{"version":"26.7.5","track":"unmanaged","confirm":true}`, `unknown track "unmanaged"`},
		{"ndcli.firmware.failure_clear", `{"version":"26.7.5","track":"all","confirm":true}`, `unknown track "all"`},
		// A bad input is refused before the preview too: a preview of a call
		// that cannot run would mislead.
		{"ndcli.firmware.version_promote", `{"version":"26.7.5","track":"slow"}`, `unknown track "slow"`},
		{"ndcli.firmware.device_set", `{"devices":["fw-a"],"track":"slow"}`, `unknown track "slow"`},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, `{}`)
			resp := callTool(t, cs, tc.tool, tc.args)
			if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" || !strings.Contains(resp.Error.Message, tc.want) {
				t.Fatalf("response = %+v, want INVALID_INPUT saying %q", resp, tc.want)
			}
			if seen.hits != 0 {
				t.Errorf("%d request(s) sent for refused input", seen.hits)
			}
		})
	}
}

// The server's code reaches error.code, and the way forward is in tool words.
func TestFirmwareRefusalsAreAdvisedInToolWords(t *testing.T) {
	for _, tc := range []struct {
		code string
		tool string
		args string
		hint string
	}{
		{"FIRMWARE_NOT_CONFIGURED", "ndcli.firmware.rollout", `{}`, "ndcli.firmware.config_create"},
		{"FIRMWARE_DISABLED", "ndcli.firmware.version_promote", `{"version":"26.7.5","track":"all","confirm":true}`, "ndcli.firmware.config_enable"},
		{"FIRMWARE_DEVICE_LEAVING", "ndcli.firmware.device_set", `{"devices":["fw-a"],"track":"fast","confirm":true}`, "ndcli.firmware.status"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			cs, _ := firmwareClient(t, http.StatusConflict, `{"error":"the server says no","code":"`+tc.code+`"}`)
			resp := callTool(t, cs, tc.tool, tc.args)
			if resp.Success || resp.Error == nil || resp.Error.Code != tc.code {
				t.Fatalf("response = %+v, want error code %s", resp, tc.code)
			}
			if !strings.Contains(resp.Error.Message, "the server says no") || !strings.Contains(resp.Error.Message, tc.hint) {
				t.Errorf("message = %q, want the server's words and %s", resp.Error.Message, tc.hint)
			}
			// A tool caller has no command line: no ndcli command is named.
			if strings.Contains(resp.Error.Message, "'ndcli ") {
				t.Errorf("message names a CLI command: %q", resp.Error.Message)
			}
		})
	}

	t.Run("a refusal with no advice keeps its code", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusConflict, `{"error":"already halted","code":"FIRMWARE_ALREADY_HALTED"}`)
		resp := callTool(t, cs, "ndcli.firmware.version_halt", `{"version":"26.7.5","reason":"r","confirm":true}`)
		if resp.Error == nil || resp.Error.Code != "FIRMWARE_ALREADY_HALTED" || resp.Error.Message != "already halted" {
			t.Errorf("response = %+v", resp)
		}
	})
}

// The tools are listed for every plan: one without the feature gets the server's
// answer and its code.
func TestFirmwareToolsAnswerNotAvailableWithoutThePlan(t *testing.T) {
	body := `{"error":"Firmware tracks are not available on your current plan.","code":"FEATURE_NOT_AVAILABLE"}`
	for tool, args := range map[string]string{
		"ndcli.firmware.config_create": `{}`,
		"ndcli.firmware.track_set":     `{"track":"fast","soak_days":2,"confirm":true}`,
		"ndcli.firmware.device_set":    `{"devices":["fw-a"],"track":"fast","confirm":true}`,
	} {
		cs, _ := firmwareClient(t, http.StatusForbidden, body)
		resp := callTool(t, cs, tool, args)
		if resp.Success || resp.Error == nil || resp.Error.Code != "FEATURE_NOT_AVAILABLE" ||
			resp.Error.Message != "Firmware tracks are not available on your current plan." {
			t.Errorf("%s: response = %+v", tool, resp)
		}
	}
}

// firmwareToolCalls is one call per tool that reaches its route (confirm=true
// where the tool previews first), keyed by tool name.
var firmwareToolCalls = map[string]string{
	"ndcli.firmware.config_show":     `{}`,
	"ndcli.firmware.config_create":   `{}`,
	"ndcli.firmware.config_update":   `{"reboot":true,"confirm":true}`,
	"ndcli.firmware.config_enable":   `{}`,
	"ndcli.firmware.config_disable":  `{"confirm":true}`,
	"ndcli.firmware.config_delete":   `{"confirm":true}`,
	"ndcli.firmware.track_show":      `{}`,
	"ndcli.firmware.track_set":       `{"track":"fast","soak_days":2,"confirm":true}`,
	"ndcli.firmware.track_schedule":  `{"track":"fast","schedule":"night","confirm":true}`,
	"ndcli.firmware.device_set":      `{"devices":["fw-a"],"track":"fast","confirm":true}`,
	"ndcli.firmware.status":          `{}`,
	"ndcli.firmware.history":         `{"device":"fw-a"}`,
	"ndcli.firmware.rollout":         `{}`,
	"ndcli.firmware.version_halt":    `{"version":"26.7.5","reason":"regression","confirm":true}`,
	"ndcli.firmware.version_unhalt":  `{"version":"26.7.5","confirm":true}`,
	"ndcli.firmware.version_promote": `{"version":"26.7.5","track":"all","confirm":true}`,
	"ndcli.firmware.version_halts":   `{}`,
	"ndcli.firmware.failure_clear":   `{"version":"26.7.5","confirm":true}`,
}

// A plan without the feature can refuse any of the routes (every one when the
// organization has no configuration, reads included), and the refusal is the
// server's: its code and its message, as sent, with nothing added.
func TestFirmwareAnswersNotAvailableAsSent(t *testing.T) {
	const message = "Firmware tracks are not available on your current plan."
	body := `{"error":"` + message + `","code":"FEATURE_NOT_AVAILABLE"}`

	var tools []string
	for tool := range firmwareToolCalls {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	want := append([]string(nil), firmwareTools...)
	sort.Strings(want)
	if !reflect.DeepEqual(tools, want) {
		t.Fatalf("calls exist for %v, tools are %v", tools, want)
	}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusForbidden, body)
			resp := callTool(t, cs, tool, firmwareToolCalls[tool])
			if resp.Success || resp.Error == nil || resp.Error.Code != "FEATURE_NOT_AVAILABLE" || resp.Error.Message != message {
				t.Errorf("response = %+v, want FEATURE_NOT_AVAILABLE saying %q", resp, message)
			}
			if seen.hits != 1 {
				t.Errorf("%d request(s) sent, want the one the server refused", seen.hits)
			}
		})
	}
}

// Each tool says what a plan without the feature does to it, in the control
// plane's terms. The sentences are written out here, not read from the
// constants the descriptions are built from: a test that sourced them from the
// implementation would agree with it by construction.
func TestFirmwareToolDescriptionsStateThePlanRule(t *testing.T) {
	tools := toolDefinitions(t)

	// The full rule is on config_show, where a caller starts.
	for _, want := range []string{
		"On a plan that includes firmware tracks it returns FIRMWARE_NOT_CONFIGURED until ndcli.firmware.config_create has run.",
		"On a plan without firmware tracks (ndcli.org.quota shows firmware_tracks_enabled) every ndcli.firmware.* tool answers FEATURE_NOT_AVAILABLE, reads included and before FIRMWARE_NOT_CONFIGURED.",
		"The exception is an organization that already has a firmware configuration (it lost the feature after setting it up): no new steps are sent; devices still in a track are being released.",
		"Its reads keep answering, so that wind-down stays visible, and four changes still work:",
		"detaching a track's schedule (ndcli.firmware.track_schedule with no_schedule=true),",
		"moving devices to unmanaged (ndcli.firmware.device_set with track=unmanaged),",
		"ndcli.firmware.config_disable and ndcli.firmware.config_delete.",
		"Every other change is refused with FEATURE_NOT_AVAILABLE.",
		"plan_enabled (whether the plan includes firmware tracks; if false, no new steps are sent; devices still in a track are being released)",
	} {
		if d := tools["ndcli.firmware.config_show"].Description; !strings.Contains(d, want) {
			t.Errorf("ndcli.firmware.config_show is missing %q:\n%s", want, d)
		}
	}

	const (
		needsPlan = "Needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise)."
		windDown  = "works on a plan without firmware tracks while the organization has a firmware configuration, so the configuration can be wound down."
	)
	// Every write the control plane refuses on a plan without the feature.
	for _, name := range []string{
		"config_create", "config_update", "config_enable", "track_set",
		"version_halt", "version_unhalt", "version_promote", "failure_clear",
	} {
		if d := tools["ndcli.firmware."+name].Description; !strings.Contains(d, needsPlan) {
			t.Errorf("ndcli.firmware.%s should say %q:\n%s", name, needsPlan, d)
		}
	}
	// The two that stay open whole, so the configuration can be wound down.
	for _, name := range []string{"config_disable", "config_delete"} {
		d := tools["ndcli.firmware."+name].Description
		if !strings.Contains(d, "Still "+windDown) {
			t.Errorf("ndcli.firmware.%s should say it still %s:\n%s", name, windDown, d)
		}
		if strings.Contains(d, needsPlan) {
			t.Errorf("ndcli.firmware.%s is open on a plan without the feature, but its description says it needs one:\n%s", name, d)
		}
	}
	// A delete is refused while devices are in a track, and both ways to release
	// them are open on a plan without the feature.
	if d := tools["ndcli.firmware.config_delete"].Description; !strings.Contains(d,
		"Both ways of releasing its devices, ndcli.firmware.config_disable and ndcli.firmware.device_set with track=unmanaged, work there too.") {
		t.Errorf("ndcli.firmware.config_delete should say that releasing the devices works on such a plan:\n%s", d)
	}
	// track_schedule is both: attaching or moving is refused, detaching is open.
	schedule := tools["ndcli.firmware.track_schedule"].Description
	for _, want := range []string{
		"Attaching or moving needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise);",
		"detaching (no_schedule=true) still " + windDown,
	} {
		if !strings.Contains(schedule, want) {
			t.Errorf("ndcli.firmware.track_schedule is missing %q:\n%s", want, schedule)
		}
	}
	// device_set is both as well: moving to a track is refused, moving to
	// unmanaged is open.
	deviceSet := tools["ndcli.firmware.device_set"].Description
	for _, want := range []string{
		"Moving devices to a track needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise) and an enabled configuration;",
		"moving them to unmanaged (track=unmanaged) still " + windDown,
	} {
		if !strings.Contains(deviceSet, want) {
			t.Errorf("ndcli.firmware.device_set is missing %q:\n%s", want, deviceSet)
		}
	}
	if strings.Contains(deviceSet, "refused too") {
		t.Errorf("ndcli.firmware.device_set says moving to unmanaged is refused, but it is open on a plan without the feature:\n%s", deviceSet)
	}
	// The rollout board carries plan_enabled too.
	if d := tools["ndcli.firmware.rollout"].Description; !strings.Contains(d,
		"plan_enabled is whether the plan includes firmware tracks; if false, no new steps are sent; devices still in a track are being released.") {
		t.Errorf("ndcli.firmware.rollout should explain plan_enabled:\n%s", d)
	}
	// The control plane acts on a plan without the feature, releasing the
	// devices, so no description may say that nothing happens.
	for name, tool := range tools {
		if strings.HasPrefix(name, "ndcli.firmware.") || name == "ndcli.org.quota" {
			if strings.Contains(strings.ToLower(tool.Description), "nothing acts") {
				t.Errorf("%s says nothing acts, but a plan without the feature releases the devices:\n%s", name, tool.Description)
			}
		}
	}
	// The reads carry no sentence of their own: they answer for an organization
	// that has a configuration, which config_show says once.
	for _, name := range []string{"track_show", "status", "history", "rollout", "version_halts"} {
		if d := tools["ndcli.firmware."+name].Description; strings.Contains(d, "Needs a plan") {
			t.Errorf("ndcli.firmware.%s is a read and must not say it needs a plan:\n%s", name, d)
		}
	}
}

func TestRunFirmwareUpgradePointsAtTheFirmwareTools(t *testing.T) {
	body := `{"error":"Device 'fw1' is in a firmware track, which moves it between releases; move it out of its track to run a firmware upgrade on it.","code":"FIRMWARE_DEVICE_MANAGED"}`
	for _, args := range []string{
		`{"devices":["fw1"],"mode":"minor","confirm":true}`,
		`{"devices":["fw1"],"mode":"minor","schedule":"nightly","confirm":true}`,
		`{"devices":["fw1"],"mode":"major","confirm":true}`,
		`{"devices":["fw1"],"mode":"major","schedule":"nightly","confirm":true}`,
	} {
		cs, _ := firmwareClient(t, http.StatusConflict, body)
		resp := callTool(t, cs, "ndcli.run.firmware_upgrade", args)
		if resp.Success || resp.Error == nil || resp.Error.Code != "FIRMWARE_DEVICE_MANAGED" {
			t.Fatalf("%s: response = %+v", args, resp)
		}
		for _, want := range []string{"Device 'fw1' is in a firmware track", "ndcli.firmware.status", "ndcli.firmware.device_set with track=unmanaged"} {
			if !strings.Contains(resp.Error.Message, want) {
				t.Errorf("%s: message %q is missing %q", args, resp.Error.Message, want)
			}
		}
	}
}

// The tools the vocabulary names for the major upgrade exist, with the
// parameters it names.
func TestFirmwareMCPVocabularyNamesTheMajorUpgradeTools(t *testing.T) {
	tools := toolDefinitions(t)
	if _, ok := tools[firmwareMCPVocabulary.Rollout]; !ok {
		t.Errorf("Rollout = %q is not a tool", firmwareMCPVocabulary.Rollout)
	}
	props, _ := schemaProps(t, tools["ndcli.firmware.device_set"])
	for name, command := range map[string]string{"DeviceAdd": firmwareMCPVocabulary.DeviceAdd, "DeviceAddWithSetting": firmwareMCPVocabulary.DeviceAddWithSetting} {
		if !strings.HasPrefix(command, "ndcli.firmware.device_set with track=<track>") || strings.Contains(command, "'ndcli ") {
			t.Errorf("%s = %q is not a call of ndcli.firmware.device_set", name, command)
		}
	}
	for _, param := range []string{"track", "boot_environment"} {
		if _, ok := props[param]; !ok {
			t.Errorf("ndcli.firmware.device_set has no %q parameter, which the advice names", param)
		}
	}
}

// The MCP vocabulary names the tool and its parameters, and fills the track's
// name in where the refusal gives it; the tool it names exists with them.
func TestFirmwareMCPVocabularyNamesTheTrackScheduleTool(t *testing.T) {
	for name, command := range map[string]string{
		"TrackDetach": firmwareMCPVocabulary.TrackDetach, "TrackSchedule": firmwareMCPVocabulary.TrackSchedule,
	} {
		if n := strings.Count(command, service.FirmwareTrackPlaceholder); n != 1 {
			t.Errorf("%s = %q carries the track placeholder %d times, want once", name, command, n)
		}
		if !strings.HasPrefix(command, "ndcli.firmware.track_schedule with ") || strings.Contains(command, "'ndcli ") {
			t.Errorf("%s = %q is not a call of ndcli.firmware.track_schedule", name, command)
		}
	}
	props, _ := schemaProps(t, toolDefinitions(t)["ndcli.firmware.track_schedule"])
	for _, param := range []string{"track", "schedule", "no_schedule"} {
		if _, ok := props[param]; !ok {
			t.Errorf("ndcli.firmware.track_schedule has no %q parameter, which the advice names", param)
		}
	}
	if got := firmwareMCPVocabulary.DetachCommand("fast"); got != "ndcli.firmware.track_schedule with track=fast, no_schedule=true" {
		t.Errorf("DetachCommand(fast) = %q", got)
	}
	if got := firmwareMCPVocabulary.ScheduleCommand("fast"); got != "ndcli.firmware.track_schedule with track=fast, schedule=<name>" {
		t.Errorf("ScheduleCommand(fast) = %q", got)
	}
}

// The generic routes refuse a track's schedule entry with a body that names no
// track, so the way forward is the template.
func TestScheduleTaskToolsPointFirmwareSpecsAtTheirTrack(t *testing.T) {
	const body = `{"error":"This schedule entry belongs to a firmware track. Attach or detach the track's schedule through the firmware configuration instead.","code":"FIRMWARE_SPEC_MANAGED"}`
	for tool, args := range map[string]string{
		"ndcli.schedule.tasks.enable":  `{"code":"Fw123456"}`,
		"ndcli.schedule.tasks.disable": `{"code":"Fw123456"}`,
		"ndcli.schedule.tasks.remove":  `{"code":"Fw123456","confirm":true}`,
	} {
		cs, _ := firmwareClient(t, http.StatusConflict, body)
		resp := callTool(t, cs, tool, args)
		if resp.Success || resp.Error == nil || resp.Error.Code != "FIRMWARE_SPEC_MANAGED" {
			t.Fatalf("%s: response = %+v", tool, resp)
		}
		// The way forward is in tool words, and detaching is named.
		for _, want := range []string{
			"This schedule entry belongs to a firmware track. Attach or detach the track's schedule through the firmware configuration instead.\n  Hint: ",
			"To detach the track from its schedule: ndcli.firmware.track_schedule with track=<track>, no_schedule=true.",
			"To move it to another schedule: ndcli.firmware.track_schedule with track=<track>, schedule=<name>.",
		} {
			if !strings.Contains(resp.Error.Message, want) {
				t.Errorf("%s: message %q is missing %q", tool, resp.Error.Message, want)
			}
		}
		if strings.Contains(resp.Error.Message, "'ndcli ") {
			t.Errorf("%s: a tool caller has no command line, but the message names one: %q", tool, resp.Error.Message)
		}
	}
}

// A schedule a track's windows run on cannot be deleted. NDManager lists the
// tracks in the refusal (conveyor order, lowercase), and the tool words name
// each one: the call to detach it is complete but for the schedule to move to.
func TestScheduleDeleteToolNamesTheTracksThatBlockIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		hint string
	}{
		{"one track",
			`{"error":"This schedule carries the update window of firmware track fast. Deleting it would leave its devices pinned with nothing to move them. Detach the track from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast"],"count":1}`,
			"To detach the track from its schedule: ndcli.firmware.track_schedule with track=fast, no_schedule=true. " +
				"To move it to another schedule: ndcli.firmware.track_schedule with track=fast, schedule=<name>."},
		{"two tracks",
			`{"error":"This schedule carries the update windows of firmware tracks fast, regular. Deleting it would leave their devices pinned with nothing to move them. Detach the tracks from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast","regular"],"count":2}`,
			"To detach the tracks from their schedule: ndcli.firmware.track_schedule with track=fast, no_schedule=true; " +
				"ndcli.firmware.track_schedule with track=regular, no_schedule=true. " +
				"To move them to another schedule: ndcli.firmware.track_schedule with track=fast, schedule=<name>; " +
				"ndcli.firmware.track_schedule with track=regular, schedule=<name>."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var served struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(tc.body), &served); err != nil {
				t.Fatal(err)
			}
			cs, seen := firmwareClient(t, http.StatusConflict, tc.body)
			resp := callTool(t, cs, "ndcli.schedule.delete", `{"name":"night","confirm":true}`)
			if resp.Success || resp.Error == nil || resp.Error.Code != "FIRMWARE_SPEC_MANAGED" {
				t.Fatalf("response = %+v", resp)
			}
			if seen.method != http.MethodDelete || seen.path != "/api/v1/organizations/acme/schedules/night" {
				t.Errorf("sent %s %s", seen.method, seen.path)
			}
			if want := served.Error + "\n  Hint: " + tc.hint; resp.Error.Message != want {
				t.Errorf("message =\n%q\nwant\n%q", resp.Error.Message, want)
			}
			if strings.Contains(resp.Error.Message, "<track>") || strings.Contains(resp.Error.Message, "'ndcli ") {
				t.Errorf("the tracks are named and a tool caller has no command line: %q", resp.Error.Message)
			}
		})
	}
}

// In a description as in the advice, <track> stands for a track's name and
// <name> for a schedule's: the delete tool tells the caller to detach a track,
// so the name it fills in is the track's.
func TestScheduleDeleteToolDescriptionNamesTheTrackWithItsPlaceholder(t *testing.T) {
	tools := toolDefinitions(t)
	if d, want := tools["ndcli.schedule.delete"].Description, "track="+service.FirmwareTrackPlaceholder; !strings.Contains(d, want) {
		t.Errorf("ndcli.schedule.delete should name the track to detach as %s: %s", want, d)
	}
	for name, tool := range tools {
		if strings.Contains(tool.Description, "track=<name>") {
			t.Errorf("%s writes track=<name>, but <name> is a schedule's name and a track's is %s: %s",
				name, service.FirmwareTrackPlaceholder, tool.Description)
		}
	}
}

// ── the FIRMWARE_TRACK task result ───────────────────────────────────────────

func TestTaskToolsCarryTheFirmwareTrackResult(t *testing.T) {
	const result = `{"action":"advance","track":"regular","outcome":"success","reason":"applied","drift":false,"reinstall_entries":0,` +
		`"from_version":"26.7.3","to_version":"26.7.5","target_version":"26.7.5","pinned_version":"26.7.5"}`
	message, _ := json.Marshal(result)
	task := func(taskType, msg string) string {
		return `{"task":"abcd1234","type":"` + taskType + `","status":"COMPLETED","device_uuid":"u","device_name":"fw-a","organization":"acme","message":` + msg + `}`
	}

	t.Run("describe", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, task("FIRMWARE_TRACK", string(message)))
		resp := callTool(t, cs, "ndcli.task.describe", `{"task":"abcd1234"}`)
		taskData := resp.Data.(map[string]interface{})["task"].(map[string]interface{})
		got, ok := taskData["firmware_track_result"].(map[string]interface{})
		if !ok {
			t.Fatalf("no firmware_track_result in %v", taskData)
		}
		for k, want := range map[string]interface{}{
			"action": "advance", "track": "regular", "outcome": "success", "reason": "applied", "drift": false,
			"from_version": "26.7.3", "to_version": "26.7.5", "target_version": "26.7.5", "pinned_version": "26.7.5",
		} {
			if got[k] != want {
				t.Errorf("firmware_track_result.%s = %v, want %v", k, got[k], want)
			}
		}
		// The raw message stays, for a caller that wants it.
		if taskData["message"] != result {
			t.Errorf("message = %v", taskData["message"])
		}
	})
	t.Run("describe leaves other tasks and other messages alone", func(t *testing.T) {
		for name, body := range map[string]string{
			"another type":  task("SYNC", string(message)),
			"pending":       task("FIRMWARE_TRACK", `"Firmware track fast: advance"`),
			"a plain error": task("FIRMWARE_TRACK", `"firmware check failed: connection refused"`),
		} {
			cs, _ := firmwareClient(t, http.StatusOK, body)
			resp := callTool(t, cs, "ndcli.task.describe", `{"task":"abcd1234"}`)
			taskData := resp.Data.(map[string]interface{})["task"].(map[string]interface{})
			if _, present := taskData["firmware_track_result"]; present {
				t.Errorf("%s: firmware_track_result must be absent, not null: %v", name, taskData)
			}
		}
	})
	t.Run("list", func(t *testing.T) {
		list := `{"items":[` + task("FIRMWARE_TRACK", string(message)) + `,` + task("PING", `"pong"`) + `],"total":2,"page":1,"per_page":30,"pages":1}`
		cs, _ := firmwareClient(t, http.StatusOK, list)
		resp := callTool(t, cs, "ndcli.task.list", `{"type":["FIRMWARE_TRACK","PING"]}`)
		tasks := resp.Data.(map[string]interface{})["tasks"].([]interface{})
		if len(tasks) != 2 {
			t.Fatalf("tasks = %v", tasks)
		}
		first := tasks[0].(map[string]interface{})
		if r, ok := first["firmware_track_result"].(map[string]interface{}); !ok || r["outcome"] != "success" {
			t.Errorf("the firmware task carries no result: %v", first)
		}
		if _, present := tasks[1].(map[string]interface{})["firmware_track_result"]; present {
			t.Errorf("a PING task must carry no firmware result: %v", tasks[1])
		}
	})
	t.Run("an escape sequence in the result is stripped", func(t *testing.T) {
		hostile := `{"action":"advance","track":"fast","outcome":"held","reason":"held_\u001b[2Jbusy","drift":false,"reinstall_entries":0}`
		quoted, _ := json.Marshal(hostile)
		cs, _ := firmwareClient(t, http.StatusOK, task("FIRMWARE_TRACK", string(quoted)))
		resp := callTool(t, cs, "ndcli.task.describe", `{"task":"abcd1234"}`)
		taskData := resp.Data.(map[string]interface{})["task"].(map[string]interface{})

		// The parsed result is what a caller reads, and it is clean. (The raw
		// message keeps the six literal characters \u001b the control plane
		// sent: text, not an escape byte, and unchanged from every other task.)
		parsed := taskData["firmware_track_result"].(map[string]interface{})
		if parsed["reason"] != "held_[2Jbusy" {
			t.Errorf("reason = %q, want the escape stripped", parsed["reason"])
		}
		var walk func(interface{})
		walk = func(v interface{}) {
			switch x := v.(type) {
			case string:
				for _, r := range x {
					if r < 0x20 && r != '\n' && r != '\t' && r != '\r' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
						t.Errorf("a control character reached a decoded tool string: %q", x)
					}
				}
			case []interface{}:
				for _, e := range x {
					walk(e)
				}
			case map[string]interface{}:
				for _, e := range x {
					walk(e)
				}
			}
		}
		walk(resp.Data)
	})
}

// Everything a firmware tool returns went through the sanitizing decode: a
// hostile control plane cannot put a terminal sequence in the model's context.
func TestFirmwareToolOutputIsSanitized(t *testing.T) {
	hostile := `{"items":[{"device_name":"fw\u001b[2J-a","device_uuid":"u","device_status":"ENABLED","track":"fast","state":"held","held_reason":"x\u001b]0;pwn\u0007","released":{"at":"2026-10-01T00:00:00Z","reason":"dead\u009b2J"}}],"total":1,"page":1,"per_page":50,"pages":1}`
	cs, _ := firmwareClient(t, http.StatusOK, hostile)
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "ndcli.firmware.status", Arguments: map[string]interface{}{}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, bad := range []string{"\x1b", `\u001b`, `\u0007`, "\u009b", `\u009b`} {
		if strings.Contains(text, bad) {
			t.Errorf("the tool output carries %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "fw[2J-a") {
		t.Errorf("the device name should survive, minus the escape:\n%s", text)
	}
}

// ── major upgrades ───────────────────────────────────────────────────────────

const (
	mcpTrackOn        = `{"track":"fast","soak_days":1,"min_success":0,"scheduled":false,"auto_apply_major":true,"devices":{"total":1,"managed":1}}`
	mcpTrackOff       = `{"track":"fast","soak_days":1,"min_success":0,"scheduled":false,"auto_apply_major":false,"cancelled_tasks":2,"devices":{"total":1,"managed":1}}`
	mcpAssignSetting  = `{"organization_name":"acme","track":null,"results":[{"device_name":"fw-a","result":"updated","track":"fast","state":"managed","boot_environment":false,"notes":[]}],"cancelled_tasks":1}`
	mcpAssignBoth     = `{"organization_name":"acme","track":"regular","results":[{"device_name":"fw-c","result":"added","track":"regular","state":"enrolling","boot_environment":true,"notes":[]}],"cancelled_tasks":0}`
	mcpSeriesHalt     = `{"halt":{"series":"26.7","kind":"major","reason":"payload changed","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"},"cancelled_tasks":2}`
	mcpSeriesUnhalt   = `{"halt":{"series":"26.7","kind":"major","reason":"payload changed","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com","unhalted_at":"2026-10-09T12:00:00Z","unhalted_by":"ops@acme.com"},"cancelled_tasks":0}`
	mcpSeriesPromote  = `{"series":"26.7","results":[{"track":"fast","result":"approved","target":"26.7"}]}`
	mcpSeriesCleared  = `{"series":"26.7","cleared":1}`
	mcpHaltsBothKinds = `{"items":[{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"t","halted_by":"a"},{"series":"26.7","kind":"major","reason":"r","halted_at":"t","halted_by":"a"}],"total":2,"page":1,"per_page":50,"pages":1}`
	mcpDevicesMajor   = `{"items":[{"device_name":"fw-b","device_uuid":"u2","device_status":"ENABLED","track":"fast","state":"managed","pinned_version":"26.1.11_10","series":"26.1","held_reason":"held_ha_member","major":{"eligible":false,"blocked_by":"held_ha_member"},"boot_environment":{"setting":false,"last":"off"},"notes":["boot_environment_off","third_party_package"]}],"total":1,"page":1,"per_page":50,"pages":1}`
	mcpRolloutMajor   = `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[],"failures":[],"halts":[],"majors":[{"track":"fast","series":"26.7","source_series":"26.1","state":"path_closed","stalled":false,"auto_apply_major":false,"devices_eligible":0,"devices_on_series":0,"devices_hopped":0,"devices_awaiting_minor_evidence":0,"offer_not_seen":0,"blocked_reason":"path_closed","source_series_end_of_life":false}],"major_failures":[{"kind":"major","series":"26.7","track":"fast","devices":["fw-b"],"count":1,"last_at":"2026-10-08T12:00:00Z"}],"agent_repo":{"configured":false,"trees":[]}}`
)

// Turning major upgrades on is the operator's explicit decision: the preview
// says what it means, in the message the model reads and as data, and nothing is
// sent until the model comes back with confirm=true.
func TestFirmwareMajorSwitchPreviewCarriesTheConsequences(t *testing.T) {
	cs, seen := firmwareClient(t, http.StatusOK, mcpTrackOn)

	preview := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":true}`)
	data, _ := preview.Data.(map[string]interface{})
	if !preview.Success || data["preview"] != true || seen.hits != 0 {
		t.Fatalf("without confirm the switch must preview and send nothing: %+v, %d hit(s)", preview, seen.hits)
	}
	// Written out here, not read from the constants the preview is built from.
	for _, want := range []string{
		"Preview: Would turn major upgrades on for firmware track 'fast'.",
		"Show the operator these consequences and ask for their go-ahead:",
		"With major upgrades on, the track's update windows move its devices to the next OPNsense series. Each such upgrade:",
		"takes the device offline for the length of the upgrade and reboots it three times;",
		"has no automatic rollback;",
		"a recovery path from the console only",
		"after a manual rollback, needs the device rebound",
		"Set confirm=true to execute.",
	} {
		if !strings.Contains(preview.Message, want) {
			t.Errorf("preview message is missing %q:\n%s", want, preview.Message)
		}
	}
	consequences, _ := data["consequences"].([]interface{})
	if len(consequences) != 7 || data["lead"] == "" || data["auto_apply_major"] != true || data["track"] != "fast" || data["organization"] != "acme" {
		t.Errorf("preview data = %v", data)
	}

	resp := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":true,"confirm":true}`)
	if !resp.Success {
		t.Fatalf("confirmed call failed: %+v", resp.Error)
	}
	if seen.method != "PUT" || seen.path != "/api/v1/organizations/acme/firmware/tracks/fast" ||
		!reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": true}) {
		t.Errorf("sent %s %s %v", seen.method, seen.path, seen.body)
	}
	if resp.Message != "Firmware track fast now applies major upgrades" {
		t.Errorf("message = %q", resp.Message)
	}
	if track := resp.Data.(map[string]interface{})["track"].(map[string]interface{}); track["auto_apply_major"] != true {
		t.Errorf("the track is returned as named data: %v", resp.Data)
	}

	t.Run("off previews too, and says what it cancels", func(t *testing.T) {
		cs, seen := firmwareClient(t, http.StatusOK, mcpTrackOff)
		preview := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":false}`)
		if !preview.Success || seen.hits != 0 || !strings.Contains(preview.Message, "Would turn major upgrades off for firmware track 'fast'") ||
			!strings.Contains(preview.Message, "queued for the track's devices and not yet dispatched are cancelled; an upgrade already running goes on") {
			t.Fatalf("preview = %+v, %d hit(s)", preview, seen.hits)
		}
		if strings.Contains(preview.Message, "no automatic rollback") {
			t.Errorf("the safe direction carries no warning of the dangerous one: %q", preview.Message)
		}
		resp := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":false,"confirm":true}`)
		if !resp.Success || !reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": false}) ||
			resp.Message != "Firmware track fast no longer applies major upgrades (2 queued upgrade step(s) cancelled)" {
			t.Errorf("resp = %+v, body %v", resp, seen.body)
		}
	})
	// Every NDManager that predates the switch ignores the key, sees an update
	// that names no field and answers 400 MISSING_FIELD; the missing echo is the
	// second guard. Either way the caller is told why, with the code that says so
	// and not the MISSING_FIELD that would send it looking for a field it gave.
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"the 400 an NDManager that predates the switch gives": {http.StatusBadRequest, `{"error":"Provide at least one field to change","code":"MISSING_FIELD"}`},
		"an answer that does not carry the switch":            {http.StatusOK, mcpTrack},
	} {
		t.Run(name+" is reported as such, with its code", func(t *testing.T) {
			cs, seen := firmwareClient(t, tc.status, tc.body)
			resp := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"regular","auto_apply_major":true,"confirm":true}`)
			if resp.Success || resp.Error == nil || resp.Error.Code != "SERVER_TOO_OLD" ||
				!strings.HasPrefix(resp.Error.Message, "this NDManager predates major upgrades: ") {
				t.Errorf("resp = %+v", resp)
			}
			if seen.hits != 1 || !reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": true}) {
				t.Errorf("%d hit(s), body %v", seen.hits, seen.body)
			}
		})
	}
	t.Run("a MISSING_FIELD for a call that did not carry the switch keeps its own code", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusBadRequest, `{"error":"Provide at least one field to change","code":"MISSING_FIELD"}`)
		resp := callTool(t, cs, "ndcli.firmware.track_set", `{"track":"regular","soak_days":3,"confirm":true}`)
		if resp.Success || resp.Error == nil || resp.Error.Code != "MISSING_FIELD" {
			t.Errorf("resp = %+v", resp)
		}
	})
}

// Lifting the halt on a major upgrade can start upgrades at the next window, so
// its preview says so, as the promotion of one does; a release's does not.
func TestFirmwareUnhaltOfAMajorUpgradeSaysWhatFollows(t *testing.T) {
	cs, seen := firmwareClient(t, http.StatusOK, mcpSeriesUnhalt)
	preview := callTool(t, cs, "ndcli.firmware.version_unhalt", `{"version":"26.7","kind":"major"}`)
	if !preview.Success || seen.hits != 0 {
		t.Fatalf("a preview sends nothing: %+v, %d hit(s)", preview, seen.hits)
	}
	for _, want := range []string{
		"Preview: Would lift the halt on firmware major upgrade to series '26.7'.",
		"A track with auto_apply_major on that has approved it will then upgrade its eligible devices at its next window: offline for the length of the upgrade, three reboots, no automatic rollback.",
		"Set confirm=true to execute.",
	} {
		if !strings.Contains(preview.Message, want) {
			t.Errorf("preview message is missing %q:\n%s", want, preview.Message)
		}
	}
	cs, _ = firmwareClient(t, http.StatusOK, mcpHalt)
	if release := callTool(t, cs, "ndcli.firmware.version_unhalt", `{"version":"26.7.5"}`); strings.Contains(release.Message, "automatic rollback") {
		t.Errorf("a release's unhalt must not warn of an upgrade: %q", release.Message)
	}
}

// A preview that warns carries the warning as data too, so a caller that reads
// the named fields cannot confirm without it; the previews that have nothing to
// warn of have no note.
func TestFirmwarePreviewNotesAreData(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tool     string
		args     string
		response string
		want     string // "" when there is no note
	}{
		{"the switch on", "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":true}`, mcpTrackOn, models.FirmwareMajorSwitchLead},
		{"the switch off", "ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":false}`, mcpTrackOff, "not yet dispatched are cancelled"},
		{"unhalt of a major upgrade", "ndcli.firmware.version_unhalt", `{"version":"26.7","kind":"major"}`, mcpSeriesUnhalt, "no automatic rollback"},
		{"promote of a major upgrade", "ndcli.firmware.version_promote", `{"version":"26.7","kind":"major","track":"fast"}`, mcpSeriesPromote, "no automatic rollback"},
		{"unhalt of a release", "ndcli.firmware.version_unhalt", `{"version":"26.7.5"}`, mcpHalt, ""},
		{"promote of a release", "ndcli.firmware.version_promote", `{"version":"26.7.5","track":"fast"}`, mcpPromote, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.response)
			preview := callTool(t, cs, tc.tool, tc.args)
			data, _ := preview.Data.(map[string]interface{})
			if !preview.Success || data["preview"] != true || seen.hits != 0 {
				t.Fatalf("not a preview: %+v, %d hit(s)", preview, seen.hits)
			}
			note, has := data["note"].(string)
			if tc.want == "" {
				if has {
					t.Errorf("a preview with nothing to warn of has a note: %q", note)
				}
				return
			}
			if !strings.Contains(note, tc.want) || !strings.Contains(preview.Message, note) {
				t.Errorf("note = %q; want it to contain %q and to be in the message %q", note, tc.want, preview.Message)
			}
		})
	}
}

// The same tools, more arguments: each form previews without confirm and, with it,
// sends what the service sends.
func TestFirmwareMajorToolsPreviewThenRun(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		args     string
		response string
		method   string
		path     string
		body     map[string]interface{}
		preview  string
		message  string
	}{
		{"ndcli.firmware.device_set", `{"devices":["fw-a"],"boot_environment":false}`, mcpAssignSetting, "PUT", "/api/v1/organizations/acme/firmware/devices",
			map[string]interface{}{"devices": []interface{}{"fw-a"}, "boot_environment": false}, "fw-a boot_environment=false", "1 device(s): boot_environment=false"},
		{"ndcli.firmware.device_set", `{"devices":["fw-c"],"track":"Regular","boot_environment":true}`, mcpAssignBoth, "PUT", "/api/v1/organizations/acme/firmware/devices",
			map[string]interface{}{"devices": []interface{}{"fw-c"}, "track": "regular", "boot_environment": true}, "fw-c → regular, boot_environment=true", "1 device(s) → regular, boot_environment=true"},
		{"ndcli.firmware.version_halt", `{"version":"26.7","kind":"major","reason":"payload changed"}`, mcpSeriesHalt, "POST", "/api/v1/organizations/acme/firmware/halts",
			map[string]interface{}{"series": "26.7", "reason": "payload changed"}, "halt firmware major upgrade to series '26.7'", "Major upgrade to 26.7 halted (2 pending step(s) cancelled)"},
		{"ndcli.firmware.version_unhalt", `{"version":"26.7","kind":"major"}`, mcpSeriesUnhalt, "DELETE", "/api/v1/organizations/acme/firmware/halts/series/26.7",
			nil, "lift the halt on firmware major upgrade to series '26.7'", "Halt on major upgrade to 26.7 lifted"},
		{"ndcli.firmware.version_promote", `{"version":"26.7","kind":"major","track":"fast"}`, mcpSeriesPromote, "POST", "/api/v1/organizations/acme/firmware/promote",
			map[string]interface{}{"series": "26.7", "track": "fast"}, "promote firmware major upgrade to series without waiting for its soak '26.7 → fast'", "Major upgrade to 26.7 promoted"},
		{"ndcli.firmware.failure_clear", `{"version":"26.7","kind":"major","track":"fast"}`, mcpSeriesCleared, "POST", "/api/v1/organizations/acme/firmware/failures/clear",
			map[string]interface{}{"series": "26.7", "track": "fast"}, "clear the failures blocking firmware major upgrade to series '26.7 (fast track)'", "Cleared 1 failure record(s) for major upgrade to 26.7"},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.response)
			preview := callTool(t, cs, tc.tool, tc.args)
			data, _ := preview.Data.(map[string]interface{})
			if !preview.Success || data["preview"] != true || seen.hits != 0 {
				t.Fatalf("without confirm the tool must preview and send nothing: %+v, %d hit(s)", preview, seen.hits)
			}
			if !strings.Contains(preview.Message, tc.preview) || !strings.Contains(preview.Message, "confirm=true") {
				t.Errorf("preview message = %q, want it to say %q and ask for confirm=true", preview.Message, tc.preview)
			}
			resp := callTool(t, cs, tc.tool, strings.TrimSuffix(tc.args, "}")+`,"confirm":true}`)
			if !resp.Success {
				t.Fatalf("confirmed call failed: %+v", resp.Error)
			}
			if seen.method != tc.method || seen.path != tc.path || !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("sent %s %s %v, want %s %s %v", seen.method, seen.path, seen.body, tc.method, tc.path, tc.body)
			}
			if resp.Message != tc.message {
				t.Errorf("message = %q, want %q", resp.Message, tc.message)
			}
		})
	}

	t.Run("promoting a major upgrade says what follows", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpSeriesPromote)
		preview := callTool(t, cs, "ndcli.firmware.version_promote", `{"version":"26.7","kind":"major","track":"all"}`)
		for _, want := range []string{
			"A track with auto_apply_major on will then upgrade its eligible devices at its next window:",
			"offline for the length of the upgrade, three reboots, no automatic rollback.",
		} {
			if !strings.Contains(preview.Message, want) {
				t.Errorf("preview message is missing %q:\n%s", want, preview.Message)
			}
		}
		// A release's promotion says none of it.
		cs, _ = firmwareClient(t, http.StatusOK, mcpPromote)
		if release := callTool(t, cs, "ndcli.firmware.version_promote", `{"version":"26.7.5","track":"all"}`); strings.Contains(release.Message, "automatic rollback") {
			t.Errorf("a release's promotion must not warn of an upgrade: %q", release.Message)
		}
	})
	t.Run("the default kind is a release, whatever the name looks like", func(t *testing.T) {
		cs, seen := firmwareClient(t, http.StatusOK, mcpHalt)
		callTool(t, cs, "ndcli.firmware.version_halt", `{"version":"26.7","reason":"r","confirm":true}`)
		if !reflect.DeepEqual(seen.body, map[string]interface{}{"version": "26.7", "reason": "r"}) {
			t.Errorf("body = %v; 26.7 without a kind is the GA release", seen.body)
		}
	})
}

// The message says what the request changed, not what the answer echoes: a
// control plane that answers a track move with no track, or a setting with none,
// still gets a message that names every change, and no panic.
func TestFirmwareDeviceSetMessageDoesNotAssumeTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, args, want string
	}{
		{"a move the answer does not carry", `{"devices":["fw-a"],"track":"fast","confirm":true}`, "1 device(s) → fast"},
		{"a move with the setting, the answer carrying only the setting",
			`{"devices":["fw-a"],"track":"fast","boot_environment":false,"confirm":true}`, "1 device(s) → fast, boot_environment=false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, _ := firmwareClient(t, http.StatusOK, mcpAssignSetting)
			resp := callTool(t, cs, "ndcli.firmware.device_set", tc.args)
			if !resp.Success || resp.Message != tc.want {
				t.Errorf("resp = %+v, want message %q", resp, tc.want)
			}
		})
	}
}

func TestFirmwareMajorToolsRefuseBadInputBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args string
		want string
	}{
		{"ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":true,"soak_days":2,"confirm":true}`, "auto_apply_major is changed on its own"},
		{"ndcli.firmware.track_set", `{"track":"fast","auto_apply_major":true,"min_success":0}`, "auto_apply_major is changed on its own"},
		{"ndcli.firmware.track_set", `{"track":"unmanaged","auto_apply_major":true}`, `unknown track "unmanaged"`},
		{"ndcli.firmware.device_set", `{"devices":["fw-a"],"confirm":true}`, "give a track, a boot_environment setting, or both"},
		{"ndcli.firmware.device_set", `{"devices":["fw-a"],"track":"unmanaged","boot_environment":false}`, "boot_environment cannot be set while moving devices to unmanaged"},
		{"ndcli.firmware.device_set", `{"devices":[],"boot_environment":false,"confirm":true}`, "at least one device name is required"},
		{"ndcli.firmware.version_halt", `{"version":"26.7","kind":"all","reason":"r"}`, `unknown kind "all"`},
		{"ndcli.firmware.version_halt", `{"version":" ","kind":"major","reason":"r"}`, "series is required"},
		{"ndcli.firmware.version_unhalt", `{"version":"26.7","kind":"everything"}`, `unknown kind "everything"`},
		{"ndcli.firmware.version_promote", `{"version":"26.7","kind":"major","track":"unmanaged"}`, `unknown track "unmanaged"`},
		{"ndcli.firmware.failure_clear", `{"version":"","kind":"major"}`, "series is required"},
		{"ndcli.firmware.version_halts", `{"kind":"everything"}`, `unknown kind "everything"`},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, `{}`)
			resp := callTool(t, cs, tc.tool, tc.args)
			if resp.Success || resp.Error == nil || resp.Error.Code != "INVALID_INPUT" || !strings.Contains(resp.Error.Message, tc.want) {
				t.Fatalf("response = %+v, want INVALID_INPUT saying %q", resp, tc.want)
			}
			if seen.hits != 0 {
				t.Errorf("%d request(s) sent for refused input", seen.hits)
			}
		})
	}
}

func TestFirmwareMajorReadTools(t *testing.T) {
	t.Run("halts of both kinds", func(t *testing.T) {
		cs, seen := firmwareClient(t, http.StatusOK, mcpHaltsBothKinds)
		resp := callTool(t, cs, "ndcli.firmware.version_halts", `{"kind":"all"}`)
		if !resp.Success || seen.query.Get("kind") != "all" {
			t.Fatalf("resp = %+v, query %v", resp, seen.query)
		}
		halts := resp.Data.(map[string]interface{})["halts"].([]interface{})
		release, series := halts[0].(map[string]interface{}), halts[1].(map[string]interface{})
		if release["version"] != "26.7.5" || release["kind"] != "minor" || series["series"] != "26.7" || series["kind"] != "major" {
			t.Errorf("halts = %v", halts)
		}
		if _, hasVersion := series["version"]; hasVersion {
			t.Errorf("a halt of a major upgrade carries no version: %v", series)
		}
	})
	t.Run("the device rows carry what the server sent as named data", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpDevicesMajor)
		resp := callTool(t, cs, "ndcli.firmware.status", `{}`)
		device := resp.Data.(map[string]interface{})["devices"].([]interface{})[0].(map[string]interface{})
		if device["series"] != "26.1" {
			t.Errorf("series = %v", device["series"])
		}
		if major := device["major"].(map[string]interface{}); major["eligible"] != false || major["blocked_by"] != "held_ha_member" {
			t.Errorf("major = %v", device["major"])
		}
		if be := device["boot_environment"].(map[string]interface{}); be["setting"] != false || be["last"] != "off" {
			t.Errorf("boot_environment = %v", device["boot_environment"])
		}
		if notes := device["notes"].([]interface{}); len(notes) != 2 || notes[1] != "third_party_package" {
			t.Errorf("notes = %v", device["notes"])
		}
	})
	t.Run("the board carries the major upgrades, their failures and the repository check", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpRolloutMajor)
		resp := callTool(t, cs, "ndcli.firmware.rollout", `{}`)
		data := resp.Data.(map[string]interface{})
		major := data["majors"].([]interface{})[0].(map[string]interface{})
		if major["state"] != "path_closed" || major["blocked_reason"] != "path_closed" || major["source_series"] != "26.1" || major["auto_apply_major"] != false {
			t.Errorf("majors[0] = %v", major)
		}
		if failure := data["major_failures"].([]interface{})[0].(map[string]interface{}); failure["series"] != "26.7" || failure["kind"] != "major" {
			t.Errorf("major_failures[0] = %v", failure)
		}
		if repo := data["agent_repo"].(map[string]interface{}); repo["configured"] != false {
			t.Errorf("agent_repo = %v", repo)
		}
	})
	t.Run("a board from an NDManager that predates them grows no key", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpRollout)
		resp := callTool(t, cs, "ndcli.firmware.rollout", `{}`)
		data := resp.Data.(map[string]interface{})
		for _, key := range []string{"majors", "major_failures", "agent_repo"} {
			if _, present := data[key]; present {
				t.Errorf("%s must be absent, not null", key)
			}
		}
	})
}

// The refusals the major upgrade added arrive in tool words, with the devices.
func TestFirmwareMajorRefusalsAreAdvisedInToolWords(t *testing.T) {
	t.Run("the upgrade path is closed", func(t *testing.T) {
		cs, _ := firmwareClient(t, 409, `{"error":"The upgrade to 26.7 is not open","code":"FIRMWARE_UPGRADE_NOT_OPEN"}`)
		resp := callTool(t, cs, "ndcli.firmware.version_promote", `{"version":"26.7","kind":"major","track":"fast","confirm":true}`)
		if resp.Success || resp.Error == nil || resp.Error.Code != "FIRMWARE_UPGRADE_NOT_OPEN" ||
			!strings.Contains(resp.Error.Message, "The upgrade to 26.7 is not open\n  Hint: ") ||
			!strings.Contains(resp.Error.Message, "OPNsense's upgrade path is closed") ||
			!strings.Contains(resp.Error.Message, "See where it stands with ndcli.firmware.rollout.") {
			t.Fatalf("response = %+v", resp)
		}
		if strings.Contains(resp.Error.Message, "'ndcli ") {
			t.Errorf("a tool caller has no command line: %q", resp.Error.Message)
		}
	})
	t.Run("devices in no track: the commands, and the names only in the server's words", func(t *testing.T) {
		// NDManager 1.97.0's own words: they name the devices and say what to do.
		const message = "The boot-environment setting belongs to a device's track row, which these devices do not have; add them to a track first, or send the track too: fw-a, fw-b"
		cs, _ := firmwareClient(t, 409, `{"error":"`+message+`","code":"FIRMWARE_DEVICE_UNMANAGED","blocking_resources":["fw-a","fw-b"],"count":2}`)
		resp := callTool(t, cs, "ndcli.firmware.device_set", `{"devices":["fw-a","fw-b"],"boot_environment":false,"confirm":true}`)
		if resp.Success || resp.Error == nil || resp.Error.Code != "FIRMWARE_DEVICE_UNMANAGED" {
			t.Fatalf("response = %+v", resp)
		}
		want := message + "\n  Hint: To add the devices to a track: ndcli.firmware.device_set with track=<track>. " +
			"To add them with the setting in one go: ndcli.firmware.device_set with track=<track>, boot_environment=<bool>."
		if resp.Error.Message != want {
			t.Errorf("message =\n%q\nwant\n%q", resp.Error.Message, want)
		}
		if n := strings.Count(resp.Error.Message, "fw-a"); n != 1 {
			t.Errorf("fw-a is named %d times, want once: %q", n, resp.Error.Message)
		}
	})
	t.Run("an NDManager that predates the setting", func(t *testing.T) {
		cs, _ := firmwareClient(t, http.StatusOK, mcpAssign)
		resp := callTool(t, cs, "ndcli.firmware.device_set", `{"devices":["fw-a"],"boot_environment":false,"confirm":true}`)
		if resp.Success || resp.Error == nil || resp.Error.Code != "SERVER_TOO_OLD" ||
			!strings.Contains(resp.Error.Message, "this NDManager predates the boot-environment setting") {
			t.Errorf("response = %+v", resp)
		}
	})
}

// Each description says what the new arguments and fields mean, in terms the
// control plane uses. The sentences are written out here, not read from the
// constants the descriptions are built from.
func TestFirmwareMajorToolDescriptions(t *testing.T) {
	tools := toolDefinitions(t)
	has := func(name string, wants ...string) {
		t.Helper()
		d := tools["ndcli.firmware."+name].Description
		for _, want := range wants {
			if !strings.Contains(d, want) {
				t.Errorf("ndcli.firmware.%s is missing %q:\n%s", name, want, d)
			}
		}
	}

	has("track_set",
		"A major upgrade moves a device to the next OPNsense series (for example 26.1 to 26.7): it takes the device offline for the length of the upgrade, reboots it three times and has no automatic rollback.",
		"Turning it on is the operator's explicit decision: the preview carries the consequences",
		"call again with confirm=true only on their go-ahead",
		"auto_apply_major=false cancels the major-upgrade steps queued for the track's devices and not yet dispatched (cancelled_tasks); an upgrade already running goes on.",
		"which is changed on its own, in a call of its own",
		"Needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise). Requires confirm=true.")
	has("track_show", "auto_apply_major", "off, the series is approved but no device is asked to upgrade")
	has("config_show", "auto_apply_major (whether the track's windows apply major upgrades; off by default; absent from an NDManager that predates them)")
	has("device_set",
		"boot_environment is whether the agent makes a ZFS boot environment before a major upgrade of the device",
		"a recovery path from the console only",
		"a device in no track has none (FIRMWARE_DEVICE_UNMANAGED names them; add them to a track first or give track too)",
		"it cannot be set with track=unmanaged",
		"changing it cancels the device's queued major-upgrade step that was not yet dispatched (cancelled_tasks)",
		"like moving into a track it is refused on a plan without firmware tracks",
		"Each result carries boot_environment (the setting afterwards)")
	has("status",
		"major {eligible, blocked_by}",
		"agent_below_major_floor (the agent is too old for major upgrades)",
		"awaiting_minor_evidence (waiting for the ordinary update window that records the final release of its series)",
		"step_in_flight (a firmware step is already in flight); held_ha_member; held_agent_abi_unavailable; held_insufficient_space; held_upgrade_aborted; held_unsupported_edition; held_custom_flavour; held_test_mode.",
		"made (made before the last major upgrade)",
		"skipped_no_room (skipped at the last major upgrade: the pool had no room)",
		"third_party_repository (a package repository from outside OPNsense and NetDefense is enabled on the device)",
		"third_party_package (a package from outside OPNsense and NetDefense is installed on the device)",
		"never named",
		"held_late_dispatch (the step reached the device with less time left than the update needs)",
		"held_agent_abi_unavailable (no usable NetDefense repository for the new FreeBSD version")
	has("history", "kind is minor (an ordinary update) or major (a major upgrade; target is then the series)")
	has("rollout",
		"majors, one entry per track (in conveyor order) and series with source_series",
		"path_closed (path closed: nothing approves it until OPNsense's upgrade files are seen on the mirror)",
		"blocked_reason",
		"final_minor_not_approved (the track has not approved the final release of the current series yet)",
		"previous_track_applies_no_majors (the previous track does not apply major upgrades, so no evidence can come from it)",
		"approval_source (auto or manual)",
		"source_series_end_of_life",
		"major_failures, the unresolved failures of major upgrades {kind, series, track, devices, count, last_at}",
		"agent_repo {configured, channel, trees: [{abi, ok, checked_at}]}",
		"these lists hold releases only")
	has("version_halts", "kind=major lists those of major upgrades, each with its series and no version, and kind=all both")
	for _, name := range []string{"version_halt", "version_unhalt", "version_promote", "failure_clear"} {
		has(name, "kind=major", "target series")
	}
	has("version_promote", "FIRMWARE_UPGRADE_NOT_OPEN", "approving it starts no upgrade by itself",
		"A major upgrade moves a device to the next OPNsense series (for example 26.1 to 26.7)")
	has("version_halt", "devices already upgrading are left alone")
	has("version_unhalt",
		"Lifting the halt on a major upgrade lets a track with auto_apply_major on that has approved the series upgrade every device that passes the gates at its next window.",
		"A major upgrade moves a device to the next OPNsense series (for example 26.1 to 26.7): it takes the device offline for the length of the upgrade, reboots it three times and has no automatic rollback.")
	has("failure_clear", "after a manual rollback of a device, clear the failures it caused once the device is rebound")

	// The reason is for a release or a major upgrade.
	haltProps, _ := schemaProps(t, tools["ndcli.firmware.version_halt"])
	if reason, _ := haltProps["reason"].(map[string]interface{}); !strings.Contains(reason["description"].(string), "Why the release or major upgrade is halted") {
		t.Errorf("the reason argument = %v", reason)
	}

	// The version argument says what it names for each kind.
	for _, name := range []string{"version_halt", "version_unhalt", "version_promote", "failure_clear"} {
		props, _ := schemaProps(t, tools["ndcli.firmware."+name])
		version, _ := props["version"].(map[string]interface{})
		if d, _ := version["description"].(string); !strings.Contains(d, "with kind=major the target series, such as 26.7") {
			t.Errorf("%s: the version argument does not say it names the series for kind=major: %q", name, d)
		}
		kind, _ := props["kind"].(map[string]interface{})
		if d, _ := kind["description"].(string); !strings.Contains(d, "a series is never a release: 26.7 is also the name of the GA release") {
			t.Errorf("%s: the kind argument does not warn that 26.7 is both: %q", name, d)
		}
	}

	// The plan rule keeps its sentences for the tools that gained arguments.
	has("device_set",
		"Moving devices to a track needs a plan that includes firmware tracks (FEATURE_NOT_AVAILABLE otherwise) and an enabled configuration;",
		"moving them to unmanaged (track=unmanaged) still works on a plan without firmware tracks while the organization has a firmware configuration, so the configuration can be wound down.")
}

func TestTaskToolsCarryTheMajorResultKindAndNotes(t *testing.T) {
	const result = `{"action":"advance","track":"fast","outcome":"success","reason":"applied","drift":false,"reinstall_entries":0,` +
		`"from_version":"26.1.11_10","to_version":"26.7.1_1","target_version":"26.7","pinned_version":"26.7.1_1","kind":"major",` +
		`"notes":["boot_environment_made","third_party_repository"]}`
	message, _ := json.Marshal(result)
	body := `{"task":"abcd1234","type":"FIRMWARE_TRACK","status":"COMPLETED","device_uuid":"u","device_name":"fw-a","organization":"acme","message":` + string(message) + `}`
	cs, _ := firmwareClient(t, http.StatusOK, body)
	resp := callTool(t, cs, "ndcli.task.describe", `{"task":"abcd1234"}`)
	got := resp.Data.(map[string]interface{})["task"].(map[string]interface{})["firmware_track_result"].(map[string]interface{})
	for k, want := range map[string]interface{}{"kind": "major", "target_version": "26.7", "outcome": "success"} {
		if got[k] != want {
			t.Errorf("firmware_track_result.%s = %v, want %v", k, got[k], want)
		}
	}
	notes, _ := got["notes"].([]interface{})
	if len(notes) != 2 || notes[0] != "boot_environment_made" || notes[1] != "third_party_repository" {
		t.Errorf("notes = %v", got["notes"])
	}
	d := toolDefinitions(t)["ndcli.task.describe"].Description
	if !strings.Contains(d, "kind") || !strings.Contains(d, "notes") {
		t.Errorf("ndcli.task.describe should explain kind and notes of firmware_track_result:\n%s", d)
	}
}

// The new fields went through the sanitizing decode too.
func TestFirmwareMajorToolOutputIsSanitized(t *testing.T) {
	hostile := `{"items":[{"device_name":"fw-a","device_uuid":"u","device_status":"ENABLED","track":"fast","state":"managed","series":"26\u001b[2J.1","major":{"eligible":false,"blocked_by":"held\u001b]0;pwn\u0007_ha"},"boot_environment":{"setting":true,"last":"ma\u009b2Jde"},"notes":["third_\u001b[31mparty"]}],"total":1,"page":1,"per_page":50,"pages":1}`
	cs, _ := firmwareClient(t, http.StatusOK, hostile)
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "ndcli.firmware.status", Arguments: map[string]interface{}{}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, bad := range []string{"\x1b", `\u001b`, `\u0007`, "\u009b", `\u009b`} {
		if strings.Contains(text, bad) {
			t.Errorf("the tool output carries %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "26[2J.1") || !strings.Contains(text, "third_[31mparty") {
		t.Errorf("the strings should survive, minus the escapes:\n%s", text)
	}
}

func TestFirmwareToolsTakeARevision(t *testing.T) {
	defs := toolDefinitions(t)
	for _, name := range []string{"version_halt", "version_unhalt", "version_promote", "failure_clear"} {
		props, _ := schemaProps(t, defs["ndcli.firmware."+name])
		rev, ok := props["revision"].(map[string]interface{})
		if !ok || rev["type"] != "integer" {
			t.Errorf("%s has no integer revision property: %v", name, props["revision"])
		}
	}
	for _, tc := range []struct {
		tool, args, response, method, path string
		query                              string
		body                               map[string]interface{}
	}{
		{"ndcli.firmware.version_halt", `{"version":"26.1.11","reason":"r","revision":2}`, mcpHalt, "POST",
			"/api/v1/organizations/acme/firmware/halts", "", map[string]interface{}{"version": "26.1.11", "reason": "r", "revision": float64(2)}},
		{"ndcli.firmware.version_unhalt", `{"version":"26.1.11","revision":2}`, mcpHalt, "DELETE",
			"/api/v1/organizations/acme/firmware/halts/26.1.11", "2", nil},
		{"ndcli.firmware.version_promote", `{"version":"26.1.11","track":"all","revision":2}`, mcpPromote, "POST",
			"/api/v1/organizations/acme/firmware/promote", "", map[string]interface{}{"version": "26.1.11", "track": "all", "revision": float64(2)}},
		{"ndcli.firmware.failure_clear", `{"version":"26.1.11","revision":2}`, mcpCleared, "POST",
			"/api/v1/organizations/acme/firmware/failures/clear", "", map[string]interface{}{"version": "26.1.11", "revision": float64(2)}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			cs, seen := firmwareClient(t, http.StatusOK, tc.response)
			preview := callTool(t, cs, tc.tool, tc.args)
			if data, _ := preview.Data.(map[string]interface{}); data["revision"] != float64(2) {
				t.Errorf("preview data lacks the revision: %v", preview.Data)
			}
			resp := callTool(t, cs, tc.tool, strings.TrimSuffix(tc.args, "}")+`,"confirm":true}`)
			if !resp.Success {
				t.Fatalf("confirmed call failed: %+v", resp.Error)
			}
			if seen.method != tc.method || seen.path != tc.path || seen.query.Get("revision") != tc.query || !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("sent %s %s ?%v %v", seen.method, seen.path, seen.query, seen.body)
			}
		})
	}
}

func TestFirmwareToolsRefuseARevisionBesideAMajorUpgrade(t *testing.T) {
	for _, tool := range []string{"version_halt", "version_unhalt", "version_promote", "failure_clear"} {
		cs, seen := firmwareClient(t, http.StatusOK, mcpHalt)
		args := `{"version":"26.7","kind":"major","reason":"r","track":"all","revision":2}`
		resp := callTool(t, cs, "ndcli.firmware."+tool, args)
		if resp.Success {
			t.Errorf("%s: a revision beside a major upgrade must be refused", tool)
		}
		resp = callTool(t, cs, "ndcli.firmware."+tool, strings.TrimSuffix(args, "}")+`,"confirm":true}`)
		if resp.Success || seen.hits != 0 {
			t.Errorf("%s: confirmed call succeeded or sent %d request(s)", tool, seen.hits)
		}
	}
}
