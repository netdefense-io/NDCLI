package tui

import (
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// tickMsg drives the auto-refresh of the active screen.
type tickMsg time.Time

// --- data-load lifecycle ------------------------------------------------
//
// Each screen issues a load command that resolves to one of these messages.
// Messages carry the originating kind/id so a result arriving after the user
// has navigated elsewhere is ignored by the now-active screen.

type listLoadedMsg struct {
	kind  string
	rows  []registry.Row
	total int
	page  int
}

type dashLoadedMsg struct {
	data *models.DashboardResponse
}

type detailLoadedMsg struct {
	kind     string
	id       string
	sections []registry.Section
}

type healthLoadedMsg struct {
	device string
	data   *models.DeviceTelemetryResponse
}

// assocLoadedMsg carries the device's associated resources (OUs, templates,
// variables) for the health screen. Fetched once when the screen opens.
type assocLoadedMsg struct {
	device string
	assoc  *deviceAssoc
}

// errMsg reports a failed load or action. context is a short label for the
// status line ("devices", "device health", …).
type errMsg struct {
	context string
	err     error
}

// orgsLoadedMsg feeds the org-switcher overlay.
type orgsLoadedMsg struct {
	names []string
}

// --- actions ------------------------------------------------------------

// actionResultMsg is emitted after an action's service call resolves. ok
// distinguishes success (green toast) from failure (red toast).
type actionResultMsg struct {
	ok   bool
	text string
}

// formReadyMsg is emitted after a Form action's dynamic select options
// (FormField.OptionsFrom) have been resolved, carrying the action with its
// Options populated. emptyField names a dynamic field that resolved to no
// candidates (the app aborts with "no <field> available"); err carries a fetch
// failure. When both are empty the app opens the form. kind/org stamp the
// originating resource + org so a result arriving after the user navigated
// elsewhere (palette, back, org-switch) is dropped instead of opening a form
// over — and dispatching against — the wrong resource.
type formReadyMsg struct {
	kind       string
	org        string
	act        registry.Action
	target     string
	emptyField string
	err        error
}

// --- navigation ---------------------------------------------------------

// pushScreenMsg asks the app to push a new screen onto the back-stack.
type pushScreenMsg struct{ s screen }

// popScreenMsg asks the app to pop the active screen.
type popScreenMsg struct{}

// orgSwitchedMsg is emitted by the org switcher when a new org is chosen.
type orgSwitchedMsg struct{ org string }

// toastExpireMsg clears a toast once its lifetime elapses.
type toastExpireMsg struct{ seq int }
