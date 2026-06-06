package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// screen is one navigable view in the back-stack. The app drives global
// concerns (header, footer, overlays, navigation, refresh tick); a screen
// owns its own data, cursor and content rendering.
type screen interface {
	// Init returns the command that loads the screen's initial data.
	Init() tea.Cmd
	// Update handles a message and returns the (possibly updated) screen.
	Update(tea.Msg) (screen, tea.Cmd)
	// View renders the content area (between header and footer).
	View() string
	// Title is the breadcrumb label.
	Title() string
	// SetSize is called with the content-area dimensions.
	SetSize(w, h int)
	// Refresh reloads the screen's data (auto-refresh tick + manual "r").
	Refresh() tea.Cmd
}

// actionable is implemented by screens that expose per-row actions (the list
// screen). The app reads these to build the footer and dispatch action keys.
type actionable interface {
	actions() []registry.Action
	selectedID() string
	resource() registry.Resource
}

// inputCapturer is implemented by screens that may consume every key (e.g. a
// list in "/" filter mode), suppressing the app's global key handling.
type inputCapturer interface {
	capturingInput() bool
}

// statuser is implemented by screens that provide a one-line idle status
// (item counts, filter state) shown faintly at the bottom-left.
type statuser interface {
	statusLine() string
}

// reactivatable is implemented by screens that reload secondary data when they
// are revealed again (popped back to), e.g. the device page re-fetching its
// associations after a child screen edited them.
type reactivatable interface {
	onReveal() tea.Cmd
}

// pushScreen / popScreen are navigation commands a screen returns to ask the
// app to change the back-stack.
func pushScreen(s screen) tea.Cmd {
	return func() tea.Msg { return pushScreenMsg{s: s} }
}

func popScreen() tea.Cmd {
	return func() tea.Msg { return popScreenMsg{} }
}
