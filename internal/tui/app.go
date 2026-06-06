package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

const (
	headerHeight = 4 // banner / context block
	footerHeight = 4 // "Keys" box: border + 2 wrapped command lines + border
	boxBorders   = 2 // content box top + bottom border rows
	boxChrome    = 4 // content box: 2 border cols + 2 padding cols
)

// App is the root Bubble Tea model. It owns global concerns — the screen
// back-stack, overlays (confirm/palette/org-switcher/help), the refresh tick,
// the header/footer chrome and toast/status — and delegates content to the
// active screen.
type App struct {
	ctx    *AppContext
	stack  []screen
	width  int
	height int

	confirm  *confirmModel
	palette  *paletteModel
	orgsw    *orgSwitcherModel
	form     *formModel
	showHelp bool

	toastText string
	toastErr  bool
	toastSeq  int

	status      string
	lastUpdated time.Time
}

func newApp(ctx *AppContext) *App {
	return &App{ctx: ctx, stack: []screen{newDashboardScreen(ctx)}}
}

func (a *App) active() screen { return a.stack[len(a.stack)-1] }

func (a *App) contentWidth() int {
	w := a.width - boxChrome
	if w < 1 {
		w = 1
	}
	return w
}

func (a *App) contentHeight() int {
	h := a.height - headerHeight - footerHeight - boxBorders
	if h < 1 {
		h = 1
	}
	return h
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.active().Init(), tickCmd(a.ctx.Refresh))
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.resizeAll()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tickMsg:
		return a, tea.Batch(a.active().Refresh(), tickCmd(a.ctx.Refresh))

	case pushScreenMsg:
		msg.s.SetSize(a.contentWidth(), a.contentHeight())
		a.stack = append(a.stack, msg.s)
		return a, msg.s.Init()

	case popScreenMsg:
		return a.pop()

	case orgSwitchedMsg:
		a.ctx.Org = msg.org
		a.stack = []screen{newDashboardScreen(a.ctx)}
		a.resizeAll()
		a.setToast("switched to "+msg.org, false)
		return a, tea.Batch(a.active().Init(), a.toastExpiryCmd())

	case orgsLoadedMsg:
		if a.orgsw != nil {
			a.orgsw.setOrgs(msg.names)
		}
		return a, nil

	case actionResultMsg:
		a.setToast(msg.text, !msg.ok)
		cmds := []tea.Cmd{a.toastExpiryCmd()}
		if msg.ok {
			cmds = append(cmds, a.active().Refresh())
		}
		return a, tea.Batch(cmds...)

	case formReadyMsg:
		a.toastText = ""
		// Drop a late result if the user navigated to a different resource or
		// switched org while the options were resolving — otherwise the form
		// would open over, and submit against, the now-active screen (mirrors
		// the kind/id staleness guards on listLoadedMsg/detailLoadedMsg).
		act, ok := a.active().(actionable)
		if !ok || act.resource().Kind() != msg.kind || a.ctx.Org != msg.org {
			return a, nil
		}
		switch {
		case msg.err != nil:
			a.setToast(msg.err.Error(), true)
			return a, a.toastExpiryCmd()
		case msg.emptyField != "":
			a.setToast("no "+strings.ToLower(msg.emptyField)+" available", true)
			return a, a.toastExpiryCmd()
		}
		a.form = newForm(msg.act, msg.target)
		return a, nil

	case errMsg:
		a.status = msg.err.Error()
		// The org switcher loads its list asynchronously; surface a failure in
		// the overlay instead of leaving it stuck on "loading…".
		if a.orgsw != nil && msg.context == "orgs" {
			a.orgsw.setErr(msg.err.Error())
			return a, nil
		}
		return a.delegate(msg)

	case toastExpireMsg:
		if msg.seq == a.toastSeq {
			a.toastText = ""
		}
		return a, nil

	case listLoadedMsg, dashLoadedMsg, detailLoadedMsg, healthLoadedMsg, assocLoadedMsg:
		a.lastUpdated = time.Now()
		a.status = ""
		return a.delegate(msg)
	}
	return a, nil
}

// delegate forwards a message to the active screen and stores the result.
func (a *App) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := a.active().Update(msg)
	a.stack[len(a.stack)-1] = s
	return a, cmd
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return a, tea.Quit
	}

	// Overlays capture input while open.
	if a.confirm != nil {
		done, confirmed := a.confirm.handleKey(key)
		if done {
			act, target := a.confirm.act, a.confirm.target
			a.confirm = nil
			if confirmed {
				return a, a.runActionCmd(act, target, nil)
			}
		}
		return a, nil
	}
	if a.palette != nil {
		done, kind := a.palette.handleKey(key)
		if done {
			a.palette = nil
			switch {
			case kind == paletteQuitKind:
				return a, tea.Quit
			case kind != "":
				if res, ok := a.ctx.Reg.Get(kind); ok {
					ls := newListScreen(a.ctx, res)
					ls.SetSize(a.contentWidth(), a.contentHeight())
					a.stack = append(a.stack, ls)
					return a, ls.Init()
				}
			}
		}
		return a, nil
	}
	if a.orgsw != nil {
		done, org := a.orgsw.handleKey(key)
		if done {
			a.orgsw = nil
			if org != "" && org != a.ctx.Org {
				return a, func() tea.Msg { return orgSwitchedMsg{org: org} }
			}
		}
		return a, nil
	}
	if a.form != nil {
		done, submit, args := a.form.handleKey(key)
		if done {
			act, target := a.form.act, a.form.target
			a.form = nil
			if submit {
				return a, a.runActionCmd(act, target, args)
			}
		}
		return a, nil
	}
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}

	// A screen capturing text input (filter mode) gets all keys.
	if ic, ok := a.active().(inputCapturer); ok && ic.capturingInput() {
		return a.delegate(msg)
	}

	switch key {
	case "q":
		return a, tea.Quit
	case "?":
		a.showHelp = true
		return a, nil
	case ":":
		a.palette = newPalette(a.ctx.Reg)
		return a, nil
	case "o":
		a.orgsw = newOrgSwitcher()
		return a, loadOrgsCmd(a.ctx.Svc)
	case "r":
		return a, a.active().Refresh()
	case "esc", "backspace":
		if len(a.stack) > 1 {
			return a.pop()
		}
		return a, nil
	}

	if act, ok := a.matchAction(key); ok {
		return a.triggerAction(act)
	}

	return a.delegate(msg)
}

func (a *App) pop() (tea.Model, tea.Cmd) {
	if len(a.stack) <= 1 {
		return a, nil
	}
	a.stack = a.stack[:len(a.stack)-1]
	a.active().SetSize(a.contentWidth(), a.contentHeight())
	cmds := []tea.Cmd{a.active().Refresh()}
	// Screens that hold secondary data (e.g. the device page's associations)
	// reload it when revealed, so edits made in a child screen are reflected.
	if r, ok := a.active().(reactivatable); ok {
		cmds = append(cmds, r.onReveal())
	}
	return a, tea.Batch(cmds...)
}

func (a *App) resizeAll() {
	for _, s := range a.stack {
		s.SetSize(a.contentWidth(), a.contentHeight())
	}
}

func (a *App) matchAction(key string) (registry.Action, bool) {
	act, ok := a.active().(actionable)
	if !ok {
		return registry.Action{}, false
	}
	for _, ac := range act.actions() {
		if ac.Key == key {
			return ac, true
		}
	}
	return registry.Action{}, false
}

func (a *App) triggerAction(act registry.Action) (tea.Model, tea.Cmd) {
	actor := a.active().(actionable)
	target := actor.selectedID()
	if !act.TargetsAll && target == "" {
		a.setToast("no row selected", true)
		return a, a.toastExpiryCmd()
	}
	switch {
	case act.Nav != "":
		return a.navigate(actor, act, target)
	case len(act.Shell) > 0:
		return a, a.shellOutCmd(act, target)
	case len(act.Form) > 0:
		// The form itself is the deliberate gate. Fleet/collection-wide forms
		// (e.g. "new") don't operate on the selected row, so don't name it.
		formTarget := target
		if act.TargetsAll {
			formTarget = ""
		}
		// Dynamic selects are resolved against the API before the modal opens;
		// the form appears once formReadyMsg arrives.
		if formHasDynamicOptions(act) {
			a.setToast("loading…", false)
			return a, a.loadFormOptionsCmd(actor.resource(), act, formTarget)
		}
		a.form = newForm(act, formTarget)
		return a, nil
	default:
		// Every action that mutates via Execute is confirmed, so nothing is
		// changed by an accidental keypress. Fleet-wide actions ignore the
		// selected row; don't name a specific device in the prompt (BlastRadius
		// describes the real scope and triggers type-to-confirm).
		confirmTarget := target
		if act.TargetsAll {
			confirmTarget = ""
		}
		a.confirm = newConfirm(act, confirmTarget)
		return a, nil
	}
}

// navigate pushes a child list screen for a Nav action (a network's members, a
// schedule's tasks, …). The resource builds the parameterised child Resource;
// the app wraps it in a generic list screen on the back-stack.
func (a *App) navigate(actor actionable, act registry.Action, target string) (tea.Model, tea.Cmd) {
	nav, ok := actor.resource().(registry.Navigator)
	if ok {
		if child, found := nav.Navigate(a.ctx.Org, target, act.Nav); found {
			ls := newListScreen(a.ctx, child)
			ls.SetSize(a.contentWidth(), a.contentHeight())
			a.stack = append(a.stack, ls)
			return a, ls.Init()
		}
	}
	a.setToast("cannot open "+act.Label, true)
	return a, a.toastExpiryCmd()
}

// formHasDynamicOptions reports whether any of the action's form fields needs
// its option list resolved against the API before the modal can open.
func formHasDynamicOptions(act registry.Action) bool {
	for _, f := range act.Form {
		if f.OptionsFrom != "" {
			return true
		}
	}
	return false
}

// loadFormOptionsCmd resolves every dynamic select field's options via the
// resource's FormOptions, returning a formReadyMsg that carries the action with
// its Options populated (or an error / empty-field marker). The form modal is
// opened from the formReadyMsg handler so the fetch never blocks the UI loop.
func (a *App) loadFormOptionsCmd(res registry.Resource, act registry.Action, target string) tea.Cmd {
	opt, ok := res.(registry.FormOptioner)
	svc, org, key, kind := a.ctx.Svc, a.ctx.Org, act.Key, res.Kind()
	return func() tea.Msg {
		form := make([]registry.FormField, len(act.Form))
		copy(form, act.Form)
		if ok {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for i := range form {
				if form[i].OptionsFrom == "" {
					continue
				}
				opts, err := opt.FormOptions(c, svc, org, target, key, form[i].OptionsFrom)
				if err != nil {
					return formReadyMsg{kind: kind, org: org, err: err}
				}
				if len(opts) == 0 {
					return formReadyMsg{kind: kind, org: org, emptyField: form[i].Label}
				}
				form[i].Options = opts
				form[i].OptionsFrom = ""
			}
		}
		ready := act
		ready.Form = form
		return formReadyMsg{kind: kind, org: org, act: ready, target: target}
	}
}

// shellOutCmd suspends the TUI and runs the local ndcli binary for an
// interactive flow (connect, $EDITOR edits). {id}/{org} are substituted into
// each arg. On return the active screen refreshes via the action toast path.
func (a *App) shellOutCmd(act registry.Action, target string) tea.Cmd {
	bin := ndcliPath()
	args := make([]string, len(act.Shell))
	for i, s := range act.Shell {
		s = strings.ReplaceAll(s, "{id}", target)
		s = strings.ReplaceAll(s, "{org}", a.ctx.Org)
		args[i] = s
	}
	label := "ndcli " + strings.Join(args, " ")
	c := exec.Command(bin, args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return actionResultMsg{ok: false, text: label + ": " + err.Error()}
		}
		return actionResultMsg{ok: true, text: label + " finished"}
	})
}

// ndcliPath prefers the ndcli binary sitting next to this one (installed as a
// pair), falling back to PATH.
func ndcliPath() string {
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "ndcli")
		if _, statErr := os.Stat(cand); statErr == nil {
			return cand
		}
	}
	return "ndcli"
}

func (a *App) runActionCmd(act registry.Action, target string, args map[string]string) tea.Cmd {
	actor, ok := a.active().(actionable)
	if !ok {
		return nil
	}
	res := actor.resource()
	svc, org := a.ctx.Svc, a.ctx.Org
	id := target
	if act.TargetsAll {
		id = ""
	}
	key := act.Key
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		msg, err := res.Execute(c, svc, org, id, key, args)
		if err != nil {
			return actionResultMsg{ok: false, text: err.Error()}
		}
		return actionResultMsg{ok: true, text: msg}
	}
}

func (a *App) setToast(text string, isErr bool) {
	a.toastSeq++
	a.toastText = text
	a.toastErr = isErr
}

func (a *App) toastExpiryCmd() tea.Cmd {
	seq := a.toastSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastExpireMsg{seq: seq} })
}

// View implements tea.Model.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "loading…"
	}
	var body string
	switch {
	case a.showHelp:
		body = a.centered(a.helpView())
	case a.confirm != nil:
		body = a.centered(a.confirm.View())
	case a.palette != nil:
		body = a.centered(a.palette.View())
	case a.orgsw != nil:
		body = a.centered(a.orgsw.View())
	case a.form != nil:
		body = a.centered(a.form.View())
	default:
		body = a.active().View()
	}
	box := a.contentBox(a.active().Title(), body)
	return strings.Join([]string{a.renderHeader(), box, a.renderFooter()}, "\n")
}

func (a *App) centered(s string) string {
	return lipgloss.Place(a.contentWidth(), a.contentHeight(), lipgloss.Center, lipgloss.Center, s)
}

// contentBox wraps the body in a rounded border box whose top border carries
// the active screen's title (k9s-style), normalising the body to exactly the
// content height so the footer stays pinned to the bottom.
func (a *App) contentBox(title, body string) string {
	contentW := a.contentWidth()
	innerH := a.contentHeight()
	outerW := a.width

	bodyLines := strings.Split(body, "\n")
	rows := make([]string, innerH)
	vbar := borderStyle.Render("│")
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(bodyLines) {
			line = bodyLines[i]
		}
		rows[i] = vbar + " " + padLine(line, contentW) + " " + vbar
	}

	label := uihelp.Truncate(title, outerW-6)
	prefix := "╭─ "
	dashes := outerW - lipgloss.Width(prefix) - lipgloss.Width(label) - 2 // " " + "╮"
	if dashes < 0 {
		dashes = 0
	}
	top := borderStyle.Render(prefix) + titleStyle.Render(label) +
		borderStyle.Render(" "+strings.Repeat("─", dashes)+"╮")
	bottom := borderStyle.Render("╰" + strings.Repeat("─", outerW-2) + "╯")

	out := make([]string, 0, innerH+2)
	out = append(out, top)
	out = append(out, rows...)
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

// boxSpec is a titled box's content for flexRow.
type boxSpec struct {
	title string
	body  []string
}

// widthBox renders a bordered panel of an exact content width (variable height).
func widthBox(title string, body []string, contentW int) string {
	if contentW < 4 {
		contentW = 4
	}
	lines := append([]string{titleStyle.Render(title)}, body...)
	for i := range lines {
		lines[i] = padLine(lines[i], contentW)
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

// flexRow renders boxes filling totalW — each an equal share (the last takes the
// remainder) — separated by gap spaces, top-aligned.
func flexRow(totalW, gap int, specs []boxSpec) string {
	n := len(specs)
	if n == 0 {
		return ""
	}
	availOuter := totalW - gap*(n-1)
	if availOuter < n*8 {
		availOuter = n * 8
	}
	eachOuter := availOuter / n
	parts := make([]string, 0, 2*n-1)
	for i, sp := range specs {
		ow := eachOuter
		if i == n-1 {
			ow = availOuter - eachOuter*(n-1)
		}
		parts = append(parts, widthBox(sp.title, sp.body, ow-4))
		if i < n-1 {
			parts = append(parts, strings.Repeat(" ", gap))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// padLine clips a (possibly ANSI-styled) line to w display cells and right-pads
// it with spaces, ANSI-safely.
func padLine(s string, w int) string {
	s = lipgloss.NewStyle().MaxWidth(w).Render(s)
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (a *App) renderHeader() string {
	crumbs := make([]string, 0, len(a.stack))
	for _, s := range a.stack {
		crumbs = append(crumbs, s.Title())
	}
	updated := "—"
	if !a.lastUpdated.IsZero() {
		updated = a.lastUpdated.Format("15:04:05")
	}
	account := a.ctx.Account
	if account == "" {
		account = "—"
	}
	info := []string{
		crumbStyle.Render("User: ") + account,
		crumbStyle.Render("Organization: ") + a.ctx.Org,
		crumbStyle.Render("View: ") + strings.Join(crumbs, " › "),
		dimStyle.Render(fmt.Sprintf("updated %s · refresh %s", updated, a.ctx.Refresh)),
	}

	banner := bannerStyle.Render(netDefenseBanner)
	bw := lipgloss.Width(banner)
	if a.width < bw+24 {
		// Too narrow for the banner — show just the context block.
		return clipBlock(info, a.width, headerHeight)
	}
	leftW := a.width - bw
	left := lipgloss.NewStyle().Width(leftW).Render(clipBlock(info, leftW, headerHeight))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, banner)
}

// clipBlock clips each line to w cells and returns exactly h lines.
func clipBlock(lines []string, w, h int) string {
	out := make([]string, h)
	clip := lipgloss.NewStyle().MaxWidth(w)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out[i] = clip.Render(lines[i])
		}
	}
	return strings.Join(out, "\n")
}

// renderFooter draws the bottom "Keys" box: the command hints wrapped across
// two lines (so a screen with many actions stays fully visible) and the
// current status (toast / error / idle counts) in the box's title bar.
func (a *App) renderFooter() string {
	var segs []string
	if act, ok := a.active().(actionable); ok {
		for _, ac := range act.actions() {
			segs = append(segs, keyStyle.Render(ac.Key)+mutedStyle.Render(" "+ac.Label))
		}
	}
	if _, isList := a.active().(*listScreen); isList {
		segs = append(segs,
			keyStyle.Render("/")+mutedStyle.Render(" filter"),
			keyStyle.Render("[ ]")+mutedStyle.Render(" page"),
			keyStyle.Render("⏎")+mutedStyle.Render(" open"),
		)
	}
	if _, isDev := a.active().(*healthScreen); isDev {
		segs = append(segs, keyStyle.Render("v")+mutedStyle.Render(" overrides"))
	}
	// ? and q lead the nav group so the help/quit escape hatches survive an
	// overflow on narrow terminals; the rest may wrap off (recoverable via ?).
	nav := []string{
		keyStyle.Render("?") + mutedStyle.Render(" help"),
		keyStyle.Render("q") + mutedStyle.Render(" quit"),
	}
	if len(a.stack) > 1 {
		nav = append(nav, keyStyle.Render("esc")+mutedStyle.Render(" back"))
	}
	nav = append(nav,
		keyStyle.Render(":")+mutedStyle.Render(" palette"),
		keyStyle.Render("o")+mutedStyle.Render(" org"),
		keyStyle.Render("r")+mutedStyle.Render(" refresh"),
		keyStyle.Render("↑↓")+mutedStyle.Render(" move"),
	)
	segs = append(segs, nav...)

	line1, line2 := wrapSegments(segs, a.contentWidth(), "  ")
	return a.keysBox(line1, line2, a.statusText())
}

// statusText returns the current footer status: a toast, a sticky error, or the
// active screen's idle status line — already coloured.
func (a *App) statusText() string {
	switch {
	case a.toastText != "":
		if a.toastErr {
			return errStyle.Render(a.toastText)
		}
		return okStyle.Render(a.toastText)
	case a.status != "":
		return errStyle.Render(a.status)
	default:
		if st, ok := a.active().(statuser); ok {
			return dimStyle.Render(st.statusLine())
		}
		return ""
	}
}

// keysBox renders the 4-line command area: a rounded border whose top carries
// the "Keys" title (left) and the status (right), with two command lines inside.
func (a *App) keysBox(line1, line2, status string) string {
	outerW := a.width
	interiorW := a.contentWidth()
	vbar := borderStyle.Render("│")
	mid1 := vbar + " " + padLine(line1, interiorW) + " " + vbar
	mid2 := vbar + " " + padLine(line2, interiorW) + " " + vbar

	left := borderStyle.Render("╭─ ") + titleStyle.Render("Keys") + borderStyle.Render(" ")
	leftW := lipgloss.Width("╭─ Keys ")

	statusPart, statusW := "", 0
	if status != "" {
		maxStatusW := outerW - leftW - 6
		if maxStatusW < 0 {
			maxStatusW = 0
		}
		status = lipgloss.NewStyle().MaxWidth(maxStatusW).Render(status)
		statusPart = " " + status + " "
		statusW = lipgloss.Width(statusPart)
	}
	dashes := outerW - leftW - statusW - 1
	if dashes < 0 {
		dashes = 0
	}
	top := left + borderStyle.Render(strings.Repeat("─", dashes)) + statusPart + borderStyle.Render("╮")
	bottom := borderStyle.Render("╰" + strings.Repeat("─", outerW-2) + "╯")
	return strings.Join([]string{top, mid1, mid2, bottom}, "\n")
}

// wrapSegments greedily packs already-styled segments into two lines that each
// fit within width, joined by sep. Segments that don't fit in two lines are
// dropped (with a trailing ellipsis on the second line).
func wrapSegments(segs []string, width int, sep string) (string, string) {
	sepW := lipgloss.Width(sep)
	lines := make([]string, 0, 2)
	cur, curW := "", 0
	for _, s := range segs {
		sw := lipgloss.Width(s)
		switch {
		case cur == "":
			cur, curW = s, sw
		case curW+sepW+sw <= width:
			cur += sep + s
			curW += sepW + sw
		default:
			lines = append(lines, cur)
			cur, curW = s, sw
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	l1, l2 := "", ""
	if len(lines) > 0 {
		l1 = lines[0]
	}
	if len(lines) > 1 {
		l2 = lines[1]
	}
	if len(lines) > 2 {
		l2 = lipgloss.NewStyle().MaxWidth(width-2).Render(l2) + mutedStyle.Render(" …")
	}
	return l1, l2
}

func (a *App) helpView() string {
	rows := [][2]string{
		{"↑/↓ j/k", "move cursor"},
		{"⏎", "open / drill in"},
		{"esc", "back"},
		{"/", "filter the list"},
		{"[ ]", "previous / next page"},
		{":", "command palette (jump to a resource)"},
		{"o", "switch organization"},
		{"r", "refresh now"},
		{"g / G", "jump to top / bottom"},
		{"q", "quit"},
	}
	lines := []string{titleStyle.Render("Keys"), ""}
	for _, r := range rows {
		lines = append(lines, keyStyle.Render(fitCell(r[0], 10))+"  "+r[1])
	}
	lines = append(lines,
		"",
		mutedStyle.Render("Per-view actions are listed in the bottom Keys box."),
		mutedStyle.Render("press any key to close"),
	)
	return modalStyle.Render(strings.Join(lines, "\n"))
}
