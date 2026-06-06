package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/resources"
)

func testApp(t *testing.T) *App {
	t.Helper()
	provider := auth.NewStaticTokenProvider("ndpat_test")
	client := api.NewClient("https://example.invalid", false, provider)
	svc := service.New(client, nil, config.Get())
	reg := registry.New()
	resources.RegisterAll(reg)
	ctx := &AppContext{Svc: svc, Cfg: config.Get(), Org: "test-org", Account: "tester@example.com", Reg: reg, Refresh: defaultRefresh}
	return newApp(ctx)
}

func ptrI64(v int64) *int64 { return &v }
func ptrInt(v int) *int     { return &v }
func ptrBool(v bool) *bool  { return &v }

func sampleDashboard() *models.DashboardResponse {
	return &models.DashboardResponse{
		AsOf:          1_700_000_000,
		Devices:       models.DashboardDeviceCounters{Total: 3, Online: 1, Stale: 1, Offline: 1},
		Sync:          models.DashboardSyncCounters{InSync: 1, Drift: 1, Error: 1},
		Tasks24h:      models.DashboardTaskCounters{Completed: 5, Failed: 1},
		AgentVersions: []models.DashboardAgentVersion{{Version: "1.2.3", Count: 2}, {Version: "1.2.2", Count: 1}},
		Compact: []models.DashboardCompactRow{
			{Name: "fw-a", StatusColor: "offline", Sync: models.DashboardCompactSync{State: "error"}, HeartbeatAgeSec: ptrI64(400)},
			{Name: "fw-b", StatusColor: "stale", Sync: models.DashboardCompactSync{State: "drift", AgeSec: ptrI64(120)},
				Telemetry: &models.DashboardCompactTelemetry{HeavySummary: &models.DashboardHeavySummary{PendingUpdates: ptrInt(3)}}},
			{Name: "fw-c", Sync: models.DashboardCompactSync{State: "ok"}, AgentVersion: "1.2.3", Online: ptrBool(true)},
		},
	}
}

func sampleTelemetry() *models.DeviceTelemetryResponse {
	return &models.DeviceTelemetryResponse{
		Name: "fw-a", Status: "ENABLED", Online: ptrBool(true), AgentVersion: "1.2.3",
		HeartbeatAgeSec: ptrI64(12), Sync: models.DeviceTelemetrySync{State: "drift", AgeSec: ptrI64(60)},
		Snapshot: &models.TelemetrySnapshot{
			UptimeSec: 86400, CPUCount: 4, Load1: 0.5, MemUsedPct: 88, MemTotalKB: 8 * 1024 * 1024,
			Disks: []models.TelemetryDisk{{Mountpoint: "/", UsedPct: 42, UsedKB: 1000, TotalKB: 4000}},
			Heavy: &models.TelemetryHeavy{
				Services: &models.TelemetryServicesBlock{Items: []models.TelemetryService{{Name: "unbound", Running: false}}},
				Updates:  &models.TelemetryUpdates{Status: "ok", UpgradeCount: 2, NeedsReboot: true},
				Certs:    &models.TelemetryCertsBlock{Items: []models.TelemetryCert{{Description: "WebGUI", DaysLeft: -1}, {Description: "VPN", DaysLeft: 10, InUse: true}}},
			},
		},
	}
}

// TestRenderPathsDoNotPanic exercises every screen and overlay render path.
func TestRenderPathsDoNotPanic(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})

	// Dashboard: loading then populated.
	if out := a.View(); !strings.Contains(out, "Organization:") {
		t.Fatalf("header missing from view: %q", firstLine(out))
	}
	a.Update(dashLoadedMsg{data: sampleDashboard()})
	mustRender(t, a, "dashboard")

	// Drill into the top fleet row (device health).
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// pushScreenMsg is produced as a command; simulate the push directly.
	a.stack = append(a.stack, newHealthScreen(a.ctx, "fw-a"))
	a.resizeAll()
	a.Update(healthLoadedMsg{device: "fw-a", data: sampleTelemetry()})
	mustRender(t, a, "health")

	// Pop back and open a generic list.
	a.pop()
	res, ok := a.ctx.Reg.Get("device")
	if !ok {
		t.Fatal("device resource not registered")
	}
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	a.Update(listLoadedMsg{kind: "device", page: 1, total: 1, rows: []registry.Row{
		{ID: "fw-a", Cells: []string{"fw-a", "ENABLED", "online", "ou1", "1.2.3", "3s", "IN_SYNC"}},
	}})
	mustRender(t, a, "device list")

	// Filter mode.
	ls.filtering = true
	ls.filter = "fw"
	mustRender(t, a, "filtered list")
	ls.filtering = false

	// Overlays.
	a.confirm = newConfirm(registry.Action{Key: "x", Label: "remove", Destructive: true, Prompt: "Remove {id}?"}, "fw-a")
	mustRender(t, a, "confirm")
	a.confirm = newConfirm(registry.Action{Key: "A", Label: "approve-all", TargetsAll: true, Destructive: true, BlastRadius: "all pending"}, "")
	mustRender(t, a, "danger confirm")
	a.confirm = nil

	a.palette = newPalette(a.ctx.Reg)
	mustRender(t, a, "palette")
	a.palette = nil

	a.orgsw = newOrgSwitcher()
	a.orgsw.setOrgs([]string{"alpha", "beta"})
	mustRender(t, a, "org switcher")
	a.orgsw = nil

	a.showHelp = true
	mustRender(t, a, "help")
	a.showHelp = false

	// Detail screen for a Describer resource.
	taskRes, _ := a.ctx.Reg.Get("task")
	d := newDetailScreen(a.ctx, taskRes, "abc123")
	d.SetSize(a.contentWidth(), a.contentHeight())
	d.sections = []registry.Section{{Title: "Task", Fields: []registry.Field{{Label: "ID", Value: "abc123"}}}, {Title: "Message", Text: "hello"}}
	a.stack = append(a.stack, d)
	mustRender(t, a, "detail")
}

func TestChromeRendersBannerAndBox(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(dashLoadedMsg{data: sampleDashboard()})
	out := a.View()
	if !strings.Contains(out, "╭─ Dashboard") {
		t.Error("content box titled border missing")
	}
	if !strings.Contains(out, "▙ ▌") { // first glyphs of the NetDefense banner
		t.Error("banner missing from the header")
	}
	if !strings.Contains(out, "device") { // footer status line (N devices in fleet)
		t.Error("footer status line missing")
	}
}

func TestTaskJSONResultRendersAsFields(t *testing.T) {
	// The detail renderer must split a JSON-derived multi-field/text section
	// into individual lines (so scroll counting and display are correct).
	secs := []registry.Section{
		{Title: "Result", Fields: []registry.Field{{Label: "Applied", Value: "true"}, {Label: "Mode", Value: "major"}}},
	}
	lines := renderSections(secs)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Applied") || !strings.Contains(joined, "major") {
		t.Fatalf("expected field labels/values in rendered detail, got:\n%s", joined)
	}
}

func TestConfirmTypeToConfirm(t *testing.T) {
	// Enter before typing "yes" must cancel, not confirm.
	if done, confirmed := newConfirm(registry.Action{BlastRadius: "x"}, "").handleKey("enter"); !done || confirmed {
		t.Fatalf("empty type-to-confirm should cancel, got done=%v confirmed=%v", done, confirmed)
	}

	c := newConfirm(registry.Action{Key: "A", BlastRadius: "fleet-wide"}, "")
	// A single key only appends to the buffer; it must not complete.
	if done, _ := c.handleKey("y"); done {
		t.Fatal("type-to-confirm should not complete on a single key")
	}
	c.handleKey("e")
	c.handleKey("s")
	done, confirmed := c.handleKey("enter")
	if !done || !confirmed {
		t.Fatalf("expected confirmed after typing yes, got done=%v confirmed=%v", done, confirmed)
	}
}

func TestPaletteQuit(t *testing.T) {
	a := testApp(t)
	p := newPalette(a.ctx.Reg)
	for _, r := range "quit" {
		p.handleKey(string(r))
	}
	done, kind := p.handleKey("enter")
	if !done || kind != paletteQuitKind {
		t.Fatalf("expected palette to resolve to quit, got done=%v kind=%q", done, kind)
	}
}

func TestConfirmStandard(t *testing.T) {
	c := newConfirm(registry.Action{Key: "x", Label: "remove"}, "fw-a")
	if done, confirmed := c.handleKey("n"); !done || confirmed {
		t.Fatalf("n should cancel: done=%v confirmed=%v", done, confirmed)
	}
	if done, confirmed := c.handleKey("y"); !done || !confirmed {
		t.Fatalf("y should confirm: done=%v confirmed=%v", done, confirmed)
	}
}

func TestFormValidationAndSubmit(t *testing.T) {
	act := registry.Action{Key: "p", Label: "ping", Form: []registry.FormField{
		{Key: "host", Label: "Host", Required: true},
		{Key: "count", Label: "Count", Default: "4"},
	}}
	f := newForm(act, "fw-a")
	if f.values[1] != "4" {
		t.Fatalf("count default = %q, want 4", f.values[1])
	}
	if done, _, _ := f.handleKey("enter"); done {
		t.Fatal("must not submit while a required field is empty")
	}
	for _, r := range "1.1.1.1" {
		f.handleKey(string(r))
	}
	done, submit, args := f.handleKey("enter")
	if !done || !submit {
		t.Fatalf("expected submit, got done=%v submit=%v", done, submit)
	}
	if args["host"] != "1.1.1.1" || args["count"] != "4" {
		t.Fatalf("args = %v", args)
	}
}

func TestFormSelectCycleAndTypingIgnored(t *testing.T) {
	act := registry.Action{Key: "f", Form: []registry.FormField{
		{Key: "mode", Label: "Mode", Options: []string{"minor", "major"}},
	}}
	f := newForm(act, "fw-a")
	if f.values[0] != "minor" {
		t.Fatalf("mode default = %q, want minor", f.values[0])
	}
	f.handleKey("right")
	if f.values[0] != "major" {
		t.Fatalf("after right, mode = %q, want major", f.values[0])
	}
	f.handleKey("right") // wraps back to first option
	if f.values[0] != "minor" {
		t.Fatalf("after wrap, mode = %q, want minor", f.values[0])
	}
	f.handleKey("z") // typing into a select field is ignored
	if f.values[0] != "minor" {
		t.Fatalf("select must ignore typed runes, got %q", f.values[0])
	}
}

func TestActionAlwaysConfirms(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	res, _ := a.ctx.Reg.Get("device")
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	a.Update(listLoadedMsg{kind: "device", page: 1, total: 1, rows: []registry.Row{
		{ID: "fw-a", Cells: []string{"fw-a", "PENDING", "online", "ou", "1.0", "3s", "—"}},
	}})
	// "a" approve is non-destructive but must still open the confirm modal so
	// nothing mutates by an accidental keypress.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if a.confirm == nil {
		t.Fatal("approve must open a confirmation modal")
	}
	if a.confirm.act.Key != "a" {
		t.Fatalf("confirm action = %q, want a", a.confirm.act.Key)
	}
}

func TestDeviceFormAndShellDispatch(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	res, _ := a.ctx.Reg.Get("device")
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	a.Update(listLoadedMsg{kind: "device", page: 1, total: 1, rows: []registry.Row{
		{ID: "fw-a", Cells: []string{"fw-a", "ENABLED", "online", "ou1", "1.2.3", "3s", "IN_SYNC"}},
	}})

	// "p" (ping) opens the form overlay.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if a.form == nil {
		t.Fatal("ping should open the form overlay")
	}
	if len(a.form.act.Form) != 2 {
		t.Fatalf("ping form fields = %d, want 2", len(a.form.act.Form))
	}
	mustRender(t, a, "ping form")
	a.form = nil

	// "c" (connect) is a Shell action: returns an ExecProcess cmd, opens no overlay.
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if a.form != nil || a.confirm != nil {
		t.Fatal("connect (Shell) must not open the form or confirm overlay")
	}
	if cmd == nil {
		t.Fatal("connect should return an ExecProcess command")
	}
}

func TestNavDispatchPushesChildScreen(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	res, ok := a.ctx.Reg.Get("network")
	if !ok {
		t.Fatal("network resource not registered")
	}
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	a.Update(listLoadedMsg{kind: "network", page: 1, total: 1, rows: []registry.Row{
		{ID: "vpn-a", Cells: []string{"vpn-a", "10.0.0.0/24", "auto", "2", "0"}},
	}})
	before := len(a.stack)
	// "m" (members) is a Nav action — it must push a child list screen.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if len(a.stack) != before+1 {
		t.Fatalf("Nav action should push a child screen: stack %d -> %d", before, len(a.stack))
	}
	if got := a.active().Title(); got != "Members" {
		t.Fatalf("child screen title = %q, want Members", got)
	}
}

func TestNavRequiresSelection(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	res, _ := a.ctx.Reg.Get("network")
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	// No rows loaded -> selectedID() == "".
	before := len(a.stack)
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if len(a.stack) != before {
		t.Fatal("Nav with no selection must not push a screen")
	}
	if a.toastText == "" || !a.toastErr {
		t.Fatal("Nav with no selection should set an error toast")
	}
}

// TestDynamicFormStalenessGuard locks in the fix for the review-found bug: a
// dynamic-options result that resolves after the user navigated to a different
// resource (or switched org) must be dropped, never opened over — and then
// dispatched against — the now-active screen.
func TestDynamicFormStalenessGuard(t *testing.T) {
	a := testApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	res, _ := a.ctx.Reg.Get("ou")
	ls := newListScreen(a.ctx, res)
	ls.SetSize(a.contentWidth(), a.contentHeight())
	a.stack = append(a.stack, ls)
	act := registry.Action{Key: "D", Label: "remove-device", Form: []registry.FormField{
		{Key: "device", Label: "Device", Options: []string{"fw-a", "fw-b"}},
	}}
	a.Update(formReadyMsg{kind: "template", org: a.ctx.Org, act: act, target: "ou-1"})
	if a.form != nil {
		t.Fatal("stale-kind formReadyMsg must be dropped")
	}
	a.Update(formReadyMsg{kind: "ou", org: "other-org", act: act, target: "ou-1"})
	if a.form != nil {
		t.Fatal("stale-org formReadyMsg must be dropped")
	}
	a.Update(formReadyMsg{kind: "ou", org: a.ctx.Org, act: act, target: "ou-1"})
	if a.form == nil {
		t.Fatal("matching formReadyMsg should open the form")
	}
}

// TestResourceActionKeysSane checks every resource (registered + Nav-only
// children) for duplicate action keys and use of keys reserved by the global
// handler or the list-screen navigation.
func TestResourceActionKeysSane(t *testing.T) {
	reserved := map[string]bool{
		"q": true, "?": true, ":": true, "o": true, "r": true,
		"j": true, "k": true, "g": true, "G": true,
		"/": true, "[": true, "]": true,
		"esc": true, "backspace": true, "enter": true, "up": true, "down": true,
	}
	all := []registry.Resource{
		resources.NetworkMemberResource{}, resources.NetworkLinkResource{},
		resources.NetworkPrefixResource{}, resources.ScheduledTaskResource{},
	}
	reg := registry.New()
	resources.RegisterAll(reg)
	all = append(all, reg.All()...)
	for _, res := range all {
		seen := map[string]bool{}
		for _, ac := range res.Actions() {
			if reserved[ac.Key] {
				t.Errorf("%s: action %q uses reserved key %q", res.Kind(), ac.Label, ac.Key)
			}
			if seen[ac.Key] {
				t.Errorf("%s: duplicate action key %q", res.Kind(), ac.Key)
			}
			seen[ac.Key] = true
		}
	}
}

func TestBackupConfigIsOrgScoped(t *testing.T) {
	reg := registry.New()
	resources.RegisterAll(reg)
	res, _ := reg.Get("backup")
	for _, ac := range res.Actions() {
		if ac.Key == "c" {
			if !ac.TargetsAll {
				t.Fatal("backup config (c) must be TargetsAll so it does not require a device row")
			}
			return
		}
	}
	t.Fatal("backup config action (c) not found")
}

func mustRender(t *testing.T, a *App, label string) {
	t.Helper()
	out := a.View()
	if out == "" {
		t.Fatalf("%s: empty view", label)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
