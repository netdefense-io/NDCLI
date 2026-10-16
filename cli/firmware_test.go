package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// firmwareCommandTools is the MCP-parity table: every `ndcli firmware` command
// and the tool that serves it. The MCP package pins the same tool names from
// its side (internal/mcp/tools_firmware_test.go); a command added here without
// a tool, or a tool without a command, fails one of the two lists.
var firmwareCommandTools = map[string]string{
	"firmware config show":     "ndcli.firmware.config_show",
	"firmware config create":   "ndcli.firmware.config_create",
	"firmware config set":      "ndcli.firmware.config_update",
	"firmware config enable":   "ndcli.firmware.config_enable",
	"firmware config disable":  "ndcli.firmware.config_disable",
	"firmware config delete":   "ndcli.firmware.config_delete",
	"firmware track show":      "ndcli.firmware.track_show",
	"firmware track set":       "ndcli.firmware.track_set",
	"firmware track schedule":  "ndcli.firmware.track_schedule",
	"firmware device set":      "ndcli.firmware.device_set",
	"firmware status":          "ndcli.firmware.status",
	"firmware history":         "ndcli.firmware.history",
	"firmware rollout":         "ndcli.firmware.rollout",
	"firmware version halt":    "ndcli.firmware.version_halt",
	"firmware version unhalt":  "ndcli.firmware.version_unhalt",
	"firmware version promote": "ndcli.firmware.version_promote",
	"firmware version halts":   "ndcli.firmware.version_halts",
	"firmware failure clear":   "ndcli.firmware.failure_clear",
}

func firmwareLeaves(cmd *cobra.Command, out *[]*cobra.Command) {
	if !cmd.HasSubCommands() {
		*out = append(*out, cmd)
		return
	}
	for _, sub := range cmd.Commands() {
		firmwareLeaves(sub, out)
	}
}

func TestFirmwareCommandTree(t *testing.T) {
	root := findCommand(t, "firmware")
	var leaves []*cobra.Command
	firmwareLeaves(root, &leaves)

	var got []string
	for _, leaf := range leaves {
		path := strings.TrimPrefix(leaf.CommandPath(), "ndcli ")
		got = append(got, path)
		tool, ok := firmwareCommandTools[path]
		if !ok {
			t.Errorf("%q has no entry in the MCP-parity table", path)
			continue
		}
		// ndcli.firmware.<path below firmware, joined with _>
		want := "ndcli.firmware." + strings.ReplaceAll(strings.TrimPrefix(path, "firmware "), " ", "_")
		if path == "firmware config set" {
			want = "ndcli.firmware.config_update" // the CLI sets, the API updates
		}
		if tool != want {
			t.Errorf("%q maps to %q, want %q", path, tool, want)
		}
	}
	sort.Strings(got)
	var want []string
	for path := range firmwareCommandTools {
		want = append(want, path)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("firmware commands = %v\nparity table      = %v", got, want)
	}
}

// The commands ship visible for every plan. A plan without the feature is told
// so by the server, and the refusal is printed as it comes.
func TestFirmwareCommandsAreNeverHiddenAndAnswerNotAvailableWithoutThePlan(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			t.Errorf("%s is hidden; firmware commands are shown to every plan", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(findCommand(t, "firmware"))

	body := `{"error":"Firmware tracks are not available on your current plan.","code":"FEATURE_NOT_AVAILABLE"}`
	for _, tc := range []struct {
		name string
		path []string
		args []string
	}{
		{"config create", []string{"config", "create"}, nil},
		{"track set", []string{"track", "set"}, []string{"fast", "--soak-days", "2"}},
		{"device set", []string{"device", "set"}, []string{"fw-a", "--track", "fast"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, _, err := runFirmware(t, http.StatusForbidden, body, "table", tc.path, tc.args...)
			if err == nil {
				t.Fatal("expected the refusal")
			}
			useFormatter(t, output.NewTableFormatter(), "")
			if got, want := printed(err), "Error: [FEATURE_NOT_AVAILABLE] Firmware tracks are not available on your current plan.\n"; got != want {
				t.Errorf("printed %q, want %q", got, want)
			}
			if stdout != "" {
				t.Errorf("a refusal leaves stdout empty, got %q", stdout)
			}
		})
	}
}

// firmwareInvocations is one invocation per command that reaches its route with
// no prompt in the way, keyed by the command path of firmwareCommandTools.
var firmwareInvocations = map[string][]string{
	"firmware config show":     nil,
	"firmware config create":   nil,
	"firmware config set":      {"--reboot"},
	"firmware config enable":   nil,
	"firmware config disable":  {"--force"},
	"firmware config delete":   {"--force"},
	"firmware track show":      nil,
	"firmware track set":       {"fast", "--soak-days", "2"},
	"firmware track schedule":  {"fast", "night"},
	"firmware device set":      {"fw-a", "--track", "fast"},
	"firmware status":          nil,
	"firmware history":         {"fw-a"},
	"firmware rollout":         nil,
	"firmware version halt":    {"26.7.5", "--reason", "regression"},
	"firmware version unhalt":  {"26.7.5"},
	"firmware version promote": {"26.7.5", "--track", "all", "--force"},
	"firmware version halts":   nil,
	"firmware failure clear":   {"26.7.5"},
}

// A plan without the feature can refuse any of the routes (every one when the
// organization has no configuration, reads included), and the refusal is the
// server's: its code and its message, printed as sent, with nothing added.
func TestFirmwareAnswersNotAvailableAsSent(t *testing.T) {
	const message = "Firmware tracks are not available on your current plan."
	body := `{"error":"` + message + `","code":"FEATURE_NOT_AVAILABLE"}`

	if len(firmwareInvocations) != len(firmwareCommandTools) {
		t.Fatalf("%d invocations for %d commands", len(firmwareInvocations), len(firmwareCommandTools))
	}
	for path, args := range firmwareInvocations {
		if _, ok := firmwareCommandTools[path]; !ok {
			t.Fatalf("%q is not a firmware command", path)
		}
		t.Run(path, func(t *testing.T) {
			words := strings.Fields(strings.TrimPrefix(path, "firmware "))
			stdout, _, seen, err := runFirmware(t, http.StatusForbidden, body, "table", words, args...)
			if err == nil {
				t.Fatal("expected the refusal")
			}
			if seen.hits != 1 {
				t.Errorf("%d request(s) sent, want the one the server refused", seen.hits)
			}
			useFormatter(t, output.NewTableFormatter(), "")
			if got, want := printed(err), "Error: [FEATURE_NOT_AVAILABLE] "+message+"\n"; got != want {
				t.Errorf("printed %q, want %q", got, want)
			}
			useFormatter(t, output.NewJSONFormatter(), "json")
			doc := decodeCommandError(t, printed(err))
			if doc["code"] != "FEATURE_NOT_AVAILABLE" || doc["message"] != message {
				t.Errorf("json error = %v", doc)
			}
			if stdout != "" {
				t.Errorf("a refusal leaves stdout empty, got %q", stdout)
			}
		})
	}
}

// `ndcli firmware --help` says what a plan without the feature does, as the
// control plane does it, and where to check the plan; the commands whose answer
// differs say so themselves.
func TestFirmwareHelpStatesThePlanRule(t *testing.T) {
	flat := func(text string) string { return strings.Join(strings.Fields(text), " ") }

	group := flat(findCommand(t, "firmware").Long)
	for _, want := range []string{
		"A track moves and updates its devices only while the organization's plan includes firmware tracks and the configuration exists and is enabled ('ndcli firmware config create').",
		"Check the plan with 'ndcli org quota <org>' (Firmware Tracks).",
		"A plan without firmware tracks refuses every firmware command as not available on the current plan (FEATURE_NOT_AVAILABLE), reads included.",
		"The exception is an organization that already has a configuration, one that lost the feature after setting it up:",
		"no new steps are sent; devices still in a track are being released.",
		"Its reads keep answering, so that wind-down stays visible, and four changes still work:",
		"detaching a track from its schedule ('ndcli firmware track schedule <track> --no-schedule'),",
		"moving devices to unmanaged ('ndcli firmware device set <device> --track unmanaged'),",
		"disabling the configuration and deleting it.",
		"Every other change is refused.",
	} {
		if !strings.Contains(group, want) {
			t.Errorf("ndcli firmware --help is missing %q:\n%s", want, group)
		}
	}

	for _, tc := range []struct {
		path []string
		want string
	}{
		{[]string{"config", "create"}, "Requires a plan that includes firmware tracks."},
		{[]string{"config", "disable"}, "Still works on a plan without firmware tracks while the organization has a firmware configuration, so the configuration can be wound down."},
		{[]string{"config", "delete"}, "Still works on a plan without firmware tracks while the organization has a firmware configuration, so the configuration can be wound down. Both ways of releasing its devices, disabling firmware and moving devices to unmanaged, work there too."},
		{[]string{"track", "schedule"}, "Attaching a schedule needs a plan that includes firmware tracks. Detaching with --no-schedule still works on a plan without them while the organization has a firmware configuration, so the configuration can be wound down."},
		{[]string{"device", "set"}, "Moving devices to a track needs a plan that includes firmware tracks and an enabled configuration. Moving them to unmanaged still works on a plan without firmware tracks while the organization has a firmware configuration, so the configuration can be wound down."},
	} {
		cmd := findCommand(t, append([]string{"firmware"}, tc.path...)...)
		if long := flat(cmd.Long); !strings.Contains(long, tc.want) {
			t.Errorf("%s --help is missing %q:\n%s", cmd.CommandPath(), tc.want, long)
		}
	}

	// The control plane is acting on a plan without the feature, releasing the
	// devices, so no help text may say that nothing happens.
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if strings.Contains(strings.ToLower(flat(c.Long)), "nothing acts") {
			t.Errorf("%s --help says nothing acts, but a plan without the feature releases the devices:\n%s", c.CommandPath(), flat(c.Long))
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(findCommand(t, "firmware"))
}

// A track is chosen device by device: nothing under firmware takes an OU, a
// pattern or a filter that names several devices at once.
func TestFirmwareHasNoOUInput(t *testing.T) {
	root := findCommand(t, "firmware")
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			for _, banned := range []string{"ou", "ous", "org-unit", "pattern", "regex", "all-devices"} {
				if f.Name == banned {
					t.Errorf("%s has a --%s flag; firmware tracks take explicit device names only", c.CommandPath(), f.Name)
				}
			}
		})
		if strings.Contains(strings.ToLower(c.Use), "<ou") {
			t.Errorf("%s takes an OU argument", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// firmwareCommand returns a registered command with its local flags at their
// defaults before the test and after it: commands are package-level, so a
// value one test sets would leak into the next.
func firmwareCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd := findCommand(t, append([]string{"firmware"}, path...)...)
	reset := func() {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if s, ok := f.Value.(pflag.SliceValue); ok {
				_ = s.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset()
	t.Cleanup(reset)
	return cmd
}

// firmwareSeen is what the fake control plane saw.
type firmwareSeen struct {
	method string
	path   string
	query  url.Values
	body   map[string]interface{}
	hits   int
}

// firmwareReply answers every request with status and body, recording it.
func firmwareReply(t *testing.T, seen *firmwareSeen, status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seen.hits++
		seen.method, seen.path, seen.query, seen.body = r.Method, r.URL.EscapedPath(), r.URL.Query(), nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, &seen.body); err != nil {
				t.Errorf("request body is not JSON: %v\n%s", err, raw)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// runFirmware runs a firmware command against a fake control plane with the
// real formatter for format, and returns its output, what the server saw and
// the command's error. The command is run the way cobra does after parsing
// flags, but without the root's config loading.
func runFirmware(t *testing.T, status int, body, format string, path []string, args ...string) (stdout, stderr string, seen *firmwareSeen, err error) {
	t.Helper()
	cmd := firmwareCommand(t, path...)
	seen = &firmwareSeen{}
	stdout, stderr = runStreams(t, firmwareReply(t, seen, status, body), format, "", func() error {
		if perr := cmd.ParseFlags(args); perr != nil {
			err = perr
			return nil
		}
		err = cmd.RunE(cmd, cmd.Flags().Args())
		return nil
	})
	return stdout, stderr, seen, err
}

const (
	fwConfig = `{"organization_name":"acme","status":"ENABLED","reboot":true,"plan_enabled":true,"tracks":[` +
		`{"track":"fast","soak_days":1,"min_success":0,"schedule":"night","scheduled":true,"next_run_at":"2099-01-02T03:04:05Z","devices":{"total":2,"managed":2}},` +
		`{"track":"regular","soak_days":9,"min_success":1,"scheduled":false,"devices":{"total":0}},` +
		`{"track":"stable","soak_days":10,"min_success":1,"scheduled":false,"devices":{"total":0}}],` +
		`"created_at":"2026-10-08T12:00:00Z","message":"Firmware configuration created"}`
	fwTrack   = `{"track":"regular","soak_days":7,"min_success":2,"schedule":"night","scheduled":true,"devices":{"total":1,"managed":1}}`
	fwAssign  = `{"organization_name":"acme","track":"fast","results":[{"device_name":"fw-a","result":"added","track":"fast","state":"enrolling","notes":[]},{"device_name":"fw-b","result":"unchanged","track":"fast","state":"managed","notes":["device_disabled"]}],"cancelled_tasks":0}`
	fwDevices = `{"items":[{"device_name":"fw-a","device_uuid":"u1","device_status":"ENABLED","agent_version":"1.24.0","track":"fast","state":"managed","pinned_version":"26.7.3","last_status":"success","last_run_at":"2026-10-08T12:00:00Z"}],"total":1,"page":1,"per_page":50,"pages":1}`
	fwHistory = `{"device_name":"fw-a","items":[{"completed_at":"2026-10-08T12:00:00Z","track":"fast","action":"advance","outcome":"success","reason":"applied","target":"26.7.5","from_version":"26.7.3","to_version":"26.7.5","applied":true}],"total":1,"page":1,"per_page":50,"pages":1}`
	fwRollout = `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[{"track":"fast","soak_days":1,"min_success":0,"scheduled":false,"devices":{"total":1},"series":[{"series":"26.7","target":"26.7.4","next_candidate":{"version":"26.7.5","state":"soaking","soak_remaining_seconds":3600,"stalled":false}}]}],"failures":[],"halts":[]}`
	fwHalt    = `{"halt":{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"},"cancelled_tasks":2}`
	fwUnhalt  = `{"halt":{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com","unhalted_at":"2026-10-09T12:00:00Z","unhalted_by":"ops@acme.com"},"cancelled_tasks":0}`
	fwHalts   = `{"items":[` + `{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"}],"total":1,"page":1,"per_page":50,"pages":1}`
	fwPromote = `{"version":"26.7.5","results":[{"track":"fast","result":"approved","target":"26.7.5"},{"track":"regular","result":"approved","target":"26.7.5"},{"track":"stable","result":"already_approved","target":"26.7.5"}]}`
	fwCleared = `{"version":"26.7.5","cleared":2}`
	fwDeleted = `{"message":"Firmware configuration deleted"}`
)

// Every command, in the human formats and in JSON, against the route it is
// documented to call. In JSON the output is exactly one document: a command
// that only changes state still prints through its formatter.
func TestFirmwareCommands(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     []string
		args     []string
		response string
		method   string
		route    string
		query    map[string]string
		body     map[string]interface{}
		human    []string // fragments of the table output
		jsonKey  string   // a key the JSON document must carry
	}{
		{"config show", []string{"config", "show"}, nil, fwConfig, "GET", "/api/v1/organizations/acme/firmware/config", nil, nil,
			[]string{"acme", "Enabled", "night", "fast", "regular", "stable"}, "tracks"},
		{"config create", []string{"config", "create"}, nil, fwConfig, "POST", "/api/v1/organizations/acme/firmware/config", nil,
			map[string]interface{}{"reboot": true}, []string{"Firmware configuration created"}, "organization_name"},
		{"config create --no-reboot", []string{"config", "create"}, []string{"--no-reboot"}, fwConfig, "POST", "/api/v1/organizations/acme/firmware/config", nil,
			map[string]interface{}{"reboot": false}, []string{"acme"}, "reboot"},
		{"config set --reboot", []string{"config", "set"}, []string{"--reboot"}, fwConfig, "PUT", "/api/v1/organizations/acme/firmware/config", nil,
			map[string]interface{}{"reboot": true}, []string{"acme"}, "reboot"},
		{"config set --no-reboot", []string{"config", "set"}, []string{"--no-reboot"}, fwConfig, "PUT", "/api/v1/organizations/acme/firmware/config", nil,
			map[string]interface{}{"reboot": false}, []string{"acme"}, "reboot"},
		{"config enable", []string{"config", "enable"}, nil, fwConfig, "PUT", "/api/v1/organizations/acme/firmware/config/status", nil,
			map[string]interface{}{"status": "ENABLED"}, []string{"acme"}, "status"},
		{"config disable", []string{"config", "disable"}, []string{"--force"}, fwConfig, "PUT", "/api/v1/organizations/acme/firmware/config/status", nil,
			map[string]interface{}{"status": "DISABLED"}, []string{"acme"}, "status"},
		{"config delete", []string{"config", "delete"}, []string{"--force"}, fwDeleted, "DELETE", "/api/v1/organizations/acme/firmware/config", nil, nil,
			[]string{"Firmware configuration deleted", "acme"}, "message"},
		{"track show (all)", []string{"track", "show"}, nil, fwConfig, "GET", "/api/v1/organizations/acme/firmware/config", nil, nil,
			[]string{"fast", "regular", "stable"}, "tracks"},
		{"track show one", []string{"track", "show"}, []string{"Regular"}, fwTrack, "GET", "/api/v1/organizations/acme/firmware/tracks/regular", nil, nil,
			[]string{"regular", "7 days"}, "soak_days"},
		{"track set", []string{"track", "set"}, []string{"regular", "--soak-days", "7", "--min-success", "2"}, fwTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/regular", nil, map[string]interface{}{"soak_days": float64(7), "min_success": float64(2)},
			[]string{"regular", "7 days"}, "min_success"},
		{"track set (zero is a value)", []string{"track", "set"}, []string{"fast", "--min-success", "0"}, fwTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/fast", nil, map[string]interface{}{"min_success": float64(0)}, []string{"regular"}, "track"},
		{"track schedule", []string{"track", "schedule"}, []string{"fast", "night"}, fwTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/fast/schedule", nil, map[string]interface{}{"schedule": "night"}, []string{"night"}, "schedule"},
		{"track schedule --no-schedule", []string{"track", "schedule"}, []string{"fast", "--no-schedule"}, fwTrack, "PUT",
			"/api/v1/organizations/acme/firmware/tracks/fast/schedule", nil, map[string]interface{}{"schedule": nil}, []string{"regular"}, "track"},
		{"device set", []string{"device", "set"}, []string{"fw-a", "fw-b", "--track", "fast"}, fwAssign, "PUT",
			"/api/v1/organizations/acme/firmware/devices", nil,
			map[string]interface{}{"devices": []interface{}{"fw-a", "fw-b"}, "track": "fast"},
			[]string{"2 devices → fast track", "fw-a", "added", "enrolling", "device disabled"}, "results"},
		{"status", []string{"status"}, []string{"--track", "fast", "--state", "managed", "--name", "fw", "--per-page", "20"}, fwDevices, "GET",
			"/api/v1/organizations/acme/firmware/devices", map[string]string{"track": "fast", "state": "managed", "name": "fw", "per_page": "20", "page": "1"}, nil,
			[]string{"fw-a", "26.7.3", "success"}, "items"},
		{"history", []string{"history"}, []string{"fw-a"}, fwHistory, "GET",
			"/api/v1/organizations/acme/firmware/devices/fw-a/history", map[string]string{"page": "1", "per_page": "50"}, nil,
			[]string{"26.7.3 → 26.7.5", "the update was applied"}, "items"},
		{"rollout", []string{"rollout"}, nil, fwRollout, "GET", "/api/v1/organizations/acme/firmware/status", nil, nil,
			[]string{"26.7.4", "26.7.5", "soaking", "1h"}, "tracks"},
		{"version halt", []string{"version", "halt"}, []string{"26.7.5", "--reason", "regression"}, fwHalt, "POST",
			"/api/v1/organizations/acme/firmware/halts", nil, map[string]interface{}{"version": "26.7.5", "reason": "regression"},
			[]string{"Release 26.7.5 halted", "2 pending steps cancelled"}, "halt"},
		{"version unhalt", []string{"version", "unhalt"}, []string{"26.7.5"}, fwUnhalt, "DELETE",
			"/api/v1/organizations/acme/firmware/halts/26.7.5", nil, nil, []string{"Halt on release 26.7.5 lifted"}, "halt"},
		{"version halts", []string{"version", "halts"}, []string{"--all"}, fwHalts, "GET",
			"/api/v1/organizations/acme/firmware/halts", map[string]string{"active": "false"}, nil, []string{"26.7.5", "regression"}, "items"},
		{"version promote", []string{"version", "promote"}, []string{"26.7.5", "--track", "all", "--force"}, fwPromote, "POST",
			"/api/v1/organizations/acme/firmware/promote", nil, map[string]interface{}{"version": "26.7.5", "track": "all"},
			[]string{"Release 26.7.5 approved in fast, regular", "already approved"}, "results"},
		{"failure clear", []string{"failure", "clear"}, []string{"26.7.5", "--track", "fast"}, fwCleared, "POST",
			"/api/v1/organizations/acme/firmware/failures/clear", nil, map[string]interface{}{"version": "26.7.5", "track": "fast"},
			[]string{"Cleared 2 failures blocking release 26.7.5 in the fast track"}, "cleared"},
		{"failure clear (every track)", []string{"failure", "clear"}, []string{"26.7.5"}, fwCleared, "POST",
			"/api/v1/organizations/acme/firmware/failures/clear", nil, map[string]interface{}{"version": "26.7.5"},
			[]string{"in every track"}, "cleared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, seen, err := runFirmware(t, http.StatusOK, tc.response, "table", tc.path, tc.args...)
			if err != nil {
				t.Fatalf("command failed: %v", err)
			}
			if seen.method != tc.method || seen.path != tc.route {
				t.Errorf("sent %s %s, want %s %s", seen.method, seen.path, tc.method, tc.route)
			}
			for k, want := range tc.query {
				if got := seen.query.Get(k); got != want {
					t.Errorf("query %s = %q, want %q", k, got, want)
				}
			}
			if !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("body = %v, want %v", seen.body, tc.body)
			}
			for _, want := range tc.human {
				if !strings.Contains(stdout, want) {
					t.Errorf("table output is missing %q:\n%s", want, stdout)
				}
			}

			// Same command, JSON: one document, nothing else.
			jsonOut, jsonErr, _, err := runFirmware(t, http.StatusOK, tc.response, "json", tc.path, tc.args...)
			if err != nil {
				t.Fatalf("json: command failed: %v", err)
			}
			var doc map[string]interface{}
			decodeOne(t, jsonOut, &doc)
			if _, ok := doc[tc.jsonKey]; !ok {
				t.Errorf("json output has no %q: %s", tc.jsonKey, jsonOut)
			}
			if jsonErr != "" {
				t.Errorf("json: stderr should be empty, got %q", jsonErr)
			}
		})
	}
}

// The confirmations: destructive commands ask, and a script that cannot answer
// must say --force. Nothing is sent before the answer.
func TestFirmwareConfirmations(t *testing.T) {
	for _, tc := range []struct {
		name string
		path []string
		args []string
	}{
		{"config disable", []string{"config", "disable"}, nil},
		{"config delete", []string{"config", "delete"}, nil},
		{"version promote", []string{"version", "promote"}, []string{"26.7.5", "--track", "fast"}},
		{"version promote of a major upgrade", []string{"version", "promote"}, []string{"26.7", "--major", "--track", "fast"}},
		{"version unhalt of a major upgrade", []string{"version", "unhalt"}, []string{"26.7", "--major"}},
		{"track set turning major upgrades on", []string{"track", "set"}, []string{"fast", "--auto-apply-major=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, seen, err := runFirmware(t, http.StatusOK, `{}`, "table", tc.path, tc.args...)
			if err == nil || !strings.Contains(err.Error(), "--force") {
				t.Fatalf("err = %v, want a request for --force when stdin is not a terminal", err)
			}
			if seen.hits != 0 || stdout != "" {
				t.Errorf("nothing may be sent or printed before the answer: %d hit(s), stdout %q", seen.hits, stdout)
			}
		})
	}
}

func TestFirmwareRefusesBadInputBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		path []string
		args []string
		want string
	}{
		{"config set with neither flag", []string{"config", "set"}, nil, "pass --reboot or --no-reboot"},
		{"track schedule with both", []string{"track", "schedule"}, []string{"fast", "night", "--no-schedule"}, "not both"},
		{"track schedule with neither", []string{"track", "schedule"}, []string{"fast"}, "give the schedule name"},
		{"track show with a bad track", []string{"track", "show"}, []string{"slow"}, `unknown track "slow"`},
		{"device set with neither a track nor a setting", []string{"device", "set"}, []string{"fw-a"}, "give a track, a boot_environment setting, or both"},
		{"device set to unmanaged with a setting", []string{"device", "set"}, []string{"fw-a", "--track", "unmanaged", "--boot-environment=false"},
			"boot_environment cannot be set while moving devices to unmanaged"},
		{"track set with the switch and soak days, past cobra's flag groups", []string{"track", "set"}, []string{"fast", "--auto-apply-major=true", "--soak-days", "2", "--force"},
			"auto_apply_major is changed on its own"},
		{"track set with the switch and min success, past cobra's flag groups", []string{"track", "set"}, []string{"regular", "--auto-apply-major=false", "--min-success", "1"},
			"auto_apply_major is changed on its own"},
		{"track set of a track that is none, before any prompt", []string{"track", "set"}, []string{"slow", "--auto-apply-major=true"}, `unknown track "slow"`},
		{"halt of a major upgrade without a reason", []string{"version", "halt"}, []string{"26.7", "--major"}, "reason is required"},
		{"unhalt of a major upgrade without a series, before the prompt", []string{"version", "unhalt"}, []string{" ", "--major"}, "series is required"},
		{"halts of a kind that is none", []string{"version", "halts"}, []string{"--kind", "everything"}, `unknown kind "everything"`},
		{"device set to all", []string{"device", "set"}, []string{"fw-a", "--track", "all"}, `unknown track "all"`},
		{"status with a bad state", []string{"status"}, []string{"--state", "asleep"}, `unknown state "asleep"`},
		{"promote to unmanaged is refused before the prompt", []string{"version", "promote"}, []string{"26.7.5", "--track", "unmanaged"}, `unknown track "unmanaged"`},
		{"promote of a blank release is refused before the prompt", []string{"version", "promote"}, []string{" ", "--track", "fast"}, "version is required"},
		{"promote of a blank series is refused before the prompt", []string{"version", "promote"}, []string{" ", "--major", "--track", "fast"}, "series is required"},
		{"halt without a reason", []string{"version", "halt"}, []string{"26.7.5"}, "reason is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, seen, err := runFirmware(t, http.StatusOK, `{}`, "table", tc.path, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
			if seen.hits != 0 {
				t.Errorf("%d request(s) sent for input that was refused", seen.hits)
			}
		})
	}
}

// Required flags are enforced by cobra before RunE, so these are checked on the
// command's definition: typing the command without them is a usage error.
func TestFirmwareRequiredFlags(t *testing.T) {
	for _, tc := range []struct {
		path []string
		flag string
	}{
		{[]string{"version", "halt"}, "reason"},
		{[]string{"version", "promote"}, "track"},
	} {
		cmd := firmwareCommand(t, tc.path...)
		f := cmd.Flags().Lookup(tc.flag)
		if f == nil || f.Annotations[cobra.BashCompOneRequiredFlag] == nil {
			t.Errorf("%s --%s must be a required flag", cmd.CommandPath(), tc.flag)
		}
	}
}

func TestFirmwareConfigDeleteListsTheDevicesThatBlockIt(t *testing.T) {
	body := `{"error":"Devices are still in a firmware track or leaving one; move them to unmanaged (or disable Firmware) and delete the configuration once they are released.",` +
		`"code":"HAS_DEPENDENCIES","blocking_resources":["fw-a","fw-b"],"count":7}`
	stdout, _, _, err := runFirmware(t, http.StatusConflict, body, "table", []string{"config", "delete"}, "--force")
	if stdout != "" {
		t.Errorf("a refusal leaves stdout empty, got %q", stdout)
	}
	want := "Devices are still in a firmware track or leaving one; move them to unmanaged (or disable Firmware) and delete the configuration once they are released. " +
		"(7 devices in all, 2 listed)\nDevices still in a track:\n  • fw-a\n  • fw-b"
	assertRefusal(t, err, stdout, "HAS_DEPENDENCIES", want)
}

// A refusal arrives with the way forward, and the code still shows.
func TestFirmwareRefusalsAreAdvisedInCLIWords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
		path   []string
		args   []string
		hint   string
	}{
		{"not configured", 404, "FIRMWARE_NOT_CONFIGURED", []string{"status"}, nil, "'ndcli firmware config create'"},
		{"disabled", 409, "FIRMWARE_DISABLED", []string{"device", "set"}, []string{"fw-a", "--track", "fast"}, "'ndcli firmware config enable'"},
		{"leaving", 409, "FIRMWARE_DEVICE_LEAVING", []string{"device", "set"}, []string{"fw-a", "--track", "fast"}, "'ndcli firmware status'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"error":"the server says no","code":"` + tc.code + `"}`
			_, _, _, err := runFirmware(t, tc.status, body, "table", tc.path, tc.args...)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			useFormatter(t, output.NewTableFormatter(), "")
			text := printed(err)
			if !strings.HasPrefix(text, "Error: ["+tc.code+"] the server says no\n  Hint: ") || !strings.Contains(text, tc.hint) {
				t.Errorf("printed %q, want the code, the server's words and a hint naming %s", text, tc.hint)
			}
			useFormatter(t, output.NewJSONFormatter(), "json")
			doc := decodeCommandError(t, printed(err))
			if doc["code"] != tc.code || !strings.Contains(doc["message"], tc.hint) {
				t.Errorf("json error = %v", doc)
			}
		})
	}
}

// The advice for a track's schedule fills the track's name into the vocabulary's
// commands, so each carries the placeholder once, and each is a command that
// exists with the flag it names.
func TestFirmwareCLIVocabularyNamesRealTrackCommands(t *testing.T) {
	for name, command := range map[string]string{
		"TrackDetach": firmwareCLIVocabulary.TrackDetach, "TrackSchedule": firmwareCLIVocabulary.TrackSchedule,
	} {
		if n := strings.Count(command, service.FirmwareTrackPlaceholder); n != 1 {
			t.Errorf("%s = %q carries the track placeholder %d times, want once", name, command, n)
		}
	}
	schedule := findCommand(t, "firmware", "track", "schedule")
	if !strings.Contains(firmwareCLIVocabulary.TrackDetach, "ndcli firmware track schedule ") ||
		!strings.Contains(firmwareCLIVocabulary.TrackDetach, " --no-schedule") || schedule.Flags().Lookup("no-schedule") == nil {
		t.Errorf("TrackDetach = %q does not match `firmware track schedule <track> --no-schedule`", firmwareCLIVocabulary.TrackDetach)
	}
	if got := firmwareCLIVocabulary.DetachCommand("stable"); got != "'ndcli firmware track schedule stable --no-schedule'" {
		t.Errorf("DetachCommand(stable) = %q", got)
	}
	if got := firmwareCLIVocabulary.ScheduleCommand("stable"); got != "'ndcli firmware track schedule stable <schedule>'" {
		t.Errorf("ScheduleCommand(stable) = %q", got)
	}
}

// The commands the vocabulary names for the major upgrade exist, with the flags
// it names.
func TestFirmwareCLIVocabularyNamesTheMajorUpgradeCommands(t *testing.T) {
	findCommand(t, "firmware", "rollout")
	if firmwareCLIVocabulary.Rollout != "'ndcli firmware rollout'" {
		t.Errorf("Rollout = %q", firmwareCLIVocabulary.Rollout)
	}
	set := findCommand(t, "firmware", "device", "set")
	for name, command := range map[string]string{"DeviceAdd": firmwareCLIVocabulary.DeviceAdd, "DeviceAddWithSetting": firmwareCLIVocabulary.DeviceAddWithSetting} {
		if !strings.Contains(command, "ndcli firmware device set <device> --track <track>") || set.Flags().Lookup("track") == nil {
			t.Errorf("%s = %q does not match `firmware device set <device> --track <track>`", name, command)
		}
	}
	if !strings.Contains(firmwareCLIVocabulary.DeviceAddWithSetting, "--boot-environment=<bool>") || set.Flags().Lookup("boot-environment") == nil {
		t.Errorf("DeviceAddWithSetting = %q names a flag that does not exist", firmwareCLIVocabulary.DeviceAddWithSetting)
	}
}

// `run firmware-upgrade` on a device in a track: the server refuses and the
// refusal points at ndcli firmware, on the live path and the --schedule one.
func TestRunFirmwareUpgradePointsAtFirmwareForAManagedDevice(t *testing.T) {
	body := `{"error":"Device 'fw1' is in a firmware track, which moves it between releases; move it out of its track to run a firmware upgrade on it.","code":"FIRMWARE_DEVICE_MANAGED"}`
	for _, args := range [][]string{
		{"--device", "fw1", "--mode", "minor"},
		{"--device", "fw1", "--mode", "minor", "--schedule", "nightly"},
	} {
		cmd := firmwareUpgradeCommand(t)
		srv := &firmwareSeen{}
		var err error
		_, _ = runStreams(t, firmwareReply(t, srv, http.StatusConflict, body), "table", "", func() error {
			if perr := cmd.ParseFlags(args); perr != nil {
				t.Fatalf("parse %v: %v", args, perr)
			}
			err = cmd.RunE(cmd, cmd.Flags().Args())
			return nil
		})
		if err == nil {
			t.Fatalf("%v: expected the refusal", args)
		}
		useFormatter(t, output.NewTableFormatter(), "")
		text := printed(err)
		for _, want := range []string{"[FIRMWARE_DEVICE_MANAGED]", "Device 'fw1' is in a firmware track", "Hint:",
			"'ndcli firmware status'", "'ndcli firmware device set <device> --track unmanaged'"} {
			if !strings.Contains(text, want) {
				t.Errorf("%v: printed %q, missing %q", args, text, want)
			}
		}
		if code := api.GetErrorCode(err); code != "FIRMWARE_DEVICE_MANAGED" {
			t.Errorf("%v: code = %q", args, code)
		}
	}
}

// The firmware windows are a track's, not the schedule commands'.
func TestScheduleTasksPointFirmwareSpecsAtTheirTrack(t *testing.T) {
	t.Run("remove is refused locally, before any prompt or delete, naming the track the spec is the window of", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			descriptor string
			detach     string
			move       string
		}{
			// The descriptor names its track, so the commands do too.
			{"a spec that names its track",
				`{"code":"Fw123456","kind":"FIRMWARE","schedule_name":"night","firmware_track":"regular","request":{},"enabled":true}`,
				"'ndcli firmware track schedule regular --no-schedule'", "'ndcli firmware track schedule regular <schedule>'"},
			// A control plane that does not send the track leaves the template.
			{"a spec that names none",
				`{"code":"Fw123456","kind":"FIRMWARE","schedule_name":"night","request":{},"enabled":true}`,
				"'ndcli firmware track schedule <track> --no-schedule'", "'ndcli firmware track schedule <track> <schedule>'"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cmd := findCommand(t, "schedule", "tasks", "remove")
				var err error
				var methods []string
				_, _ = runStreams(t, func(w http.ResponseWriter, r *http.Request) {
					methods = append(methods, r.Method)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, tc.descriptor)
				}, "table", "", func() error {
					err = runScheduleTasksRemove(cmd, []string{"Fw123456"})
					return nil
				})
				if err == nil || !strings.Contains(err.Error(), "detach the track from its schedule with "+tc.detach) ||
					!strings.Contains(err.Error(), "move it to another schedule with "+tc.move) {
					t.Fatalf("err = %v, want it to give %s and %s", err, tc.detach, tc.move)
				}
				if len(methods) != 1 || methods[0] != http.MethodGet {
					t.Errorf("requests = %v; only the lookup may be sent", methods)
				}
			})
		}
	})
	t.Run("enable and disable carry the server's refusal and the way forward", func(t *testing.T) {
		// NDManager's body on a track's schedule entry: it names no track.
		const body = `{"error":"This schedule entry belongs to a firmware track. Attach or detach the track's schedule through the firmware configuration instead.","code":"FIRMWARE_SPEC_MANAGED"}`
		for name, run := range map[string]func(*cobra.Command, []string) error{
			"enable": runScheduleTasksEnable, "disable": runScheduleTasksDisable,
		} {
			cmd := findCommand(t, "schedule", "tasks", name)
			var err error
			seen := &firmwareSeen{}
			_, _ = runStreams(t, firmwareReply(t, seen, http.StatusConflict, body), "table", "", func() error {
				err = run(cmd, []string{"Fw123456"})
				return nil
			})
			useFormatter(t, output.NewTableFormatter(), "")
			text := printed(err)
			for _, want := range []string{
				"Error: [FIRMWARE_SPEC_MANAGED] This schedule entry belongs to a firmware track. Attach or detach the track's schedule through the firmware configuration instead.\n  Hint: ",
				"To detach the track from its schedule: 'ndcli firmware track schedule <track> --no-schedule'.",
				"To move it to another schedule: 'ndcli firmware track schedule <track> <schedule>'.",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("%s: printed %q, missing %q", name, text, want)
				}
			}
		}
	})
	// A schedule a track's windows run on cannot be deleted: NDManager refuses
	// with the tracks in blocking_resources (conveyor order, lowercase), and the
	// hint names the command that detaches each one.
	t.Run("delete is refused while a track runs on the schedule, naming the tracks", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			body string
			hint string
		}{
			{"one track",
				`{"error":"This schedule carries the update window of firmware track fast. Deleting it would leave its devices pinned with nothing to move them. Detach the track from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast"],"count":1}`,
				"To detach the track from its schedule: 'ndcli firmware track schedule fast --no-schedule'. " +
					"To move it to another schedule: 'ndcli firmware track schedule fast <schedule>'."},
			{"two tracks",
				`{"error":"This schedule carries the update windows of firmware tracks fast, regular. Deleting it would leave their devices pinned with nothing to move them. Detach the tracks from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast","regular"],"count":2}`,
				"To detach the tracks from their schedule: 'ndcli firmware track schedule fast --no-schedule'; 'ndcli firmware track schedule regular --no-schedule'. " +
					"To move them to another schedule: 'ndcli firmware track schedule fast <schedule>'; 'ndcli firmware track schedule regular <schedule>'."},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var message struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(tc.body), &message); err != nil {
					t.Fatal(err)
				}
				cmd := findCommand(t, "schedule", "delete")
				t.Cleanup(func() { _ = cmd.Flags().Set("force", "false") })
				if err := cmd.Flags().Set("force", "true"); err != nil {
					t.Fatal(err)
				}
				var err error
				seen := &firmwareSeen{}
				stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusConflict, tc.body), "table", "", func() error {
					err = runScheduleDelete(cmd, []string{"night"})
					return nil
				})
				if err == nil {
					t.Fatal("expected the refusal")
				}
				if seen.method != http.MethodDelete || seen.path != "/api/v1/organizations/acme/schedules/night" {
					t.Errorf("sent %s %s", seen.method, seen.path)
				}
				if stdout != "" {
					t.Errorf("a refusal leaves stdout empty, got %q", stdout)
				}
				useFormatter(t, output.NewTableFormatter(), "")
				text := printed(err)
				// The server's words as sent, then the hint, and the code in front.
				if want := "Error: [FIRMWARE_SPEC_MANAGED] " + message.Error + "\n  Hint: " + tc.hint + "\n"; text != want {
					t.Errorf("printed\n%q\nwant\n%q", text, want)
				}
				if strings.Contains(text, "<track>") {
					t.Errorf("the tracks are named, so no <track> template is left: %q", text)
				}
				useFormatter(t, output.NewJSONFormatter(), "json")
				doc := decodeCommandError(t, printed(err))
				if doc["code"] != "FIRMWARE_SPEC_MANAGED" || !strings.Contains(doc["message"], "fast --no-schedule") {
					t.Errorf("json error = %v", doc)
				}
			})
		}
	})
	t.Run("the schedule delete help says so", func(t *testing.T) {
		long := findCommand(t, "schedule", "delete").Long
		if !strings.Contains(long, "firmware track") || !strings.Contains(long, "ndcli firmware track schedule <track> --no-schedule") {
			t.Errorf("schedule delete help:\n%s", long)
		}
	})
	t.Run("a FIRMWARE spec is listed as the window of its track", func(t *testing.T) {
		cmd := findCommand(t, "schedule", "tasks", "list")
		body := `{"items":[` +
			`{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","firmware_track":"fast","request":{},"enabled":true},` +
			`{"code":"Bk123456","schedule_name":"night","kind":"BACKUP","firmware_track":null,"request":{"type":"BACKUP"},"enabled":true}` +
			`],"total":2}`
		for _, format := range []string{"table", "simple"} {
			seen := &firmwareSeen{}
			stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), format, "", func() error {
				return runScheduleTasksList(cmd, nil)
			})
			if !strings.Contains(stdout, "FIRMWARE") || !strings.Contains(stdout, "fast track window") || strings.Contains(stdout, "{}") {
				t.Errorf("%s list output:\n%s", format, stdout)
			}
		}

		seen := &firmwareSeen{}
		stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), "json", "", func() error {
			return runScheduleTasksList(cmd, nil)
		})
		var doc struct {
			Tasks []map[string]interface{} `json:"tasks"`
		}
		decodeOne(t, stdout, &doc)
		if len(doc.Tasks) != 2 || doc.Tasks[0]["firmware_track"] != "fast" {
			t.Fatalf("json tasks = %v", doc.Tasks)
		}
		if track, present := doc.Tasks[1]["firmware_track"]; !present || track != nil {
			t.Errorf("a BACKUP spec's firmware_track = %v (present: %v), want null", track, present)
		}
	})
	t.Run("a schedule lists its FIRMWARE windows with their track", func(t *testing.T) {
		cmd := findCommand(t, "schedule", "describe")
		body := `{"organization_name":"acme","name":"night","enabled":true,"schedule":"0 2 * * 6","timezone":"UTC","scheduled_tasks":[` +
			`{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","firmware_track":"regular","request":{},"enabled":true},` +
			`{"code":"Bk123456","schedule_name":"night","kind":"BACKUP","firmware_track":null,"request":{"type":"BACKUP"},"enabled":true}]}`
		for _, format := range []string{"table", "simple", "detailed"} {
			seen := &firmwareSeen{}
			stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), format, "", func() error {
				return runScheduleDescribe(cmd, []string{"night"})
			})
			if seen.path != "/api/v1/organizations/acme/schedules/night" {
				t.Errorf("%s: sent %s", format, seen.path)
			}
			if !strings.Contains(stdout, "Fw123456") || !strings.Contains(stdout, "regular track window") || strings.Contains(stdout, "{}") {
				t.Errorf("%s describe output:\n%s", format, stdout)
			}
		}
	})
	t.Run("a FIRMWARE spec from a control plane that names no track keeps the plain wording", func(t *testing.T) {
		cmd := findCommand(t, "schedule", "tasks", "list")
		body := `{"items":[{"code":"Fw123456","schedule_name":"night","kind":"FIRMWARE","request":{},"enabled":true}],"total":1}`
		seen := &firmwareSeen{}
		stdout, _ := runStreams(t, firmwareReply(t, seen, http.StatusOK, body), "table", "", func() error {
			return runScheduleTasksList(cmd, nil)
		})
		if !strings.Contains(stdout, "FIRMWARE") || !strings.Contains(stdout, "firmware track window") || strings.Contains(stdout, "{}") {
			t.Errorf("list output:\n%s", stdout)
		}
	})
}

func TestFirmwareTrackFlagsComplete(t *testing.T) {
	for _, tc := range []struct {
		path []string
		flag string
		want []string
	}{
		{[]string{"device", "set"}, "track", models.FirmwareAssignTracks},
		{[]string{"status"}, "track", models.FirmwareAssignTracks},
		{[]string{"status"}, "state", models.FirmwareStates},
		{[]string{"version", "promote"}, "track", models.FirmwarePromoteTracks},
		{[]string{"failure", "clear"}, "track", models.FirmwareTracks},
	} {
		cmd := firmwareCommand(t, tc.path...)
		complete, ok := cmd.GetFlagCompletionFunc(tc.flag)
		if !ok {
			t.Errorf("%s --%s has no completion", cmd.CommandPath(), tc.flag)
			continue
		}
		got, directive := complete(cmd, nil, "")
		if !reflect.DeepEqual(got, tc.want) || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s --%s completes %v (%v), want %v", cmd.CommandPath(), tc.flag, got, directive, tc.want)
		}
		// The usage names the same words.
		if usage := cmd.Flags().Lookup(tc.flag).Usage; !strings.Contains(usage, strings.Join(tc.want, ", ")) {
			t.Errorf("%s --%s usage %q does not list %v", cmd.CommandPath(), tc.flag, usage, tc.want)
		}
	}
	for _, path := range [][]string{{"track", "show"}, {"track", "set"}, {"track", "schedule"}} {
		cmd := firmwareCommand(t, path...)
		got, _ := cmd.ValidArgsFunction(cmd, nil, "")
		if !reflect.DeepEqual(got, models.FirmwareTracks) {
			t.Errorf("%s completes %v as its first argument, want the tracks", cmd.CommandPath(), got)
		}
		if after, _ := cmd.ValidArgsFunction(cmd, []string{"fast", "x"}, ""); len(after) != 0 && path[1] != "schedule" {
			t.Errorf("%s completes %v past its argument", cmd.CommandPath(), after)
		}
	}
}

// ── major upgrades ───────────────────────────────────────────────────────────

const (
	fwTrackOn  = `{"track":"fast","soak_days":1,"min_success":0,"schedule":"night","scheduled":true,"auto_apply_major":true,"devices":{"total":1,"managed":1}}`
	fwTrackOff = `{"track":"fast","soak_days":1,"min_success":0,"schedule":"night","scheduled":true,"auto_apply_major":false,"cancelled_tasks":2,"devices":{"total":1,"managed":1}}`

	fwAssignSetting = `{"organization_name":"acme","track":null,"results":[` +
		`{"device_name":"fw-a","result":"updated","track":"fast","state":"managed","boot_environment":false,"notes":[]},` +
		`{"device_name":"fw-b","result":"unchanged","track":"regular","state":"managed","boot_environment":false,"notes":[]}],"cancelled_tasks":1}`
	fwAssignBoth = `{"organization_name":"acme","track":"regular","results":[` +
		`{"device_name":"fw-c","result":"added","track":"regular","state":"enrolling","boot_environment":true,"notes":[]}],"cancelled_tasks":0}`

	fwDevicesMajor = `{"items":[` +
		`{"device_name":"fw-a","device_uuid":"u1","device_status":"ENABLED","agent_version":"1.25.0","track":"fast","state":"managed","pinned_version":"26.1.11_10","series":"26.1",` +
		`"last_status":"success","last_run_at":"2026-10-08T12:00:00Z","major":{"eligible":true},"boot_environment":{"setting":true}},` +
		`{"device_name":"fw-b","device_uuid":"u2","device_status":"ENABLED","agent_version":"1.25.0","track":"fast","state":"managed","pinned_version":"26.1.11_10","series":"26.1",` +
		`"held_reason":"held_insufficient_space","last_status":"held","major":{"eligible":false,"blocked_by":"held_insufficient_space"},` +
		`"boot_environment":{"setting":false,"last":"skipped_no_room"},"notes":["boot_environment_skipped_no_room","third_party_repository"]},` +
		`{"device_name":"fw-c","device_uuid":"u3","device_status":"ENABLED","agent_version":"1.25.0","track":"regular","state":"managed","pinned_version":"26.1.9","series":"26.1",` +
		`"major":{"eligible":false,"blocked_by":"final_minor_not_installed"},"boot_environment":{"setting":true,"last":"made"}}` +
		`],"total":3,"page":1,"per_page":50,"pages":1}`
	fwHistoryMajor = `{"device_name":"fw-a","items":[` +
		`{"completed_at":"2026-10-09T12:00:00Z","track":"fast","action":"advance","outcome":"success","reason":"applied","kind":"major","target":"26.7","from_version":"26.1.11_10","to_version":"26.7.1_1","applied":true},` +
		`{"completed_at":"2026-10-08T12:00:00Z","track":"fast","action":"advance","outcome":"noop","reason":"at_or_above_target","kind":"minor","target":"26.1.11","from_version":"26.1.11_10","to_version":"26.1.11_10","applied":false}` +
		`],"total":2,"page":1,"per_page":50,"pages":1}`
	fwRolloutMajor = `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[],"failures":[],"halts":[],` +
		`"majors":[` +
		`{"track":"fast","series":"26.7","source_series":"26.1","state":"approved","stalled":false,"auto_apply_major":true,"devices_eligible":2,"devices_on_series":1,"devices_hopped":1,` +
		`"devices_awaiting_minor_evidence":0,"offer_not_seen":0,"approved_at":"2026-10-01T00:00:00Z","approval_source":"auto","source_series_end_of_life":true},` +
		`{"track":"regular","series":"26.7","source_series":"26.1","state":"path_closed","soak_remaining_seconds":3600,"evidence":{"reached":0,"required":1},"stalled":true,"stall_reason":"evidence_unreachable",` +
		`"auto_apply_major":false,"devices_eligible":0,"devices_on_series":0,"devices_hopped":0,"devices_awaiting_minor_evidence":1,"offer_not_seen":1,` +
		`"blocked_reason":"previous_track_applies_no_majors","source_series_end_of_life":true,"notice":"payload_changed"}],` +
		`"major_failures":[{"kind":"major","series":"26.7","track":"fast","devices":["fw-b"],"count":1,"last_at":"2026-10-08T12:00:00Z"}],` +
		`"agent_repo":{"configured":true,"channel":"https://repo.netdefense.io/dev/opnsense","trees":[` +
		`{"abi":"FreeBSD:14:amd64","ok":true,"checked_at":"2026-10-12T12:00:00Z"},{"abi":"FreeBSD:15:amd64","ok":false,"checked_at":"2026-10-12T12:00:00Z"}]}}`

	fwSeriesHalt   = `{"halt":{"series":"26.7","kind":"major","reason":"payload changed","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"},"cancelled_tasks":2}`
	fwSeriesUnhalt = `{"halt":{"series":"26.7","kind":"major","reason":"payload changed","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com","unhalted_at":"2026-10-09T12:00:00Z","unhalted_by":"ops@acme.com"},"cancelled_tasks":0}`
	fwSeriesHalts  = `{"items":[` +
		`{"version":"26.7.5","kind":"minor","reason":"regression","halted_at":"2026-10-08T12:00:00Z","halted_by":"admin@acme.com"},` +
		`{"series":"26.7","kind":"major","reason":"payload changed","halted_at":"2026-10-09T12:00:00Z","halted_by":"admin@acme.com"}],"total":2,"page":1,"per_page":50,"pages":1}`
	fwSeriesPromote = `{"series":"26.7","results":[{"track":"fast","result":"approved","target":"26.7"}]}`
	fwSeriesCleared = `{"series":"26.7","cleared":1}`
)

// The commands that came with the major upgrade, in the human formats and in
// JSON, against the route they are documented to call.
func TestFirmwareMajorCommands(t *testing.T) {
	const base = "/api/v1/organizations/acme/firmware"
	for _, tc := range []struct {
		name     string
		path     []string
		args     []string
		response string
		method   string
		route    string
		query    map[string]string
		body     map[string]interface{}
		human    []string
		jsonKey  string
	}{
		{"track set, major upgrades on", []string{"track", "set"}, []string{"fast", "--auto-apply-major=true", "--force"}, fwTrackOn, "PUT",
			base + "/tracks/fast", nil, map[string]interface{}{"auto_apply_major": true},
			[]string{"Major upgrades:", "on: the track's windows move its devices to the next OPNsense series"}, "auto_apply_major"},
		{"track set, major upgrades off (no confirmation needed)", []string{"track", "set"}, []string{"fast", "--auto-apply-major=false"}, fwTrackOff, "PUT",
			base + "/tracks/fast", nil, map[string]interface{}{"auto_apply_major": false},
			[]string{"off: the track's windows ask none of its devices to upgrade", "2 queued major-upgrade steps cancelled"}, "cancelled_tasks"},
		{"track show one", []string{"track", "show"}, []string{"fast"}, fwTrackOn, "GET", base + "/tracks/fast", nil, nil,
			[]string{"Major upgrades:", "on:"}, "auto_apply_major"},
		{"device set, the setting alone", []string{"device", "set"}, []string{"fw-a", "fw-b", "--boot-environment=false"}, fwAssignSetting, "PUT",
			base + "/devices", nil, map[string]interface{}{"devices": []interface{}{"fw-a", "fw-b"}, "boot_environment": false},
			[]string{"2 devices: boot-environment setting changed on 1", "Boot Env", "fw-a", "updated", "off", "1 pending firmware step cancelled"}, "results"},
		{"device set, a track and the setting", []string{"device", "set"}, []string{"fw-c", "--track", "regular", "--boot-environment=true"}, fwAssignBoth, "PUT",
			base + "/devices", nil, map[string]interface{}{"devices": []interface{}{"fw-c"}, "track": "regular", "boot_environment": true},
			[]string{"1 device → regular track: 1 added", "Boot Env", "on"}, "results"},
		{"status", []string{"status"}, nil, fwDevicesMajor, "GET", base + "/devices", nil, nil,
			[]string{"Series", "26.1", "Major", "eligible", "blocked", "Boot Env", "off (last: skipped, no room)", "on (last: made)",
				"major upgrade blocked by the last hold", "major upgrade blocked: the device is not on the final release of its series yet",
				"the last major upgrade found a third-party repository on the device"}, "items"},
		{"history", []string{"history"}, []string{"fw-a"}, fwHistoryMajor, "GET", base + "/devices/fw-a/history", map[string]string{"page": "1"}, nil,
			[]string{"Kind", "major", "minor", "26.7", "26.1.11_10 → 26.7.1_1"}, "items"},
		{"rollout", []string{"rollout"}, nil, fwRolloutMajor, "GET", base + "/status", nil, nil,
			[]string{"Major upgrades:", "26.1 → 26.7", "approved", "path closed", "previous track does not apply major upgrades",
				"Failures blocking a major upgrade:", "fw-b", "NetDefense repository:", "https://repo.netdefense.io/dev/opnsense", "FreeBSD:15:amd64 missing",
				"OPNsense gives 26.1 no more updates"}, "majors"},
		{"version halt, a major upgrade", []string{"version", "halt"}, []string{"26.7", "--major", "--reason", "payload changed"}, fwSeriesHalt, "POST",
			base + "/halts", nil, map[string]interface{}{"series": "26.7", "reason": "payload changed"},
			[]string{"Major upgrade to 26.7 halted", "devices already upgrading are left alone", "2 pending steps cancelled", "payload changed"}, "halt"},
		{"version unhalt, a major upgrade", []string{"version", "unhalt"}, []string{"26.7", "--major", "--force"}, fwSeriesUnhalt, "DELETE",
			base + "/halts/series/26.7", nil, nil, []string{"Halt on major upgrade to 26.7 lifted"}, "halt"},
		{"version promote, a major upgrade", []string{"version", "promote"}, []string{"26.7", "--major", "--track", "fast", "--force"}, fwSeriesPromote, "POST",
			base + "/promote", nil, map[string]interface{}{"series": "26.7", "track": "fast"},
			[]string{"Major upgrade to 26.7 approved in fast", "next window", "Series"}, "results"},
		{"failure clear, a major upgrade", []string{"failure", "clear"}, []string{"26.7", "--major", "--track", "fast"}, fwSeriesCleared, "POST",
			base + "/failures/clear", nil, map[string]interface{}{"series": "26.7", "track": "fast"},
			[]string{"Cleared 1 failure blocking the major upgrade to 26.7 in the fast track"}, "cleared"},
		{"version halts, both kinds", []string{"version", "halts"}, []string{"--kind", "all"}, fwSeriesHalts, "GET",
			base + "/halts", map[string]string{"kind": "all", "active": "true"}, nil,
			[]string{"Kind", "major", "minor", "26.7.5", "26.7", "payload changed"}, "items"},
		{"version halts, the default sends no kind", []string{"version", "halts"}, nil, fwHalts, "GET", base + "/halts", map[string]string{"active": "true"}, nil,
			[]string{"Release", "26.7.5"}, "items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, seen, err := runFirmware(t, http.StatusOK, tc.response, "table", tc.path, tc.args...)
			if err != nil {
				t.Fatalf("command failed: %v", err)
			}
			if seen.method != tc.method || seen.path != tc.route {
				t.Errorf("sent %s %s, want %s %s", seen.method, seen.path, tc.method, tc.route)
			}
			for k, want := range tc.query {
				if got := seen.query.Get(k); got != want {
					t.Errorf("query %s = %q, want %q", k, got, want)
				}
			}
			if tc.name == "version halts, the default sends no kind" {
				if _, present := seen.query["kind"]; present {
					t.Errorf("a default list must leave the kind to the server: %v", seen.query)
				}
			}
			if !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("body = %v, want %v", seen.body, tc.body)
			}
			for _, want := range tc.human {
				if !strings.Contains(stdout, want) {
					t.Errorf("table output is missing %q:\n%s", want, stdout)
				}
			}
			if strings.Contains(tc.name, "(no confirmation needed)") && stderr != "" {
				t.Errorf("turning the switch off asks nothing and warns of nothing, stderr = %q", stderr)
			}

			for _, format := range []string{"simple", "detailed"} {
				out, _, _, err := runFirmware(t, http.StatusOK, tc.response, format, tc.path, tc.args...)
				if err != nil || strings.TrimSpace(out) == "" {
					t.Errorf("%s: err = %v, output %q", format, err, out)
				}
			}
			jsonOut, jsonErr, _, err := runFirmware(t, http.StatusOK, tc.response, "json", tc.path, tc.args...)
			if err != nil {
				t.Fatalf("json: command failed: %v", err)
			}
			var doc map[string]interface{}
			decodeOne(t, jsonOut, &doc)
			if _, ok := doc[tc.jsonKey]; !ok {
				t.Errorf("json output has no %q: %s", tc.jsonKey, jsonOut)
			}
			if jsonErr != "" {
				t.Errorf("json: stderr should be empty, got %q", jsonErr)
			}
		})
	}
}

// -f json is the model: a major upgrade's halt has a series and no version, and
// the setting alone has no track.
func TestFirmwareMajorJSONCarriesWhatTheServerSent(t *testing.T) {
	out, _, _, err := runFirmware(t, http.StatusOK, fwSeriesHalt, "json", []string{"version", "halt"}, "26.7", "--major", "--reason", "r")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"version"`) || !strings.Contains(out, `"series": "26.7"`) {
		t.Errorf("a halt of a major upgrade names its series and no version:\n%s", out)
	}
	out, _, _, err = runFirmware(t, http.StatusOK, fwAssignSetting, "json", []string{"device", "set"}, "fw-a", "--boot-environment=false")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Track   *string                  `json:"track"`
		Results []map[string]interface{} `json:"results"`
	}
	decodeOne(t, out, &doc)
	if doc.Track != nil || doc.Results[0]["boot_environment"] != false {
		t.Errorf("assignment = %s", out)
	}
}

// Turning major upgrades on asks, and says what it means before it asks: to
// stderr, so stdout stays the command's. Nothing is sent before the answer.
func TestFirmwareTurningMajorUpgradesOnAsksAndSaysWhatItMeans(t *testing.T) {
	t.Run("without --force a script is told to pass it, and has been shown the consequences", func(t *testing.T) {
		stdout, stderr, seen, err := runFirmware(t, http.StatusOK, fwTrackOn, "table", []string{"track", "set"}, "fast", "--auto-apply-major=true")
		if err == nil || !strings.Contains(err.Error(), "--force") {
			t.Fatalf("err = %v, want a request for --force when stdin is not a terminal", err)
		}
		if seen.hits != 0 || stdout != "" {
			t.Errorf("nothing may be sent or printed to stdout before the answer: %d hit(s), stdout %q", seen.hits, stdout)
		}
		// The text is written out here, not read from the constants the prompt is
		// built from.
		for _, want := range []string{
			"With major upgrades on, the track's update windows move its devices to the next OPNsense series. Each such upgrade:",
			"takes the device offline for the length of the upgrade and reboots it three times;",
			"has no automatic rollback;",
			"leaves a device that loses power during the base or package install in a state that needs the console or a VM snapshot to recover;",
			"needs a link of about 3 Mbit/s or better",
			"needs 3.25 GiB free",
			"a recovery path from the console only",
			"after a manual rollback, needs the device rebound",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr is missing %q:\n%s", want, stderr)
			}
		}
	})
	t.Run("with --force it goes ahead and does not repeat what the operator waved through", func(t *testing.T) {
		stdout, stderr, seen, err := runFirmware(t, http.StatusOK, fwTrackOn, "table", []string{"track", "set"}, "fast", "--auto-apply-major=true", "--force")
		if err != nil || seen.hits != 1 || stdout == "" {
			t.Fatalf("err = %v, %d hit(s), stdout %q", err, seen.hits, stdout)
		}
		if strings.Contains(stderr, "no automatic rollback") {
			t.Errorf("--force skips the prompt and its text, stderr = %q", stderr)
		}
	})
	t.Run("turning it off is the safe direction and asks nothing", func(t *testing.T) {
		_, stderr, seen, err := runFirmware(t, http.StatusOK, fwTrackOff, "table", []string{"track", "set"}, "fast", "--auto-apply-major=false")
		if err != nil || seen.hits != 1 || stderr != "" {
			t.Errorf("err = %v, %d hit(s), stderr %q", err, seen.hits, stderr)
		}
	})
	t.Run("promoting a major upgrade says what follows", func(t *testing.T) {
		cmd := firmwareCommand(t, "version", "promote")
		if long := strings.Join(strings.Fields(cmd.Long), " "); !strings.Contains(long, "a track with major upgrades on then upgrades every device that passes the gates at its next window: offline for the length of the upgrade, three reboots, no automatic rollback") {
			t.Errorf("promote --help:\n%s", long)
		}
	})
	t.Run("lifting the halt on a major upgrade says what follows, and asks", func(t *testing.T) {
		cmd := firmwareCommand(t, "version", "unhalt")
		long := strings.Join(strings.Fields(cmd.Long), " ")
		for _, want := range []string{
			"Lifting the halt on a major upgrade lets a track with major upgrades on that has approved the series upgrade every device that passes the gates at its next window: offline for the length of the upgrade, three reboots, no automatic rollback.",
			"So it asks for a confirmation (--force skips the prompt, and is required when stdin is not a TTY). Lifting the halt on a release asks nothing.",
		} {
			if !strings.Contains(long, want) {
				t.Errorf("unhalt --help is missing %q:\n%s", want, long)
			}
		}
		if f := cmd.Flags().Lookup("force"); f == nil || !strings.Contains(f.Usage, "required when stdin is not a TTY") {
			t.Errorf("unhalt has no --force that says so: %+v", f)
		}
		// A release's halt is lifted as it always was.
		_, stderr, seen, err := runFirmware(t, http.StatusOK, fwUnhalt, "table", []string{"version", "unhalt"}, "26.7.5")
		if err != nil || seen.hits != 1 || stderr != "" {
			t.Errorf("a release: err = %v, %d hit(s), stderr %q", err, seen.hits, stderr)
		}
	})
	t.Run("the reason is for a release or a major upgrade", func(t *testing.T) {
		if f := firmwareCommand(t, "version", "halt").Flags().Lookup("reason"); f == nil || !strings.Contains(f.Usage, "Why the release or major upgrade is halted") {
			t.Errorf("--reason = %+v", f)
		}
	})
	t.Run("the help of the switch says it too", func(t *testing.T) {
		long := strings.Join(strings.Fields(firmwareCommand(t, "track", "set").Long), " ")
		for _, want := range []string{
			"an upgrade takes the device offline, reboots it three times and has no automatic rollback",
			"--force skips the prompt, and is required when stdin is not a TTY",
			"It is changed on its own, not with --soak-days or --min-success.",
			"--auto-apply-major=false cancels the upgrade steps that were queued for the track's devices and not yet dispatched; one already running goes on.",
		} {
			if !strings.Contains(long, want) {
				t.Errorf("track set --help is missing %q:\n%s", want, long)
			}
		}
	})
}

// The flag groups cobra enforces on the real path (the harness calls RunE after
// ParseFlags, which skips them).
func TestFirmwareMajorFlagGroups(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    []string
		args    []string
		wantErr string
	}{
		{"device set with nothing", []string{"device", "set"}, []string{"fw-a"}, "at least one of the flags in the group [track boot-environment] is required"},
		{"device set, the setting alone", []string{"device", "set"}, []string{"fw-a", "--boot-environment=false"}, ""},
		{"device set, the track alone", []string{"device", "set"}, []string{"fw-a", "--track", "fast"}, ""},
		{"device set, both", []string{"device", "set"}, []string{"fw-a", "--track", "fast", "--boot-environment=true"}, ""},
		{"track set with nothing", []string{"track", "set"}, []string{"fast"}, "at least one of the flags in the group [soak-days min-success auto-apply-major] is required"},
		{"track set, the switch alone", []string{"track", "set"}, []string{"fast", "--auto-apply-major=true"}, ""},
		{"track set, soak days and min success together", []string{"track", "set"}, []string{"regular", "--soak-days", "3", "--min-success", "1"}, ""},
		{"track set, the switch beside soak days", []string{"track", "set"}, []string{"fast", "--auto-apply-major=true", "--soak-days", "3"}, "if any flags in the group [auto-apply-major soak-days] are set none of the others can be"},
		{"track set, the switch beside min success", []string{"track", "set"}, []string{"fast", "--auto-apply-major=false", "--min-success", "0"}, "if any flags in the group [auto-apply-major min-success] are set none of the others can be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := firmwareCommand(t, tc.path...)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			err := cmd.ValidateFlagGroups()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("err = %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
	// --track is not a required flag any more: the setting can stand alone.
	if f := firmwareCommand(t, "device", "set").Flags().Lookup("track"); f == nil || f.Annotations[cobra.BashCompOneRequiredFlag] != nil {
		t.Errorf("--track must not be required: %+v", f)
	}
}

// Every form the major upgrade added reaches its route and, on a plan without
// the feature, gets the server's refusal as sent, with nothing added.
func TestFirmwareMajorFormsAnswerNotAvailableAsSent(t *testing.T) {
	const message = "Firmware tracks are not available on your current plan."
	body := `{"error":"` + message + `","code":"FEATURE_NOT_AVAILABLE"}`
	for name, tc := range map[string]struct {
		path []string
		args []string
	}{
		"track set --auto-apply-major=true":  {[]string{"track", "set"}, []string{"fast", "--auto-apply-major=true", "--force"}},
		"track set --auto-apply-major=false": {[]string{"track", "set"}, []string{"fast", "--auto-apply-major=false"}},
		"device set --boot-environment":      {[]string{"device", "set"}, []string{"fw-a", "--boot-environment=false"}},
		"version halt --major":               {[]string{"version", "halt"}, []string{"26.7", "--major", "--reason", "r"}},
		"version unhalt --major":             {[]string{"version", "unhalt"}, []string{"26.7", "--major", "--force"}},
		"version promote --major":            {[]string{"version", "promote"}, []string{"26.7", "--major", "--track", "all", "--force"}},
		"failure clear --major":              {[]string{"failure", "clear"}, []string{"26.7", "--major"}},
		"version halts --kind all":           {[]string{"version", "halts"}, []string{"--kind", "all"}},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, _, seen, err := runFirmware(t, http.StatusForbidden, body, "table", tc.path, tc.args...)
			if err == nil || seen.hits != 1 {
				t.Fatalf("err = %v, %d request(s)", err, seen.hits)
			}
			useFormatter(t, output.NewTableFormatter(), "")
			if got, want := printed(err), "Error: [FEATURE_NOT_AVAILABLE] "+message+"\n"; got != want {
				t.Errorf("printed %q, want %q", got, want)
			}
			if stdout != "" {
				t.Errorf("a refusal leaves stdout empty, got %q", stdout)
			}
		})
	}
}

// The two new refusals arrive with the way forward in CLI words, and the
// devices FIRMWARE_DEVICE_UNMANAGED lists are named.
func TestFirmwareMajorRefusalsAreAdvisedInCLIWords(t *testing.T) {
	t.Run("the upgrade path is closed", func(t *testing.T) {
		body := `{"error":"The upgrade to 26.7 is not open","code":"FIRMWARE_UPGRADE_NOT_OPEN"}`
		_, _, _, err := runFirmware(t, 409, body, "table", []string{"version", "promote"}, "26.7", "--major", "--track", "fast", "--force")
		if err == nil {
			t.Fatal("expected the refusal")
		}
		useFormatter(t, output.NewTableFormatter(), "")
		text := printed(err)
		for _, want := range []string{"Error: [FIRMWARE_UPGRADE_NOT_OPEN] The upgrade to 26.7 is not open\n  Hint: ", "OPNsense's upgrade path is closed", "'ndcli firmware rollout'"} {
			if !strings.Contains(text, want) {
				t.Errorf("printed %q, missing %q", text, want)
			}
		}
	})
	t.Run("devices in no track", func(t *testing.T) {
		// NDManager 1.97.0's own words: they name the devices and say what to do.
		const message = "The boot-environment setting belongs to a device's track row, which these devices do not have; add them to a track first, or send the track too: fw-a, fw-b"
		body := `{"error":"` + message + `","code":"FIRMWARE_DEVICE_UNMANAGED","blocking_resources":["fw-a","fw-b"],"count":2}`
		stdout, _, _, err := runFirmware(t, 409, body, "table", []string{"device", "set"}, "fw-a", "fw-b", "fw-c", "--boot-environment=false")
		if err == nil {
			t.Fatal("expected the refusal")
		}
		if stdout != "" {
			t.Errorf("a refusal leaves stdout empty, got %q", stdout)
		}
		useFormatter(t, output.NewTableFormatter(), "")
		text := printed(err)
		want := "Error: [FIRMWARE_DEVICE_UNMANAGED] " + message + "\n  Hint: " +
			"To add the devices to a track: 'ndcli firmware device set <device> --track <track>'. " +
			"To add them with the setting in one go: 'ndcli firmware device set <device> --track <track> --boot-environment=<bool>'.\n"
		if text != want {
			t.Errorf("printed\n%q\nwant\n%q", text, want)
		}
		if n := strings.Count(text, "fw-a"); n != 1 {
			t.Errorf("fw-a is named %d times, want once, in the server's words: %q", n, text)
		}
		useFormatter(t, output.NewJSONFormatter(), "json")
		doc := decodeCommandError(t, printed(err))
		if doc["code"] != "FIRMWARE_DEVICE_UNMANAGED" || !strings.Contains(doc["message"], "fw-a, fw-b") {
			t.Errorf("json error = %v", doc)
		}
	})
	t.Run("an NDManager that predates the setting", func(t *testing.T) {
		_, _, _, err := runFirmware(t, http.StatusOK, fwAssign, "table", []string{"device", "set"}, "fw-a", "--boot-environment=false")
		if err == nil || !strings.Contains(err.Error(), "this NDManager predates the boot-environment setting") {
			t.Fatalf("err = %v", err)
		}
		useFormatter(t, output.NewTableFormatter(), "")
		if text := printed(err); !strings.HasPrefix(text, "Error: [SERVER_TOO_OLD] this NDManager predates the boot-environment setting") {
			t.Errorf("printed %q", text)
		}
	})
	// Every NDManager that predates the switch ignores the key, sees an update
	// that names no field, and answers 400 MISSING_FIELD; the missing echo is the
	// second guard. Either way the user is told why, not that a field is missing.
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"the 400 an NDManager that predates the switch gives": {http.StatusBadRequest, `{"error":"Provide at least one field to change","code":"MISSING_FIELD"}`},
		"an answer that does not carry the switch":            {http.StatusOK, fwTrack},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, _, seen, err := runFirmware(t, tc.status, tc.body, "table", []string{"track", "set"}, "regular", "--auto-apply-major=true", "--force")
			if err == nil || !strings.Contains(err.Error(), "this NDManager predates major upgrades") {
				t.Fatalf("err = %v", err)
			}
			if stdout != "" || seen.hits != 1 || !reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": true}) {
				t.Errorf("stdout %q, %d hit(s), body %v", stdout, seen.hits, seen.body)
			}
			useFormatter(t, output.NewTableFormatter(), "")
			text := printed(err)
			if !strings.HasPrefix(text, "Error: [SERVER_TOO_OLD] this NDManager predates major upgrades: ") || strings.Contains(text, "[MISSING_FIELD]") {
				t.Errorf("printed %q", text)
			}
			useFormatter(t, output.NewJSONFormatter(), "json")
			if doc := decodeCommandError(t, printed(err)); doc["code"] != "SERVER_TOO_OLD" {
				t.Errorf("json error = %v", doc)
			}
		})
	}
}

// The unhalt argument completes from the halts of the kind asked for.
func TestFirmwareUnhaltCompletesFromTheHaltsOfItsKind(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		kind  string
		reply string
		want  []string
	}{
		{"releases", nil, "minor", `{"items":[{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"t","halted_by":"a"}],"total":1,"page":1,"per_page":200,"pages":1}`, []string{"26.7.5"}},
		{"series", []string{"--major"}, "major", `{"items":[{"series":"26.7","kind":"major","reason":"r","halted_at":"t","halted_by":"a"}],"total":1,"page":1,"per_page":200,"pages":1}`, []string{"26.7"}},
		// An NDManager that predates major upgrades ignores kind and answers its
		// release halts: none of them is a series to offer.
		{"series, from an NDManager that ignores the kind", []string{"--major"}, "major", `{"items":[{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"t","halted_by":"a"},{"version":"26.7.4","reason":"r","halted_at":"t","halted_by":"a"}],"total":2,"page":1,"per_page":200,"pages":1}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := firmwareCommand(t, "version", "unhalt")
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			seen := &firmwareSeen{}
			var got []string
			runStreams(t, firmwareReply(t, seen, http.StatusOK, tc.reply), "table", "", func() error {
				got, _ = cmd.ValidArgsFunction(cmd, nil, "")
				return nil
			})
			if len(got) != len(tc.want) || (len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Errorf("completes %v, want %v", got, tc.want)
			}
			if seen.query.Get("kind") != tc.kind {
				t.Errorf("kind = %q, want %q", seen.query.Get("kind"), tc.kind)
			}
		})
	}
	cmd := firmwareCommand(t, "version", "halts")
	complete, ok := cmd.GetFlagCompletionFunc("kind")
	if !ok {
		t.Fatal("version halts --kind has no completion")
	}
	if got, _ := complete(cmd, nil, ""); !reflect.DeepEqual(got, models.FirmwareHaltKinds) {
		t.Errorf("--kind completes %v, want %v", got, models.FirmwareHaltKinds)
	}
}

// A device in a track is refused a manual upgrade of any mode, a major one
// included, and the refusal still points at ndcli firmware.
func TestRunFirmwareUpgradeModeMajorOnAManagedDeviceKeepsItsRefusal(t *testing.T) {
	body := `{"error":"Device 'fw1' is in a firmware track, which moves it between releases; move it out of its track to run a firmware upgrade on it.","code":"FIRMWARE_DEVICE_MANAGED"}`
	for _, args := range [][]string{
		{"--device", "fw1", "--mode", "major"},
		{"--device", "fw1", "--mode", "major", "--schedule", "nightly"},
	} {
		cmd := firmwareUpgradeCommand(t)
		srv := &firmwareSeen{}
		var err error
		_, _ = runStreams(t, firmwareReply(t, srv, http.StatusConflict, body), "table", "", func() error {
			if perr := cmd.ParseFlags(args); perr != nil {
				t.Fatalf("parse %v: %v", args, perr)
			}
			err = cmd.RunE(cmd, cmd.Flags().Args())
			return nil
		})
		if err == nil {
			t.Fatalf("%v: expected the refusal", args)
		}
		useFormatter(t, output.NewTableFormatter(), "")
		text := printed(err)
		for _, want := range []string{"[FIRMWARE_DEVICE_MANAGED]", "Hint:", "'ndcli firmware status'", "'ndcli firmware device set <device> --track unmanaged'"} {
			if !strings.Contains(text, want) {
				t.Errorf("%v: printed %q, missing %q", args, text, want)
			}
		}
	}
}
