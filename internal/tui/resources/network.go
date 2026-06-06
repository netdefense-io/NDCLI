package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// networkResource surfaces the org's VPN networks. It is read-only in the TUI
// for v1 — creating, editing and wiring members/links stays on the cli surface.
// Describe powers the generic detail fallback.
type networkResource struct{}

func (networkResource) Kind() string  { return "network" }
func (networkResource) Title() string { return "Networks" }

func (networkResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 24},
		{Title: "CIDR", Width: 20},
		{Title: "HUBS", Width: 8},
		{Title: "MEMBERS", Width: 9},
		{Title: "OVERRIDES", Width: 0},
	}
}

func (networkResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.NetworkList(ctx, org, page, perPage)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Networks))
	for _, n := range res.Networks {
		rows = append(rows, registry.Row{
			ID: n.Name,
			Cells: []string{
				n.Name,
				uihelp.Default(n.OverlayCIDRv4, "—"),
				autoConnectLabel(n.AutoConnectHubs),
				fmt.Sprintf("%d", n.MemberCount),
				fmt.Sprintf("%d", n.LinkCount),
			},
		})
	}
	return rows, res.Total, nil
}

func (networkResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete network {id}?"},
	}
}

func (networkResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.NetworkDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (networkResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	n, err := svc.NetworkGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: n.Name},
		{Label: "Overlay CIDR", Value: uihelp.Default(n.OverlayCIDRv4, "—")},
		{Label: "Auto-connect hubs", Value: fmt.Sprintf("%t", n.AutoConnectHubs)},
		{Label: "Auto firewall rules", Value: fmt.Sprintf("%t", n.AutoFirewallRules)},
		{Label: "Listen port default", Value: fmt.Sprintf("%d", n.ListenPortDefault)},
		{Label: "MTU default", Value: intPtrLabel(n.MTUDefault)},
		{Label: "Keepalive default", Value: intPtrLabel(n.KeepaliveDefault)},
		{Label: "Members", Value: fmt.Sprintf("%d", n.MemberCount)},
		{Label: "Overrides", Value: fmt.Sprintf("%d", n.LinkCount)},
		{Label: "Created", Value: fullTime(n.CreatedAt)},
		{Label: "Updated", Value: fullTime(n.UpdatedAt)},
	}
	return []registry.Section{{Title: "VPN Network", Fields: fields}}, nil
}

// autoConnectLabel renders the auto-connect-hubs flag for the list view.
func autoConnectLabel(b bool) string {
	if b {
		return "auto"
	}
	return "—"
}

// intPtrLabel renders a nilable int default ("—" when unset/server-NULL).
func intPtrLabel(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *p)
}
