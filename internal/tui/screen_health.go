package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/resources"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// vpnMembership is the device's membership in one VPN network.
type vpnMembership struct {
	network   string
	overlayIP string
	role      string
}

// deviceAssoc holds the config resources associated with a device, fetched when
// the device page opens (and reloaded when it regains focus).
type deviceAssoc struct {
	ous       []string
	templates []string
	variables []models.Variable
	vpn       []vpnMembership
}

// healthScreen is the per-device page: a summary header, the device's health
// telemetry laid out in compact boxes (resources, disks, services, updates,
// certs), the device's associated resources (OUs, templates, variables), and
// the full set of device actions (connect, sync, ...).
type healthScreen struct {
	ctx    *AppContext
	device string
	data   *models.DeviceTelemetryResponse
	assoc  *deviceAssoc
	scroll int
	w, h   int
	err    string
}

func newHealthScreen(ctx *AppContext, device string) *healthScreen {
	return &healthScreen{ctx: ctx, device: device}
}

func (s *healthScreen) Title() string    { return "Device · " + s.device }
func (s *healthScreen) SetSize(w, h int) { s.w, s.h = w, h }
func (s *healthScreen) Init() tea.Cmd    { return tea.Batch(s.Refresh(), s.loadAssoc()) }

// actionable — the device page exposes the same device commands as the list
// (connect, sync, restart, …) against this device.
func (s *healthScreen) actions() []registry.Action {
	if r, ok := s.ctx.Reg.Get("device"); ok {
		return r.Actions()
	}
	return nil
}

func (s *healthScreen) selectedID() string { return s.device }

func (s *healthScreen) resource() registry.Resource {
	r, _ := s.ctx.Reg.Get("device")
	return r
}

// loadAssoc fetches the device's OUs, the templates applied via those OUs, its
// device-scoped variable overrides, and its VPN memberships. Run on open and
// when the page regains focus.
func (s *healthScreen) loadAssoc() tea.Cmd {
	ctx, device := s.ctx, s.device
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		assoc := &deviceAssoc{}

		if dev, err := ctx.Svc.DeviceGet(c, ctx.Org, device); err == nil {
			assoc.ous = dev.OrganizationalUnits
			seen := map[string]bool{}
			for _, ou := range dev.OrganizationalUnits {
				res, e := ctx.Svc.OUTemplateList(c, ctx.Org, ou)
				if e != nil {
					continue
				}
				for _, t := range res.Items {
					if !seen[t.Name] {
						seen[t.Name] = true
						assoc.templates = append(assoc.templates, t.Name)
					}
				}
			}
		}

		// Variable overrides: the device-scope variable list (per_page capped at
		// 100 by NDManager).
		if vres, e := ctx.Svc.VariableList(c, service.VarScopeDevice, ctx.Org, device, service.VariableListOpts{PerPage: 100}); e == nil {
			assoc.variables = vres.Variables
		}

		// VPN memberships: probe each network for this device.
		if nets, e := ctx.Svc.NetworkList(c, ctx.Org, 1, 200); e == nil {
			for i, n := range nets.Networks {
				if i >= 50 {
					break
				}
				if m, e2 := ctx.Svc.NetworkMemberGet(c, ctx.Org, n.Name, device); e2 == nil && m != nil {
					assoc.vpn = append(assoc.vpn, vpnMembership{
						network: n.Name, overlayIP: m.OverlayIPv4, role: m.Role,
					})
				}
			}
		}

		return assocLoadedMsg{device: device, assoc: assoc}
	}
}

// onReveal reloads associations when the device page regains focus, so overrides
// edited in the child Variable Overrides screen show up.
func (s *healthScreen) onReveal() tea.Cmd { return s.loadAssoc() }

func (s *healthScreen) Refresh() tea.Cmd {
	ctx, device := s.ctx, s.device
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := ctx.Svc.DeviceHealth(c, ctx.Org, device)
		if err != nil {
			return errMsg{context: "health:" + device, err: err}
		}
		return healthLoadedMsg{device: device, data: d}
	}
}

func (s *healthScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case healthLoadedMsg:
		if msg.device == s.device {
			s.data = msg.data
			s.err = ""
		}
	case assocLoadedMsg:
		if msg.device == s.device {
			s.assoc = msg.assoc
		}
	case errMsg:
		if msg.context == "health:"+s.device {
			s.err = msg.err.Error()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if s.scroll > 0 {
				s.scroll--
			}
		case "down", "j":
			s.scroll++
		case "pgdown", "]":
			s.scroll += s.pageStep()
		case "pgup", "[":
			s.scroll -= s.pageStep()
			if s.scroll < 0 {
				s.scroll = 0
			}
		case "g", "home":
			s.scroll = 0
		case "v":
			return s, pushScreen(newListScreen(s.ctx, resources.ScopedVarResource{
				Scope: service.VarScopeDevice, Entity: s.device, Name: "Variable Overrides", KindID: "device-variable",
			}))
		}
	}
	return s, nil
}

func (s *healthScreen) pageStep() int {
	if s.h > 3 {
		return s.h - 2
	}
	return 1
}

func (s *healthScreen) View() string {
	if s.err != "" {
		return errStyle.Render("error: " + s.err)
	}
	if s.data == nil {
		return mutedStyle.Render("loading device…")
	}
	lines := strings.Split(s.render(), "\n")
	if s.scroll > len(lines)-1 {
		s.scroll = len(lines) - 1
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	end := s.scroll + s.h
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[s.scroll:end], "\n")
}

// render lays out the device page like the NDWeb device view: a summary header,
// then full-width rows of boxes — telemetry (resources / disks / updates),
// OUs / services / certificates, and templates / variable overrides.
func (s *healthScreen) render() string {
	d := s.data
	w := s.w
	var heavy *models.TelemetryHeavy
	if d.Snapshot != nil {
		heavy = d.Snapshot.Heavy
	}

	parts := []string{s.summary(), ""}
	if d.Snapshot == nil {
		parts = append(parts, mutedStyle.Render("No recent telemetry."))
	} else {
		parts = append(parts, flexRow(w, 2, []boxSpec{
			{"Resources", resourcesBody(d.Snapshot)},
			{"Disks", disksBody(d.Snapshot)},
			{"Updates", updatesBody(heavy)},
		}))
	}
	parts = append(parts, "", flexRow(w, 2, []boxSpec{
		{"Organizational Units", s.ousBody()},
		{"Services", servicesBody(heavy)},
		{"Certificates", certsBody(heavy)},
	}))
	parts = append(parts, "", flexRow(w, 2, []boxSpec{
		{"Templates", s.templatesBody()},
		{"Variable Overrides", s.variablesBody()},
	}))
	parts = append(parts, "", widthBox("VPN Networks", s.vpnBody(), w-4))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (s *healthScreen) summary() string {
	d := s.data
	l1 := statusStyle(d.Status).Render(d.Status) + "  " + mutedStyle.Render(uihelp.OnlineLabel(d.Online)) +
		mutedStyle.Render(fmt.Sprintf("   agent %s · heartbeat %s ago · snapshot %s ago",
			uihelp.Default(d.AgentVersion, "—"),
			uihelp.HumanDurationPtr(d.HeartbeatAgeSec),
			uihelp.HumanDurationPtr(d.SnapshotAgeSec)))
	l2 := mutedStyle.Render("sync ") + statusStyle(d.Sync.State).Render(d.Sync.State)
	if d.Sync.AgeSec != nil {
		l2 += mutedStyle.Render(" · " + uihelp.HumanDuration(*d.Sync.AgeSec) + " ago")
	}
	return l1 + "\n" + l2
}

func resourcesBody(sn *models.TelemetrySnapshot) []string {
	body := []string{
		fmt.Sprintf("uptime %s · %d CPU", uihelp.HumanDuration(int64(sn.UptimeSec)), sn.CPUCount),
		fmt.Sprintf("load %.2f / %.2f / %.2f", sn.Load1, sn.Load5, sn.Load15),
		"mem  " + meter(sn.MemUsedPct) + fmt.Sprintf(" %.0f%% of %s", sn.MemUsedPct, uihelp.HumanBytesKB(sn.MemTotalKB)),
	}
	if sn.SwapTotalKB > 0 {
		body = append(body, "swap "+meter(sn.SwapUsedPct)+fmt.Sprintf(" %.0f%%", sn.SwapUsedPct))
	}
	return body
}

func disksBody(sn *models.TelemetrySnapshot) []string {
	if len(sn.Disks) == 0 {
		return []string{mutedStyle.Render("—")}
	}
	body := make([]string, 0, len(sn.Disks))
	for _, dk := range sn.Disks {
		body = append(body, fmt.Sprintf("%-10s %s %3.0f%%", uihelp.Truncate(dk.Mountpoint, 10), meter(dk.UsedPct), dk.UsedPct))
	}
	return body
}

// updatesBody is the friendlier pending-updates summary: a status word, a
// package count, and the reboot flag.
func updatesBody(h *models.TelemetryHeavy) []string {
	if h == nil || h.Updates == nil {
		return []string{mutedStyle.Render("—")}
	}
	u := h.Updates
	pending := u.UpgradeCount + u.NewCount
	body := []string{okStyle.Render("Up-to-date")}
	if pending > 0 {
		body[0] = warnStyle.Render("Updates available")
	}
	body = append(body, fmt.Sprintf("Packages: %d", pending))
	if u.NeedsReboot {
		body = append(body, warnStyle.Render("Reboot required"))
	}
	return body
}

func servicesBody(h *models.TelemetryHeavy) []string {
	if h == nil || h.Services == nil {
		return []string{mutedStyle.Render("—")}
	}
	run, down := uihelp.SplitServices(h.Services.Items)
	body := []string{mutedStyle.Render(fmt.Sprintf("%d running / %d down", len(run), len(down)))}
	for i, ds := range down {
		if i >= 8 {
			body = append(body, mutedStyle.Render(fmt.Sprintf("+%d more", len(down)-8)))
			break
		}
		body = append(body, errStyle.Render("✗ ")+ds.Name)
	}
	return body
}

func certsBody(h *models.TelemetryHeavy) []string {
	if h == nil || h.Certs == nil || len(h.Certs.Items) == 0 {
		return []string{mutedStyle.Render("—")}
	}
	body := make([]string, 0)
	for i, c := range h.Certs.Items {
		if i >= 8 {
			body = append(body, mutedStyle.Render(fmt.Sprintf("+%d more", len(h.Certs.Items)-8)))
			break
		}
		tag := fmt.Sprintf("%dd", c.DaysLeft)
		style := mutedStyle
		switch {
		case c.DaysLeft <= 0:
			tag, style = "expired", errStyle
		case c.DaysLeft <= 30:
			style = warnStyle
		}
		body = append(body, style.Render(fmt.Sprintf("%-8s", tag))+" "+c.Description)
	}
	return body
}

func (s *healthScreen) ousBody() []string {
	if s.assoc == nil {
		return []string{mutedStyle.Render("loading…")}
	}
	return bulletsOrDash(s.assoc.ous)
}

func (s *healthScreen) templatesBody() []string {
	if s.assoc == nil {
		return []string{mutedStyle.Render("loading…")}
	}
	return bulletsOrDash(s.assoc.templates)
}

func (s *healthScreen) variablesBody() []string {
	if s.assoc == nil {
		return []string{mutedStyle.Render("loading…")}
	}
	if len(s.assoc.variables) == 0 {
		return []string{mutedStyle.Render("no device overrides")}
	}
	body := make([]string, 0, len(s.assoc.variables))
	for _, v := range s.assoc.variables {
		val := v.Value
		if v.Secret {
			val = "••••••"
		}
		body = append(body, fmt.Sprintf("%s = %s", v.Name, val))
	}
	return body
}

func (s *healthScreen) vpnBody() []string {
	if s.assoc == nil {
		return []string{mutedStyle.Render("loading…")}
	}
	if len(s.assoc.vpn) == 0 {
		return []string{mutedStyle.Render("not a member of any VPN network")}
	}
	body := make([]string, 0, len(s.assoc.vpn))
	for _, m := range s.assoc.vpn {
		body = append(body, fmt.Sprintf("%-26s %-18s %s",
			uihelp.Truncate(m.network, 26), uihelp.Default(m.overlayIP, "—"), mutedStyle.Render(m.role)))
	}
	return body
}

func bulletsOrDash(items []string) []string {
	if len(items) == 0 {
		return []string{mutedStyle.Render("—")}
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, "• "+it)
	}
	return out
}

// meter renders a tiny 10-cell usage bar coloured by severity.
func meter(pct float64) string {
	const cells = 10
	filled := int(pct/100*cells + 0.5)
	if filled > cells {
		filled = cells
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
	style := okStyle
	switch {
	case pct >= 90:
		style = errStyle
	case pct >= 75:
		style = warnStyle
	}
	return style.Render(bar)
}
