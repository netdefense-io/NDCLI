package tui

import (
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// AppContext is the read-mostly state shared by every screen: the service
// layer, the active organization, the resource registry and the refresh
// cadence. Screens hold a pointer to it, so updating Org (via the org
// switcher) is visible everywhere — though in practice an org switch rebuilds
// the screen stack, so stale data never lingers.
type AppContext struct {
	Svc     *service.Service
	Cfg     *config.Config
	Org     string
	Account string // logged-in account email/name, shown in the header
	Reg     *registry.Registry
	Refresh time.Duration
}

// defaultRefresh is the auto-refresh cadence for the active screen. Overridable
// via the NDCLI_TUI_REFRESH environment variable (e.g. "10s").
const defaultRefresh = 5 * time.Second
