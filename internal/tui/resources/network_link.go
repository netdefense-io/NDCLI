package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// NetworkLinkResource lists the raw link overrides of one VPN network. A link's
// row id encodes both endpoints as "deviceA|deviceB"; splitLinkID recovers them
// for update/delete.
type NetworkLinkResource struct {
	Network string
}

func (NetworkLinkResource) Kind() string  { return "network-link" }
func (NetworkLinkResource) Title() string { return "Links" }

func (NetworkLinkResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "DEVICE A", Width: 0},
		{Title: "DEVICE B", Width: 0},
		{Title: "ENABLED", Width: 8},
		{Title: "PSK", Width: 6},
		{Title: "UPDATED", Width: 0},
	}
}

func (r NetworkLinkResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.NetworkLinkListRaw(ctx, org, r.Network, page, perPage)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Links))
	for _, l := range res.Links {
		rows = append(rows, registry.Row{
			ID: l.DeviceAName + "|" + l.DeviceBName,
			Cells: []string{
				l.DeviceAName,
				l.DeviceBName,
				yesNo(l.Enabled),
				yesNo(l.HasPSK),
				ago(l.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (NetworkLinkResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "device_a", Label: "Device A", Required: true},
			{Key: "device_b", Label: "Device B", Required: true},
			{Key: "enabled", Label: "Enabled", Options: []string{"yes", "no"}, Default: "yes"},
			{Key: "generate_psk", Label: "Generate PSK", Options: []string{"no", "yes"}, Default: "no"},
		}},
		{Key: "u", Label: "update", Form: []registry.FormField{
			{Key: "enabled", Label: "Enabled", Options: []string{"(unchanged)", "yes", "no"}, Default: "(unchanged)"},
			{Key: "regenerate_psk", Label: "Regen PSK", Options: []string{"no", "yes"}, Default: "no"},
		}},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete link {id}?"},
	}
}

func (r NetworkLinkResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		opts := service.NetworkLinkCreateOpts{
			Enabled:     boolPtr(args["enabled"] == "yes"),
			GeneratePSK: boolPtr(args["generate_psk"] == "yes"),
		}
		if _, err := svc.NetworkLinkCreate(ctx, org, r.Network, args["device_a"], args["device_b"], opts); err != nil {
			return "", err
		}
		return "created link", nil
	case "u":
		a, b := splitLinkID(id)
		opts := service.NetworkLinkUpdateOpts{
			Enabled: netSelBoolPtr(args["enabled"]),
		}
		if args["regenerate_psk"] == "yes" {
			opts.RegeneratePSK = boolPtr(true)
		}
		if _, err := svc.NetworkLinkUpdate(ctx, org, r.Network, a, b, opts); err != nil {
			return "", err
		}
		return "updated link", nil
	case "x":
		a, b := splitLinkID(id)
		if err := svc.NetworkLinkDelete(ctx, org, r.Network, a, b); err != nil {
			return "", err
		}
		return "deleted link", nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// splitLinkID recovers the two endpoint device names from a link row id of the
// form "deviceA|deviceB" (split on the first "|").
func splitLinkID(id string) (a, b string) {
	a, b, _ = strings.Cut(id, "|")
	return a, b
}
