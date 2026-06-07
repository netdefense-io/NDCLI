package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// networkResource surfaces the org's VPN networks (id is the network NAME).
// It supports create/update/delete here, and drills into a network's members
// and links via Nav actions. Describe powers the generic detail fallback.
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
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "cidr", Label: "Overlay CIDR", Placeholder: "10.100.0.0/24", Required: true},
			{Key: "auto_connect_hubs", Label: "Auto-connect", Options: []string{"(default)", "yes", "no"}, Default: "(default)"},
			{Key: "auto_firewall_rules", Label: "Auto-fw", Options: []string{"(default)", "yes", "no"}, Default: "(default)"},
			{Key: "listen_port", Label: "Listen port"},
			{Key: "mtu", Label: "MTU"},
			{Key: "keepalive", Label: "Keepalive"},
		}},
		{Key: "u", Label: "update", Form: []registry.FormField{
			{Key: "name", Label: "New name", Placeholder: "(blank=keep)"},
			{Key: "auto_connect_hubs", Label: "Auto-connect", Options: []string{"(unchanged)", "yes", "no"}, Default: "(unchanged)"},
			{Key: "auto_firewall_rules", Label: "Auto-fw", Options: []string{"(unchanged)", "yes", "no"}, Default: "(unchanged)"},
			{Key: "listen_port", Label: "Listen port"},
			{Key: "mtu", Label: "MTU"},
			{Key: "keepalive", Label: "Keepalive"},
		}},
		{Key: "m", Label: "members", Nav: "members"},
		{Key: "l", Label: "links", Nav: "links"},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete network {id}?"},
	}
}

func (networkResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		listenPort, err := netOptIntPtr(args["listen_port"])
		if err != nil {
			return "", err
		}
		mtu, err := netOptIntPtr(args["mtu"])
		if err != nil {
			return "", err
		}
		keepalive, err := netOptIntPtr(args["keepalive"])
		if err != nil {
			return "", err
		}
		opts := service.NetworkCreateOpts{
			Name:              args["name"],
			OverlayCIDRv4:     args["cidr"],
			AutoConnectHubs:   netSelBoolPtr(args["auto_connect_hubs"]),
			AutoFirewallRules: netSelBoolPtr(args["auto_firewall_rules"]),
			ListenPortDefault: listenPort,
			MTUDefault:        mtu,
			KeepaliveDefault:  keepalive,
		}
		if _, err := svc.NetworkCreate(ctx, org, opts); err != nil {
			return "", err
		}
		return "created network " + args["name"], nil
	case "u":
		listenPort, err := netOptIntPtr(args["listen_port"])
		if err != nil {
			return "", err
		}
		mtu, err := netOptIntPtr(args["mtu"])
		if err != nil {
			return "", err
		}
		keepalive, err := netOptIntPtr(args["keepalive"])
		if err != nil {
			return "", err
		}
		opts := service.NetworkUpdateOpts{
			AutoConnectHubs:   netSelBoolPtr(args["auto_connect_hubs"]),
			AutoFirewallRules: netSelBoolPtr(args["auto_firewall_rules"]),
			ListenPortDefault: listenPort,
			MTUDefault:        mtu,
			KeepaliveDefault:  keepalive,
		}
		if v := args["name"]; v != "" {
			opts.Name = &v
		}
		if opts.Name == nil && opts.AutoConnectHubs == nil && opts.AutoFirewallRules == nil &&
			opts.ListenPortDefault == nil && opts.MTUDefault == nil && opts.KeepaliveDefault == nil {
			return "", fmt.Errorf("no updates specified")
		}
		if _, err := svc.NetworkUpdate(ctx, org, id, opts); err != nil {
			return "", err
		}
		return "updated " + id, nil
	case "x":
		if err := svc.NetworkDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Navigate implements registry.Navigator: drill from a network row into its
// members or links child screens.
func (networkResource) Navigate(org, id, nav string) (registry.Resource, bool) {
	switch nav {
	case "members":
		return NetworkMemberResource{Network: id}, true
	case "links":
		return NetworkLinkResource{Network: id}, true
	}
	return nil, false
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

// =============================================================================
// Shared file-private helpers for the network/* resources (members, links,
// prefixes share this package, so each helper is defined exactly once here).
// =============================================================================

// yesNo renders a bool as "yes"/"no" for list cells.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// boolPtr returns a pointer to the given bool.
func boolPtr(b bool) *bool { return &b }

// netSelBoolPtr maps a form select to *bool: "yes" => true, "no" => false, and
// any sentinel ("(default)" on create / "(unchanged)" on update) => nil, so the
// field is left to the server default or left unchanged.
func netSelBoolPtr(v string) *bool {
	switch v {
	case "yes":
		return boolPtr(true)
	case "no":
		return boolPtr(false)
	}
	return nil
}

// netOptIntPtr parses an optional int form field into a *int: blank => nil,
// otherwise strconv.Atoi with a clear error on bad input.
func netOptIntPtr(v string) (*int, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("invalid number %q", v)
	}
	return &n, nil
}

// netOptInt parses an optional int form field into a plain int: blank => 0,
// otherwise strconv.Atoi with a clear error on bad input.
func netOptInt(v string) (int, error) {
	if strings.TrimSpace(v) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", v)
	}
	return n, nil
}
