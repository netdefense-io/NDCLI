package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// plainFormatters builds the four formatters over a buffer, colour off so the
// text compares as written. The table formatter's styled tables go to the
// buffer too (RenderTo); the few places that still print to os.Stdout are
// captured by the callers that need them.
func plainFormatters(t *testing.T) (map[string]Formatter, map[string]*bytes.Buffer) {
	t.Helper()
	restore := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = restore })

	bufs := map[string]*bytes.Buffer{}
	fs := map[string]Formatter{}
	for name, factory := range formatterFactories {
		b := &bytes.Buffer{}
		bufs[name], fs[name] = b, factory(b)
	}
	return fs, bufs
}

func intRef(n int) *int    { return &n }
func boolRef(b bool) *bool { return &b }

// A year out, so a "next window" never reads as due and a soak never as over.
const farFuture = "2099-01-02T03:04:05Z"

func firmwareConfigSample() *models.FirmwareConfig {
	return &models.FirmwareConfig{
		OrganizationName: "acme-corp", Status: "ENABLED", Reboot: true, PlanEnabled: true,
		CreatedAt: "2026-10-08T12:00:00Z", UpdatedAt: "2026-10-09T08:00:00Z",
		Message: "Firmware configuration created",
		Tracks: []models.FirmwareTrack{
			{Track: "fast", SoakDays: 1, MinSuccess: 0, Schedule: "saturday-night", Scheduled: true, NextRunAt: farFuture, AutoApplyMajor: boolRef(true),
				Devices: models.FirmwareDeviceCounts{Total: 4, Managed: 3, Held: 1}},
			{Track: "regular", SoakDays: 9, MinSuccess: 1, Schedule: "weekly", Scheduled: false, AutoApplyMajor: boolRef(false),
				Devices: models.FirmwareDeviceCounts{Total: 2, Managed: 2}},
			{Track: "stable", SoakDays: 10, MinSuccess: 1, AutoApplyMajor: boolRef(false)},
		},
	}
}

// firmwareLegacy strips what a control plane that predates major upgrades does
// not send, so a test can show that its output is what it always was.
func firmwareLegacyConfig() *models.FirmwareConfig {
	cfg := firmwareConfigSample()
	for i := range cfg.Tracks {
		cfg.Tracks[i].AutoApplyMajor = nil
	}
	return cfg
}

func firmwareDevicesSample() *models.FirmwareDeviceList {
	return &models.FirmwareDeviceList{Total: 4, Page: 1, PerPage: 50, Pages: 1, Items: []models.FirmwareDevice{
		{DeviceName: "fw-a", DeviceUUID: "u1", DeviceStatus: "ENABLED", AgentVersion: "1.24.0", Track: "fast", State: "managed",
			PinnedVersion: "26.7.3", LastRunAt: "2026-10-08T12:00:00Z", LastStatus: "success", Drift: boolRef(false)},
		{DeviceName: "fw-b", DeviceUUID: "u2", DeviceStatus: "ENABLED", AgentVersion: "1.24.0", Track: "fast", State: "held",
			HeldReason: "held_custom_flavour", LastRunAt: "2026-10-08T12:00:00Z", LastStatus: "held", Drift: boolRef(true),
			ReinstallEntries: intRef(1), LastError: ""},
		{DeviceName: "fw-c", DeviceUUID: "u3", DeviceStatus: "DISABLED", AgentVersion: "1.22.1", Track: "regular", State: "managed",
			PinnedVersion: "26.7.2", LastStatus: "failed", LastError: "the update did not finish", Blocked: "agent_too_old"},
		{DeviceName: "fw-d", DeviceUUID: "u4", DeviceStatus: "ENABLED",
			Released: &models.FirmwareReleasedInfo{At: "2026-10-01T00:00:00Z", Reason: "dead_man"}},
	}}
}

// firmwareDevicesMajorSample is the page of a control plane that knows major
// upgrades: the device rows carry a series, whether the next window would send
// the major upgrade, the boot-environment setting and the notes of the newest one.
func firmwareDevicesMajorSample() *models.FirmwareDeviceList {
	return &models.FirmwareDeviceList{Total: 5, Page: 1, PerPage: 50, Pages: 1, Items: []models.FirmwareDevice{
		{DeviceName: "fw-a", DeviceUUID: "u1", DeviceStatus: "ENABLED", AgentVersion: "1.25.0", Track: "fast", State: "managed",
			PinnedVersion: "26.1.11_10", Series: "26.1", LastRunAt: "2026-10-08T12:00:00Z", LastStatus: "success",
			Major: &models.FirmwareDeviceMajor{Eligible: true}, BootEnvironment: &models.FirmwareBootEnvironment{Setting: true}},
		{DeviceName: "fw-b", DeviceUUID: "u2", DeviceStatus: "ENABLED", AgentVersion: "1.25.0", Track: "fast", State: "managed",
			PinnedVersion: "26.1.11_10", Series: "26.1", HeldReason: "held_insufficient_space", LastStatus: "held",
			Major:           &models.FirmwareDeviceMajor{Eligible: false, BlockedBy: "held_insufficient_space"},
			BootEnvironment: &models.FirmwareBootEnvironment{Setting: false, Last: "skipped_no_room"},
			Notes:           []string{"boot_environment_skipped_no_room", "third_party_repository", "third_party_package"}},
		{DeviceName: "fw-c", DeviceUUID: "u3", DeviceStatus: "ENABLED", AgentVersion: "1.25.0", Track: "regular", State: "managed",
			PinnedVersion: "26.1.9", Series: "26.1",
			Major:           &models.FirmwareDeviceMajor{Eligible: false, BlockedBy: "final_minor_not_installed"},
			BootEnvironment: &models.FirmwareBootEnvironment{Setting: true, Last: "made"}},
		{DeviceName: "fw-d", DeviceUUID: "u4", DeviceStatus: "DISABLED", Track: "stable", State: "held", PinnedVersion: "26.7.1_1", Series: "26.7",
			BootEnvironment: &models.FirmwareBootEnvironment{Setting: true}},
		{DeviceName: "fw-e", DeviceUUID: "u5", DeviceStatus: "ENABLED"},
	}}
}

func firmwareRolloutSample() *models.FirmwareRollout {
	return &models.FirmwareRollout{OrganizationName: "acme-corp", Status: "ENABLED", PlanEnabled: true,
		Tracks: []models.FirmwareTrackStatus{
			{Track: "fast", SoakDays: 1, Schedule: "saturday-night", Scheduled: true,
				Devices: models.FirmwareDeviceCounts{Total: 4, Managed: 3, Held: 1},
				Series: []models.FirmwareSeriesStatus{{Series: "26.7", Target: "26.7.4", TargetApprovedAt: "2026-10-01T00:00:00Z", TargetSource: "auto",
					NextCandidate: &models.FirmwareCandidate{Version: "26.7.5", State: "soaking", SoakEndsAt: farFuture, SoakRemainingSeconds: intRef(14000)}}}},
			{Track: "regular", SoakDays: 9, MinSuccess: 1, Devices: models.FirmwareDeviceCounts{Total: 2, Managed: 2},
				Series: []models.FirmwareSeriesStatus{{Series: "26.7", Target: "26.7.3", TargetSource: "manual",
					NextCandidate: &models.FirmwareCandidate{Version: "26.7.4", State: "waiting_for_evidence",
						Evidence: &models.FirmwareEvidence{Reached: 0, Required: 1}, Stalled: true, StallReason: "evidence_unreachable"}}}},
			{Track: "stable", SoakDays: 10, MinSuccess: 1},
		},
		Failures: []models.FirmwareFailure{{Version: "26.7.4", Track: "fast", Devices: []string{"fw-b", "fw-e"}, Count: 2, LastAt: "2026-10-08T12:00:00Z"}},
		Halts:    []models.FirmwareHalt{{Version: "26.7.5", Kind: "minor", Reason: "regression in the VPN stack", HaltedAt: "2026-10-08T12:00:00Z", HaltedBy: "admin@acme.com"}},
	}
}

// firmwareRolloutMajorSample is the board of a control plane that knows major
// upgrades, beside the release sample: one major upgrade approved and applied,
// one stalled on a previous track that applies none, one with the path closed.
func firmwareRolloutMajorSample() *models.FirmwareRollout {
	r := firmwareRolloutSample()
	r.Majors = []models.FirmwareMajorEntry{
		{Track: "fast", Series: "26.7", SourceSeries: "26.1", State: "approved", AutoApplyMajor: true,
			DevicesEligible: 2, DevicesOnSeries: 3, DevicesHopped: 1, ApprovedAt: "2026-10-01T00:00:00Z", ApprovalSource: "auto",
			UpgradeOpenAt: "2026-09-20T00:00:00Z", SourceSeriesEndOfLife: true},
		{Track: "regular", Series: "26.7", SourceSeries: "26.1", State: "waiting_for_evidence", AutoApplyMajor: false,
			SoakEndsAt: farFuture, SoakRemainingSeconds: intRef(7200), Evidence: &models.FirmwareEvidence{Reached: 0, Required: 1},
			Stalled: true, StallReason: "evidence_unreachable", BlockedReason: "previous_track_applies_no_majors",
			DevicesAwaitingMinorEvidence: 1, OfferNotSeen: 2, SourceSeriesEndOfLife: true, Notice: "payload_changed"},
		{Track: "stable", Series: "26.7", SourceSeries: "26.1", State: "path_closed", AutoApplyMajor: false, BlockedReason: "path_closed"},
	}
	r.MajorFailures = []models.FirmwareMajorFailure{{Kind: "major", Series: "26.7", Track: "fast", Devices: []string{"fw-b"}, Count: 1, LastAt: "2026-10-08T12:00:00Z"}}
	r.AgentRepo = &models.FirmwareAgentRepo{Configured: true, Channel: "https://repo.netdefense.io/dev/opnsense", Trees: []models.FirmwareAgentRepoTree{
		{ABI: "FreeBSD:14:amd64", OK: true, CheckedAt: "2026-10-12T12:00:00Z"},
		{ABI: "FreeBSD:15:amd64", OK: false, CheckedAt: "2026-10-12T12:00:00Z"},
	}}
	return r
}

func firmwareHistorySample() *models.FirmwareHistory {
	return &models.FirmwareHistory{DeviceName: "fw-a", Total: 3, Page: 1, PerPage: 50, Pages: 1, Items: []models.FirmwareHistoryItem{
		{CompletedAt: "2026-10-08T12:00:00Z", Track: "fast", Action: "advance", Outcome: "success", Reason: "applied", Target: "26.7.5", FromVersion: "26.7.3", ToVersion: "26.7.5", Applied: true},
		{CompletedAt: "2026-10-07T12:00:00Z", Track: "fast", Action: "advance", Outcome: "failed", Target: "26.7.4", ClearedAt: "2026-10-07T13:00:00Z", ClearedBy: "admin@acme.com"},
		{CompletedAt: "2026-10-01T12:00:00Z", Track: "fast", Action: "enroll", Outcome: "success", Reason: "enrolled", FromVersion: "26.7.3", ToVersion: "26.7.3"},
	}}
}

// firmwareHistoryMajorSample is a ledger that holds a major upgrade among minors.
func firmwareHistoryMajorSample() *models.FirmwareHistory {
	return &models.FirmwareHistory{DeviceName: "fw-a", Total: 3, Page: 1, PerPage: 50, Pages: 1, Items: []models.FirmwareHistoryItem{
		{CompletedAt: "2026-10-09T12:00:00Z", Track: "fast", Action: "advance", Outcome: "success", Reason: "applied", Kind: "major", Target: "26.7",
			FromVersion: "26.1.11_10", ToVersion: "26.7.1_1", Applied: true},
		{CompletedAt: "2026-10-08T12:00:00Z", Track: "fast", Action: "advance", Outcome: "held", Reason: "held_insufficient_space", Kind: "major", Target: "26.7",
			FromVersion: "26.1.11_10", ToVersion: "26.1.11_10"},
		{CompletedAt: "2026-10-07T12:00:00Z", Track: "fast", Action: "advance", Outcome: "noop", Reason: "at_or_above_target", Kind: "minor", Target: "26.1.11",
			FromVersion: "26.1.11_10", ToVersion: "26.1.11_10"},
	}}
}

func firmwareHaltSample() models.FirmwareHalt {
	return models.FirmwareHalt{Version: "26.7.5", Kind: "minor", Reason: "regression", HaltedAt: "2026-10-08T12:00:00Z", HaltedBy: "admin@acme.com"}
}

// firmwareSeriesHaltSample is a halt of a major upgrade: its series, no version.
func firmwareSeriesHaltSample() models.FirmwareHalt {
	return models.FirmwareHalt{Series: "26.7", Kind: "major", Reason: "the payload changed", HaltedAt: "2026-10-09T12:00:00Z", HaltedBy: "admin@acme.com"}
}

// firmwareSamples gives every FormatFirmware* method its arguments. It is also
// the formatter coverage guard: a method added to the Formatter interface with
// no sample here fails TestEveryFirmwareViewRendersInEveryFormat.
func firmwareSamples() map[string][]interface{} {
	cfg := firmwareConfigSample()
	lifted := firmwareHaltSample()
	lifted.UnhaltedAt, lifted.UnhaltedBy = "2026-10-09T12:00:00Z", "ops@acme.com"
	return map[string][]interface{}{
		"FormatFirmwareConfig":        {cfg},
		"FormatFirmwareConfigDeleted": {&models.FirmwareConfigDeleted{Organization: "acme-corp", Message: "Firmware configuration deleted"}},
		"FormatFirmwareTracks":        {cfg.Tracks},
		"FormatFirmwareTrack":         {&cfg.Tracks[0]},
		"FormatFirmwareAssignment": {&models.FirmwareAssignment{OrganizationName: "acme", Track: "fast", CancelledTasks: 1,
			Results: []models.FirmwareAssignmentResult{
				{DeviceName: "fw-a", Result: "added", Track: "fast", State: "enrolling", BootEnvironment: boolRef(true), Notes: []string{}},
				{DeviceName: "fw-b", Result: "moved", Track: "fast", State: "managed", BootEnvironment: boolRef(false), Notes: []string{"agent_too_old", "device_disabled"}},
			}}},
		"FormatFirmwareDevices": {firmwareDevicesMajorSample()},
		"FormatFirmwareHistory": {firmwareHistoryMajorSample()},
		"FormatFirmwareRollout": {firmwareRolloutMajorSample()},
		"FormatFirmwareHalts": {&models.FirmwareHaltList{Total: 3, Page: 1, PerPage: 50, Pages: 1,
			Items: []models.FirmwareHalt{firmwareHaltSample(), lifted, firmwareSeriesHaltSample()}}},
		"FormatFirmwareHalt": {&models.FirmwareHaltResult{Halt: firmwareHaltSample(), CancelledTasks: 2}},
		"FormatFirmwarePromotion": {&models.FirmwarePromotion{Version: "26.7.5", Results: []models.FirmwarePromotionResult{
			{Track: "fast", Result: "approved", Target: "26.7.5"}, {Track: "regular", Result: "already_approved", Target: "26.7.5"}}}},
		"FormatFirmwareFailuresCleared": {&models.FirmwareFailuresCleared{Version: "26.7.5", Track: "fast", Cleared: 2}},
	}
}

func call(t *testing.T, f Formatter, method string, args []interface{}) error {
	t.Helper()
	m := reflect.ValueOf(f).MethodByName(method)
	if !m.IsValid() {
		t.Fatalf("%T has no %s", f, method)
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	out := m.Call(in)
	if err, _ := out[0].Interface().(error); err != nil {
		return err
	}
	return nil
}

// TestEveryFirmwareViewRendersInEveryFormat is the formatter coverage guard
// for the firmware views: each FormatFirmware* method of the Formatter
// interface is implemented by all four formatters, prints something, and in
// JSON prints exactly one parseable document.
func TestEveryFirmwareViewRendersInEveryFormat(t *testing.T) {
	samples := firmwareSamples()
	iface := reflect.TypeOf((*Formatter)(nil)).Elem()
	seen := 0
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "FormatFirmware") {
			continue
		}
		seen++
		args, ok := samples[name]
		if !ok {
			t.Errorf("%s has no sample in firmwareSamples: add one so all four formatters are exercised", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			fs, bufs := plainFormatters(t)
			for format, f := range fs {
				stdout := captureStdout(t, func() {
					if err := call(t, f, name, args); err != nil {
						t.Errorf("%s: %v", format, err)
					}
				})
				out := bufs[format].String() + stdout
				if strings.TrimSpace(out) == "" {
					t.Errorf("%s printed nothing", format)
				}
				if format == "json" {
					var doc interface{}
					dec := json.NewDecoder(strings.NewReader(out))
					if err := dec.Decode(&doc); err != nil {
						t.Fatalf("json output is not a document: %v\n%s", err, out)
					}
					if dec.More() {
						t.Errorf("json output carries more than one document:\n%s", out)
					}
				}
			}
		})
	}
	if seen != len(samples) {
		t.Errorf("the interface has %d FormatFirmware methods, the samples %d", seen, len(samples))
	}
}

// JSON is the model, byte for byte what the server sent (nulls aside).
func TestFirmwareJSONIsTheModel(t *testing.T) {
	fs, bufs := plainFormatters(t)
	for method, args := range firmwareSamples() {
		bufs["json"].Reset()
		if err := call(t, fs["json"], method, args); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var got interface{}
		if err := json.Unmarshal(bufs["json"].Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		want := args[0]
		if method == "FormatFirmwareTracks" {
			want = map[string]interface{}{"tracks": args[0], "total": 3}
		}
		wantJSON, _ := json.Marshal(want)
		var wantDoc interface{}
		_ = json.Unmarshal(wantJSON, &wantDoc)
		if !reflect.DeepEqual(got, wantDoc) {
			t.Errorf("%s: json = %s, want %s", method, bufs["json"].String(), wantJSON)
		}
	}
}

func render(t *testing.T, format, method string, args ...interface{}) string {
	t.Helper()
	fs, bufs := plainFormatters(t)
	stdout := captureStdout(t, func() {
		if err := call(t, fs[format], method, args); err != nil {
			t.Fatalf("%s %s: %v", format, method, err)
		}
	})
	return bufs[format].String() + stdout
}

// squash flattens a box: the border characters go and every run of white space
// becomes one space, so a phrase that wrapped inside a box still reads whole.
func squash(out string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "┌", " ", "└", " ", "┐", " ", "┘", " ", "╭", " ", "╰", " ", "╮", " ", "╯", " ").Replace(out)), " ")
}

func mustContain(t *testing.T, label, out string, wants ...string) {
	t.Helper()
	flat := squash(out)
	for _, want := range wants {
		if !strings.Contains(out, want) && !strings.Contains(flat, want) {
			t.Errorf("%s: missing %q in:\n%s", label, want, out)
		}
	}
}

func mustNotContain(t *testing.T, label, out string, nots ...string) {
	t.Helper()
	for _, not := range nots {
		if strings.Contains(out, not) {
			t.Errorf("%s: should not contain %q:\n%s", label, not, out)
		}
	}
}

func TestFirmwareConfigRendering(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareConfig", firmwareConfigSample())
			mustContain(t, format, out,
				"Firmware configuration created", // what a write reported
				"acme-corp", "Enabled", "saturday-night",
				"fast", "regular", "stable",
				"1 day", "9 days", "10 days",
				"weekly (disabled)", // attached to a schedule that is not firing
				"3 managed", "1 held",
			)
		})
	}

	t.Run("a plan without the feature says no step is sent and the devices are being released", func(t *testing.T) {
		cfg := firmwareConfigSample()
		cfg.PlanEnabled = false
		cfg.Reboot = false
		cfg.Status = "DISABLED"
		cfg.Message = ""
		cfg.ReleasedDevices = intRef(4)
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareConfig", cfg)
			mustContain(t, format, out,
				"not included (no new steps are sent; devices still in a track are being released)",
				"userland packages only", "Disabled", "4 devices leaving their track")
			mustNotContain(t, format, out, "created", "nothing acts")
		}
	})
	t.Run("a plan with the feature says it is included", func(t *testing.T) {
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareConfig", firmwareConfigSample())
			mustContain(t, format, out, "Plan: included")
			mustNotContain(t, format, out, "not included", "being released")
		}
	})

	// The server sends released_devices on the status change both ways, 0 when
	// enabling; only a disable has devices leaving their tracks to report.
	t.Run("enabling does not report devices leaving their track", func(t *testing.T) {
		cfg := firmwareConfigSample()
		cfg.Message = "Firmware enabled"
		cfg.ReleasedDevices = intRef(0)
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareConfig", cfg)
			mustContain(t, format, out, "Firmware enabled", "Enabled")
			mustNotContain(t, format, out, "Releasing", "leaving their track")
		}
	})
	t.Run("disabling reports the count, even when it is none", func(t *testing.T) {
		cfg := firmwareConfigSample()
		cfg.Status = "DISABLED"
		cfg.Message = "Firmware disabled; every device in a track is being released"
		for count, want := range map[int]string{0: "Releasing: 0 devices leaving their track", 1: "Releasing: 1 device leaving their track"} {
			cfg.ReleasedDevices = intRef(count)
			for _, format := range []string{"table", "simple", "detailed"} {
				mustContain(t, format, render(t, format, "FormatFirmwareConfig", cfg), want)
			}
		}
	})
	t.Run("showing the configuration reports no release count", func(t *testing.T) {
		cfg := firmwareConfigSample()
		cfg.Status = "DISABLED"
		for _, format := range []string{"table", "simple", "detailed"} {
			mustNotContain(t, format, render(t, format, "FormatFirmwareConfig", cfg), "Releasing")
		}
	})
}

// A device moved to unmanaged has no track while it leaves its old one, and a
// device that was never in a track has neither a track nor a state: no format
// prints an empty field for them.
func TestFirmwareAssignmentOfDevicesLeavingTheirTrack(t *testing.T) {
	leaving := &models.FirmwareAssignment{OrganizationName: "acme", Track: "unmanaged", Results: []models.FirmwareAssignmentResult{
		{DeviceName: "fw-a", Result: "released", State: "unenrolling", Notes: []string{}},
		{DeviceName: "fw-b", Result: "unchanged", Notes: []string{}},
	}}
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareAssignment", leaving)
			mustContain(t, format, out, "2 devices → unmanaged: 1 released, 1 unchanged", "fw-a", "fw-b", "unmanaged", "unenrolling")
			mustNotContain(t, format, out, "(,", ", )", "()")
		})
	}
	simple := render(t, "simple", "FormatFirmwareAssignment", leaving)
	mustContain(t, "simple", simple, "• fw-a: released (unmanaged, unenrolling)\n", "• fw-b: unchanged\n")
}

// tableHeaders returns the header row of every styled table in out.
func tableHeaders(out string) [][]string {
	var tables [][]string
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, BoxTopLeft) || i+1 == len(lines) {
			continue
		}
		var cells []string
		for _, cell := range strings.Split(lines[i+1], BoxVertical) {
			if cell = strings.TrimSpace(cell); cell != "" {
				cells = append(cells, cell)
			}
		}
		tables = append(tables, cells)
	}
	return tables
}

// The backup, schedule and task tables head their columns in Title Case, every
// word capitalised ("Last Backup", "Next Run"); the firmware tables follow.
func TestFirmwareTableHeadersAreTitleCase(t *testing.T) {
	titleCase := regexp.MustCompile(`^[A-Z][a-z]*( [A-Z][a-z]*)*$`)
	distinct := map[string]bool{}
	for method, args := range firmwareSamples() {
		for _, headers := range tableHeaders(render(t, "table", method, args...)) {
			distinct[strings.Join(headers, " | ")] = true
			for _, header := range headers {
				if !titleCase.MatchString(header) {
					t.Errorf("%s: the header %q is not Title Case ('Last Run', not 'LAST RUN' or 'Last run')", method, header)
				}
			}
		}
	}
	// Tracks, device assignment, devices, history, halts, the rollout board, its
	// failures and promotion: if one stopped being drawn, nothing was checked.
	if len(distinct) < 8 {
		t.Errorf("only %d distinct firmware tables were checked, want all 8: %v", len(distinct), distinct)
	}
}

func TestFirmwareTracksAndTrackRendering(t *testing.T) {
	cfg := firmwareConfigSample()
	for _, format := range []string{"table", "simple", "detailed"} {
		out := render(t, format, "FormatFirmwareTracks", cfg.Tracks)
		mustContain(t, format+" tracks", out, "fast", "regular", "stable")

		one := render(t, format, "FormatFirmwareTrack", &cfg.Tracks[1])
		mustContain(t, format+" track", one, "regular", "9 days", "previous track's evidence", "1 device of the previous track")
		mustNotContain(t, format+" track", one, "stable")
	}
	// The fast track has nothing before it, and says so.
	mustContain(t, "fast", render(t, "table", "FormatFirmwareTrack", &cfg.Tracks[0]),
		"counted from OPNsense publishing the release", "no previous track to wait for")

	for _, format := range []string{"table", "simple", "detailed"} {
		mustContain(t, format+" empty", render(t, format, "FormatFirmwareTracks", []models.FirmwareTrack{}), "No tracks found")
	}
	if got := render(t, "json", "FormatFirmwareTracks", []models.FirmwareTrack{}); !strings.Contains(got, `"total": 0`) {
		t.Errorf("an empty json list should say total 0:\n%s", got)
	}
}

func TestFirmwareDevicesRendering(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareDevices", firmwareDevicesSample())
			mustContain(t, format, out,
				"fw-a", "fw-b", "fw-c", "fw-d",
				"managed", "held", "unmanaged",
				"26.7.3", "26.7.2",
				"a custom firmware flavour is already set on the device", // held reason as a sentence
				"success", "failed",
				"agent too old for firmware tracks (1.22.1)",
				"the pin had been changed outside NetDefense",
				"released automatically", "no contact for 30 days",
			)
		})
	}
	// A failed step's message is a note in the compact formats (cut for a table
	// cell, whole in simple) and a field of its own in the box.
	long := firmwareDevicesSample()
	long.Items[2].LastError = "the update did not finish: " + strings.Repeat("é", 80)
	table := render(t, "table", "FormatFirmwareDevices", long)
	mustContain(t, "table", table, "last error: the update did not finish: ééé", "…")
	mustNotContain(t, "table", table, strings.Repeat("é", 80))
	mustContain(t, "simple", render(t, "simple", "FormatFirmwareDevices", long), "note: last error: the update did not finish: "+strings.Repeat("é", 80))
	mustContain(t, "table", render(t, "table", "FormatFirmwareDevices", firmwareDevicesSample()), "last error: the update did not finish")
	// The detailed view also carries what the others leave out.
	detailed := render(t, "detailed", "FormatFirmwareDevices", firmwareDevicesSample())
	mustContain(t, "detailed", detailed, "held_custom_flavour", "the update did not finish", "1 same-version reinstall entry")

	for _, format := range []string{"table", "simple", "detailed"} {
		mustContain(t, format+" empty", render(t, format, "FormatFirmwareDevices", &models.FirmwareDeviceList{}), "No devices found")
	}
}

func TestFirmwareRolloutRendering(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareRollout", firmwareRolloutSample())
			mustContain(t, format, out,
				"26.7", "26.7.4", "26.7.5", "26.7.3",
				"soaking", "3h 53m", // 14000 s, computed by the server and shown as given
				"waiting for evidence",
				"manual",
				"too few devices in the previous track can still reach it",
				"fw-b", "fw-e", // the failures
				"regression in the VPN stack", "admin@acme.com", // the halts
			)
		})
	}
	// The evidence reads n/min in the compact formats and in words in the boxes.
	mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", firmwareRolloutSample()), "0/1")
	mustContain(t, "simple", render(t, "simple", "FormatFirmwareRollout", firmwareRolloutSample()), "evidence 0/1")
	mustContain(t, "detailed", render(t, "detailed", "FormatFirmwareRollout", firmwareRolloutSample()), "0 of 1 devices of the previous track")
	// A track with no release in play still has its row.
	mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", firmwareRolloutSample()), "stable")

	t.Run("a release blocked by a failure points at the command that clears it", func(t *testing.T) {
		r := firmwareRolloutSample()
		cand := r.Tracks[1].Series[0].NextCandidate
		cand.State, cand.StallReason = "blocked_by_failure", "blocked_by_failure"
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareRollout", r), "blocked by a failure", "ndcli firmware failure clear")
		}
	})
	t.Run("no failures and no halts says so", func(t *testing.T) {
		r := firmwareRolloutSample()
		r.Failures, r.Halts = nil, nil
		r.PlanEnabled = false
		out := render(t, "table", "FormatFirmwareRollout", r)
		mustContain(t, "table", out, "Failures: none", "Halts: none", "not included")
	})
	// The board of an organization that lost the feature says what is happening
	// to its devices, in the words of the configuration view.
	t.Run("a plan without the feature says no step is sent and the devices are being released", func(t *testing.T) {
		r := firmwareRolloutSample()
		r.PlanEnabled = false
		for format, want := range map[string]string{
			"table":    "not included (no new steps are sent; devices still in a track are being released)",
			"detailed": "not included (no new steps are sent; devices still in a track are being released)",
			"simple":   "The plan does not include firmware tracks: no new steps are sent; devices still in a track are being released",
		} {
			out := render(t, format, "FormatFirmwareRollout", r)
			mustContain(t, format, out, want)
			mustNotContain(t, format, out, "nothing acts")
		}
	})
	t.Run("a plan with the feature adds no warning", func(t *testing.T) {
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareRollout", firmwareRolloutSample())
			mustContain(t, format, out, "acme-corp")
			mustNotContain(t, format, out, "not included", "does not include", "being released")
		}
	})
}

func TestFirmwareHistoryRendering(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareHistory", firmwareHistorySample())
			mustContain(t, format, out,
				"advance", "enroll", "success", "failed",
				"26.7.3 → 26.7.5", "26.7.3 (unchanged)",
				"the update was applied",
				"failure cleared", "admin@acme.com",
			)
		})
	}
	for _, format := range []string{"table", "simple", "detailed"} {
		mustContain(t, format+" empty", render(t, format, "FormatFirmwareHistory", &models.FirmwareHistory{DeviceName: "fw-z"}),
			"No firmware history for fw-z")
	}
}

func TestFirmwareWriteResultsRendering(t *testing.T) {
	samples := firmwareSamples()
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			assign := render(t, format, "FormatFirmwareAssignment", samples["FormatFirmwareAssignment"][0])
			// A long note wraps inside the detailed box, so the fragments stay short.
			mustContain(t, format+" assign", assign, "2 devices → fast track", "1 added", "1 moved", "fw-a", "fw-b",
				"enrolling", "agent too old for firmware tracks", "disabled", "1 pending firmware step cancelled")

			halt := render(t, format, "FormatFirmwareHalt", samples["FormatFirmwareHalt"][0])
			mustContain(t, format+" halt", halt, "Release 26.7.5 halted", "stay on it", "2 pending steps cancelled", "regression")

			lifted := firmwareHaltSample()
			lifted.UnhaltedAt, lifted.UnhaltedBy = "2026-10-09T12:00:00Z", "ops@acme.com"
			unhalt := render(t, format, "FormatFirmwareHalt", &models.FirmwareHaltResult{Halt: lifted})
			mustContain(t, format+" unhalt", unhalt, "Halt on release 26.7.5 lifted", "competes for the tracks again")
			mustNotContain(t, format+" unhalt", unhalt, "pending")

			promo := render(t, format, "FormatFirmwarePromotion", samples["FormatFirmwarePromotion"][0])
			mustContain(t, format+" promote", promo, "Release 26.7.5 approved in fast", "approved now", "already approved", "regular")

			again := render(t, format, "FormatFirmwarePromotion", &models.FirmwarePromotion{Version: "26.7.5",
				Results: []models.FirmwarePromotionResult{{Track: "fast", Result: "already_approved", Target: "26.7.5"}}})
			mustContain(t, format+" promote again", again, "was already approved in every track asked")

			cleared := render(t, format, "FormatFirmwareFailuresCleared", &models.FirmwareFailuresCleared{Version: "26.7.5", Cleared: 1})
			mustContain(t, format+" cleared", cleared, "Cleared 1 failure blocking release 26.7.5 in every track", "history")

			none := render(t, format, "FormatFirmwareFailuresCleared", &models.FirmwareFailuresCleared{Version: "26.7.5", Track: "stable"})
			mustContain(t, format+" cleared none", none, "No recorded failure was blocking release 26.7.5 in the stable track")

			deleted := render(t, format, "FormatFirmwareConfigDeleted", samples["FormatFirmwareConfigDeleted"][0])
			mustContain(t, format+" deleted", deleted, "Firmware configuration deleted", "acme-corp")

			halts := render(t, format, "FormatFirmwareHalts", samples["FormatFirmwareHalts"][0])
			mustContain(t, format+" halts", halts, "26.7.5", "in force", "lifted", "ops@acme.com")
		})
	}
}

func TestFirmwareHelpers(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "now"}, {0, "now"}, {20 * time.Second, "<1m"}, {90 * time.Second, "1m"},
		{47 * time.Minute, "47m"}, {5 * time.Hour, "5h"}, {5*time.Hour + 20*time.Minute, "5h 20m"},
		{24 * time.Hour, "1d"}, {52 * time.Hour, "2d 4h"}, {9 * 24 * time.Hour, "9d"},
	} {
		if got := firmwareDuration(tc.d); got != tc.want {
			t.Errorf("firmwareDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	if got := firmwareUntil("2026-10-10T16:00:00Z", now); got != "in 2d 4h" {
		t.Errorf("firmwareUntil = %q", got)
	}
	if got := firmwareUntil("2026-10-08T11:00:00Z", now); got != "due" {
		t.Errorf("a window already past should read due, got %q", got)
	}
	if got := firmwareUntil("not a time", now); got != "" {
		t.Errorf("an unparseable instant = %q, want nothing", got)
	}
	if got := firmwareTimestamp(""); got != "-" {
		t.Errorf("firmwareTimestamp(\"\") = %q", got)
	}
	if got := firmwareTimestamp("garbled"); got != "garbled" {
		t.Errorf("an unparseable timestamp should show as sent, got %q", got)
	}
	for _, tc := range []struct {
		from, to, want string
	}{
		{"26.7.3", "26.7.5", "26.7.3 → 26.7.5"}, {"26.7.5", "26.7.5", "26.7.5 (unchanged)"},
		{"", "26.7.5", "→ 26.7.5"}, {"26.7.3", "", "26.7.3"}, {"", "", "-"},
	} {
		if got := firmwareVersionMove(tc.from, tc.to); got != tc.want {
			t.Errorf("firmwareVersionMove(%q, %q) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
	counts := models.FirmwareDeviceCounts{Total: 4, Managed: 3, Held: 1}
	if got := firmwareDeviceCounts(counts); got != "4 (3 managed, 1 held)" {
		t.Errorf("firmwareDeviceCounts = %q", got)
	}
	if got := firmwareDevicesPhrase(counts); got != "4 devices (3 managed, 1 held)" {
		t.Errorf("firmwareDevicesPhrase = %q", got)
	}
	if got := firmwareDevicesPhrase(models.FirmwareDeviceCounts{Total: 1}); got != "1 device" {
		t.Errorf("firmwareDevicesPhrase(1) = %q", got)
	}
	if got := firmwareDeviceCounts(models.FirmwareDeviceCounts{}); got != "0" {
		t.Errorf("no devices = %q", got)
	}
	// A state, outcome or candidate the build has no word for shows as itself.
	if got := firmwareCandidateLabel("from_the_future"); got != "from_the_future" {
		t.Errorf("an unknown candidate state = %q", got)
	}
	if got := firmwareReasonFull("held_from_the_future"); got != "held_from_the_future" {
		t.Errorf("an unknown reason = %q", got)
	}
	if got := firmwareReasonFull("held_custom_flavour"); got != "a custom firmware flavour is already set on the device (held_custom_flavour)" {
		t.Errorf("a known reason = %q", got)
	}
}

// ── the FIRMWARE_TRACK task result ───────────────────────────────────────────

func firmwareTrackTask(message string) *models.Task {
	return &models.Task{
		ID: "abcd1234", Type: models.TaskTypeFirmwareTrack, Status: "COMPLETED", DeviceName: "fw-a",
		Organization: "acme", Message: message,
	}
}

const advanceApplied = `{"action":"advance","track":"regular","outcome":"success","reason":"applied","drift":false,` +
	`"reinstall_entries":0,"from_version":"26.7.3","to_version":"26.7.5","target_version":"26.7.5","pinned_version":"26.7.5"}`

const advanceHeld = `{"action":"advance","track":"fast","outcome":"held","reason":"held_reboot_pending","drift":true,` +
	`"reinstall_entries":1,"from_version":"26.7.3","to_version":"26.7.3"}`

func TestFormatTaskRendersTheFirmwareTrackResult(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatTask", firmwareTrackTask(advanceApplied))
			// The section is titled in the case each format sets for its
			// firmware sections; the lines are the same in all three.
			mustContain(t, format, strings.ToLower(out),
				"firmware track result",
				"advance on the regular track", "success", "the update was applied (applied)",
				"26.7.3 → 26.7.5", "target:", "pinned:", "drift:",
			)
			mustNotContain(t, format, out, `"outcome"`)

			held := render(t, format, "FormatTask", firmwareTrackTask(advanceHeld))
			mustContain(t, format+" held", held, "held", "a reboot is pending on the device (held_reboot_pending)",
				"26.7.3 (unchanged)", "yes: the pin had been changed outside NetDefense", "1 same-version reinstall entry")
		})
	}

	t.Run("a pending task shows the sentence the control plane wrote", func(t *testing.T) {
		task := firmwareTrackTask("Firmware track fast: advance")
		task.Status = "PENDING"
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatTask", task)
			mustContain(t, format, out, "Firmware track fast: advance")
			mustNotContain(t, format, strings.ToLower(out), "firmware track result")
		}
	})
	t.Run("a failed task shows the agent's error", func(t *testing.T) {
		task := firmwareTrackTask("firmware check failed: connection refused")
		task.Status = "FAILED"
		out := render(t, "table", "FormatTask", task)
		mustContain(t, "table", out, "firmware check failed: connection refused")
	})
	t.Run("another task type is left alone, whatever its message", func(t *testing.T) {
		task := firmwareTrackTask(advanceApplied)
		task.Type = models.TaskTypeSync
		out := render(t, "table", "FormatTask", task)
		mustNotContain(t, "table", strings.ToLower(out), "firmware track result")
	})
	t.Run("a FIRMWARE_UPGRADE task still renders as one", func(t *testing.T) {
		task := firmwareTrackTask(`{"resolved_mode":"minor","from_version":"26.1.2","to_version":"26.1.9","applied":true,"reboot_performed":true}`)
		task.Type = models.TaskTypeFirmwareUpgrade
		out := render(t, "table", "FormatTask", task)
		mustContain(t, "table", out, "Firmware upgrade result", "26.1.2 → 26.1.9")
	})
	t.Run("an escape sequence inside the result never reaches the terminal", func(t *testing.T) {
		const esc = `\u001b`
		hostile := `{"action":"advance","track":"fast","outcome":"held","reason":"held_` + esc + `[2Jbusy","drift":false,` +
			`"reinstall_entries":0,"from_version":"26.7` + esc + `]0;pwn\u0007.3"}`
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatTask", firmwareTrackTask(hostile))
			if hasControl(out) {
				t.Errorf("%s: a control sequence reached the output: %q", format, firstControlContext(out))
			}
		}
	})
	t.Run("json keeps the raw message and adds the parsed result", func(t *testing.T) {
		out := render(t, "json", "FormatTask", firmwareTrackTask(advanceApplied))
		var doc struct {
			models.Task
			Result map[string]interface{} `json:"firmware_track_result"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Message != advanceApplied || doc.ID != "abcd1234" {
			t.Fatalf("json task = %+v, %v; want the task as it came", doc, err)
		}
		for k, want := range map[string]interface{}{"action": "advance", "outcome": "success", "reason": "applied", "from_version": "26.7.3", "to_version": "26.7.5", "drift": false} {
			if doc.Result[k] != want {
				t.Errorf("firmware_track_result.%s = %v, want %v", k, doc.Result[k], want)
			}
		}
	})
	t.Run("json prints an empty list as it always did", func(t *testing.T) {
		if out := render(t, "json", "FormatTasks", []models.Task{}, 0); !strings.Contains(out, `"tasks": []`) {
			t.Errorf("an empty list should print []:\n%s", out)
		}
		if out := render(t, "json", "FormatTasks", []models.Task(nil), 0); !strings.Contains(out, `"tasks": null`) {
			t.Errorf("a nil list should print null, as before:\n%s", out)
		}
	})
	t.Run("json adds nothing to any other task", func(t *testing.T) {
		for name, task := range map[string]*models.Task{
			"another type": {ID: "abcd1234", Type: models.TaskTypeSync, Status: "COMPLETED", Message: advanceApplied},
			"pending":      firmwareTrackTask("Firmware track fast: advance"),
		} {
			out := render(t, "json", "FormatTask", task)
			if strings.Contains(out, "firmware_track_result") {
				t.Errorf("%s: json should carry no firmware_track_result:\n%s", name, out)
			}
		}
	})
}

func TestFormatTasksSummarisesFirmwareTrackResults(t *testing.T) {
	applied := firmwareTrackTask(advanceApplied)
	applied.ID = "aaaaaaaa1111"
	held := firmwareTrackTask(advanceHeld)
	held.ID = "bbbbbbbb2222"
	enrolled := firmwareTrackTask(`{"action":"enroll","track":"fast","outcome":"success","reason":"enrolled","drift":false,` +
		`"reinstall_entries":0,"from_version":"26.7.3","to_version":"26.7.3","pinned_version":"26.7.3"}`)
	enrolled.ID = "cccccccc3333"
	pending := firmwareTrackTask("Firmware track fast: advance")
	pending.ID, pending.Status = "dddddddd4444", "PENDING"
	ping := &models.Task{ID: "eeeeeeee5555", Type: models.TaskTypePing, Status: "COMPLETED", DeviceName: "fw-b", Message: "pong"}
	tasks := []models.Task{*applied, *held, *enrolled, *pending, *ping}

	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatTasks", tasks, 5)
			mustContain(t, format, out,
				"advance success 26.7.3 → 26.7.5",
				"advance held 26.7.3 (unchanged) — a reboot is pending on the device (pin drift)",
				"enroll success, pinned 26.7.3",
			)
		})
	}

	t.Run("the table grows a Result column only when a page has a result", func(t *testing.T) {
		with := render(t, "table", "FormatTasks", tasks, 5)
		mustContain(t, "with", with, "Result")
		without := render(t, "table", "FormatTasks", []models.Task{*ping, *pending}, 2)
		mustNotContain(t, "without", without, "Result")
		mustContain(t, "without", without, "PING", "FIRMWARE_TRACK")
	})
	t.Run("an escape sequence inside a listed result never reaches the terminal", func(t *testing.T) {
		const esc = `\u001b`
		hostile := firmwareTrackTask(`{"action":"advance","track":"fast","outcome":"held","reason":"held_` + esc + `[2Jbusy","drift":false,` +
			`"reinstall_entries":0,"from_version":"26.7` + esc + `]0;pwn\u0007.3"}`)
		hostile.ID = "ffffffff6666"
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatTasks", []models.Task{*hostile}, 1)
			if hasControl(out) {
				t.Errorf("%s: a control sequence reached the output: %q", format, firstControlContext(out))
			}
			mustContain(t, format, out, "advance held")
		}
	})
	t.Run("json lists the tasks as they came, with the results parsed", func(t *testing.T) {
		out := render(t, "json", "FormatTasks", tasks, 5)
		var doc struct {
			Tasks []struct {
				models.Task
				Result map[string]interface{} `json:"firmware_track_result"`
			} `json:"tasks"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Tasks) != 5 || doc.Tasks[0].Message != advanceApplied || doc.Total != 5 {
			t.Fatalf("json = %+v, %v", doc, err)
		}
		withResult := 0
		for _, task := range doc.Tasks {
			if task.Result != nil {
				withResult++
			}
		}
		if withResult != 3 {
			t.Errorf("%d tasks carry a parsed result, want the 3 completed FIRMWARE_TRACK ones", withResult)
		}
	})
}

func TestFirmwareTrackResultLines(t *testing.T) {
	if lines := firmwareTrackResultLines(nil); lines != nil {
		t.Errorf("no result, no lines: got %v", lines)
	}
	r := models.ParseFirmwareTrackResult(advanceApplied)
	got := strings.Join(firmwareTrackResultLines(r), "\n")
	want := strings.Join([]string{
		"Action:    advance on the regular track",
		"Outcome:   success",
		"Reason:    the update was applied (applied)",
		"Version:   26.7.3 → 26.7.5",
		"Target:    26.7.5",
		"Pinned:    26.7.5",
		"Drift:     no",
	}, "\n")
	if got != want {
		t.Errorf("lines:\n%s\nwant:\n%s", got, want)
	}
}
