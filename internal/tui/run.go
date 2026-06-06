package tui

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/resources"
)

// Run parses flags, wires up the shared service layer (mirroring the CLI/MCP
// bootstrap in cli/root.go and internal/mcp/server.go) and starts the TUI.
func Run(args []string, version string) error {
	fs := flag.NewFlagSet("netdefense", flag.ContinueOnError)
	var orgFlag, orgShort, confFlag string
	var showVersion bool
	fs.StringVar(&orgFlag, "org", "", "organization name (overrides config)")
	fs.StringVar(&orgShort, "o", "", "organization name (shorthand)")
	fs.StringVar(&confFlag, "conf", "", "config file path")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Printf("netdefense version %s\n", version)
		fmt.Printf("Build time: %s\n", config.BuildTime)
		return nil
	}

	if err := config.Load(confFlag); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	svc, cleanup, err := buildService()
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	cfg := config.Get()
	org := firstNonEmpty(orgFlag, orgShort, cfg.Organization.Name)
	if org == "" {
		return fmt.Errorf("no organization set — pass --org, set organization.name in config, or set NDCLI_ORGANIZATION_NAME")
	}

	refresh := defaultRefresh
	if v := os.Getenv("NDCLI_TUI_REFRESH"); v != "" {
		if d, perr := time.ParseDuration(v); perr == nil && d >= time.Second {
			refresh = d
		}
	}

	reg := registry.New()
	resources.RegisterAll(reg)

	ctx := &AppContext{Svc: svc, Cfg: cfg, Org: org, Account: resolveAccount(svc), Reg: reg, Refresh: refresh}
	p := tea.NewProgram(newApp(ctx), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// resolveAccount returns the logged-in account's email (or name) for the
// header. It prefers the locally-cached identity (OAuth token claims) and falls
// back to GET /auth/me, which also works under NDCLI_TOKEN.
func resolveAccount(svc *service.Service) string {
	if u, err := svc.AuthLocalUser(); err == nil && u != nil {
		if u.Email != "" {
			return u.Email
		}
		if u.Name != "" {
			return u.Name
		}
	}
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if me, err := svc.AuthMe(c); err == nil && me != nil {
		if me.Email != "" {
			return me.Email
		}
		if me.Name != nil && *me.Name != "" {
			return *me.Name
		}
	}
	return ""
}

// buildService constructs the shared *service.Service, supporting both the
// static-PAT (NDCLI_TOKEN) and OAuth2 paths. The returned cleanup closes the
// auth manager when non-nil.
func buildService() (*service.Service, func(), error) {
	if token := os.Getenv("NDCLI_TOKEN"); token != "" {
		if len(token) <= 6 || token[:6] != "ndpat_" {
			return nil, nil, fmt.Errorf("NDCLI_TOKEN does not look like a valid personal access token (expected prefix: ndpat_)")
		}
		provider := auth.NewStaticTokenProvider(token)
		client := api.NewClientFromConfig(provider)
		return service.New(client, nil, config.Get()), nil, nil
	}

	mgr := auth.GetManager()
	if !mgr.IsAuthenticated() {
		mgr.Close()
		return nil, nil, fmt.Errorf("not authenticated — run 'ndcli auth login' first")
	}
	client := api.NewClientFromConfig(mgr)
	return service.New(client, mgr, config.Get()), mgr.Close, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
