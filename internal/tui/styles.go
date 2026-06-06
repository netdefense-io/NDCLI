package tui

import "github.com/charmbracelet/lipgloss"

// Colour palette — 256-colour codes so it degrades gracefully over SSH.
var (
	colAccent = lipgloss.Color("39")  // cyan-blue
	colMuted  = lipgloss.Color("244") // grey
	colDim    = lipgloss.Color("240") // dark grey
	colOK     = lipgloss.Color("42")  // green
	colWarn   = lipgloss.Color("214") // amber
	colErr    = lipgloss.Color("203") // red
	colText   = lipgloss.Color("231") // near-white
	colSelBg  = lipgloss.Color("24")  // selected-row background
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	crumbStyle = lipgloss.NewStyle().Foreground(colMuted)
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle   = lipgloss.NewStyle().Foreground(colDim)

	okStyle   = lipgloss.NewStyle().Foreground(colOK)
	warnStyle = lipgloss.NewStyle().Foreground(colWarn)
	errStyle  = lipgloss.NewStyle().Foreground(colErr)

	colHeadStyle = lipgloss.NewStyle().Bold(true).Foreground(colMuted)
	selRowStyle  = lipgloss.NewStyle().Foreground(colText).Background(colSelBg)

	keyStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	borderStyle = lipgloss.NewStyle().Foreground(colMuted)
	bannerStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colMuted).
			Padding(0, 1)

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(1, 3)

	dangerModalStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colErr).
				Padding(1, 3)
)

// netDefenseBanner is the NetDefense block-letter logo from the `ndcli` setup
// wizard (internal/wizard ShowBanner), rendered in the TUI's top-right corner.
// The wizard's wordmark reads "NetDefense CLI"; the TUI shows just the
// "NetDefense" portion (the " CLI" columns are dropped).
const netDefenseBanner = "▙ ▌   ▐  ▛▀▖   ▗▀▖\n" +
	"▌▌▌▞▀▖▜▀ ▌ ▌▞▀▖▐  ▞▀▖▛▀▖▞▀▘▞▀▖\n" +
	"▌▝▌▛▀ ▐ ▖▌ ▌▛▀ ▜▀ ▛▀ ▌ ▌▝▀▖▛▀\n" +
	"▘ ▘▝▀▘ ▀ ▀▀ ▝▀▘▐  ▝▀▘▘ ▘▀▀ ▝▀▘"

// statusStyle returns a colour for a known status/state string so list and
// dashboard cells read at a glance.
func statusStyle(s string) lipgloss.Style {
	switch s {
	case "online", "in-sync", "IN_SYNC", "ENABLED", "COMPLETED", "yes":
		return okStyle
	case "stale", "drift", "DRIFT", "PENDING", "SCHEDULED", "IN_PROGRESS", "unknown":
		return warnStyle
	case "offline", "error", "ERROR", "FAILED", "EXPIRED", "CANCELLED", "DISABLED":
		return errStyle
	default:
		return lipgloss.NewStyle()
	}
}
