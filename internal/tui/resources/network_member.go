package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// NetworkMemberResource lists the members of one VPN network (id is the member
// DEVICE name). Reached by drilling in from a networkResource row ("m"); it
// further drills into a member's published prefixes ("p").
type NetworkMemberResource struct {
	Network string
}

func (NetworkMemberResource) Kind() string  { return "network-member" }
func (NetworkMemberResource) Title() string { return "Members" }

func (NetworkMemberResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "DEVICE", Width: 0},
		{Title: "ROLE", Width: 8},
		{Title: "ENABLED", Width: 8},
		{Title: "OVERLAY IPv4", Width: 18},
		{Title: "UPDATED", Width: 0},
	}
}

func (r NetworkMemberResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.NetworkMemberList(ctx, org, r.Network, page, perPage)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Members))
	for _, m := range res.Members {
		rows = append(rows, registry.Row{
			ID: m.DeviceName,
			Cells: []string{
				m.DeviceName,
				m.Role,
				yesNo(m.Enabled),
				uihelp.Default(m.OverlayIPv4, "—"),
				ago(m.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (r NetworkMemberResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "add", TargetsAll: true, Form: []registry.FormField{
			{Key: "device", Label: "Device", Required: true},
			{Key: "role", Label: "Role", Options: []string{"SPOKE", "HUB"}, Default: "SPOKE"},
			{Key: "enabled", Label: "Enabled", Options: []string{"yes", "no"}, Default: "yes"},
			{Key: "overlay_ip", Label: "Overlay IP"},
			{Key: "endpoint_host", Label: "Endpoint host"},
			{Key: "endpoint_port", Label: "Endpoint port"},
			{Key: "listen_port", Label: "Listen port"},
			{Key: "mtu", Label: "MTU"},
			{Key: "keepalive", Label: "Keepalive"},
			{Key: "transit_via_hub", Label: "Transit hub"},
		}},
		{Key: "u", Label: "update", Form: []registry.FormField{
			{Key: "role", Label: "Role", Options: []string{"(unchanged)", "SPOKE", "HUB"}, Default: "(unchanged)"},
			{Key: "enabled", Label: "Enabled", Options: []string{"(unchanged)", "yes", "no"}, Default: "(unchanged)"},
			{Key: "endpoint_host", Label: "Endpoint host"},
			{Key: "endpoint_port", Label: "Endpoint port"},
			{Key: "listen_port", Label: "Listen port"},
			{Key: "mtu", Label: "MTU"},
			{Key: "keepalive", Label: "Keepalive"},
			{Key: "transit_via_hub", Label: "Transit hub"},
			{Key: "regenerate_keys", Label: "Regen keys", Options: []string{"no", "yes"}, Default: "no"},
		}},
		{Key: "x", Label: "remove", Destructive: true,
			Prompt: "Remove member {id} from " + r.Network + "?"},
		{Key: "p", Label: "prefixes", Nav: "prefixes"},
	}
}

func (r NetworkMemberResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		opts := service.NetworkMemberAddOpts{
			Role:          args["role"],
			Enabled:       boolPtr(args["enabled"] == "yes"),
			OverlayIPv4:   args["overlay_ip"],
			EndpointHost:  args["endpoint_host"],
			TransitViaHub: args["transit_via_hub"],
		}
		var err error
		if opts.EndpointPort, err = netOptInt(args["endpoint_port"]); err != nil {
			return "", err
		}
		if opts.ListenPort, err = netOptInt(args["listen_port"]); err != nil {
			return "", err
		}
		if opts.MTU, err = netOptInt(args["mtu"]); err != nil {
			return "", err
		}
		if opts.Keepalive, err = netOptInt(args["keepalive"]); err != nil {
			return "", err
		}
		if _, err := svc.NetworkMemberAdd(ctx, org, r.Network, args["device"], opts); err != nil {
			return "", err
		}
		return "added member " + args["device"], nil
	case "u":
		opts := service.NetworkMemberUpdateOpts{
			Enabled: netUnchangedBoolPtr(args["enabled"]),
		}
		if role := args["role"]; role != "(unchanged)" {
			opts.Role = &role
		}
		if v := args["endpoint_host"]; v != "" {
			opts.EndpointHost = &v
		}
		if v := args["transit_via_hub"]; v != "" {
			opts.TransitViaHub = &v
		}
		var err error
		if opts.EndpointPort, err = netOptIntPtr(args["endpoint_port"]); err != nil {
			return "", err
		}
		if opts.ListenPort, err = netOptIntPtr(args["listen_port"]); err != nil {
			return "", err
		}
		if opts.MTU, err = netOptIntPtr(args["mtu"]); err != nil {
			return "", err
		}
		if opts.Keepalive, err = netOptIntPtr(args["keepalive"]); err != nil {
			return "", err
		}
		if args["regenerate_keys"] == "yes" {
			opts.RegenerateKeys = boolPtr(true)
		}
		if _, err := svc.NetworkMemberUpdate(ctx, org, r.Network, id, opts); err != nil {
			return "", err
		}
		return "updated member " + id, nil
	case "x":
		if err := svc.NetworkMemberRemove(ctx, org, r.Network, id); err != nil {
			return "", err
		}
		return "removed member " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Navigate implements registry.Navigator: drill from a member into its
// published prefixes.
func (r NetworkMemberResource) Navigate(org, id, nav string) (registry.Resource, bool) {
	if nav == "prefixes" {
		return NetworkPrefixResource{Network: r.Network, Device: id}, true
	}
	return nil, false
}
