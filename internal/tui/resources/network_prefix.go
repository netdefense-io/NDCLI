package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// NetworkPrefixResource lists the prefixes one member publishes in a VPN
// network (id is the published VARIABLE name). Reached by drilling in from a
// NetworkMemberResource row ("p").
type NetworkPrefixResource struct {
	Network string
	Device  string
}

func (NetworkPrefixResource) Kind() string  { return "network-prefix" }
func (NetworkPrefixResource) Title() string { return "Prefixes" }

func (NetworkPrefixResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "VARIABLE", Width: 0},
		{Title: "PUBLISHED", Width: 10},
		{Title: "DEVICE", Width: 0},
		{Title: "UPDATED", Width: 0},
	}
}

func (r NetworkPrefixResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.NetworkPrefixList(ctx, org, r.Network, r.Device, page, perPage)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Prefixes))
	for _, p := range res.Prefixes {
		rows = append(rows, registry.Row{
			ID: p.VariableName,
			Cells: []string{
				p.VariableName,
				yesNo(p.Publish),
				p.DeviceName,
				ago(p.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (NetworkPrefixResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "add", TargetsAll: true, Form: []registry.FormField{
			{Key: "variable", Label: "Variable", Required: true},
			{Key: "publish", Label: "Publish", Options: []string{"yes", "no"}, Default: "yes"},
		}},
		{Key: "u", Label: "publish", Form: []registry.FormField{
			{Key: "publish", Label: "Publish", Options: []string{"yes", "no"}, Default: "yes"},
		}},
		{Key: "x", Label: "remove", Destructive: true,
			Prompt: "Remove prefix {id}?"},
	}
}

func (r NetworkPrefixResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		if _, err := svc.NetworkPrefixAdd(ctx, org, r.Network, r.Device, args["variable"], boolPtr(args["publish"] == "yes")); err != nil {
			return "", err
		}
		return "added prefix " + args["variable"], nil
	case "u":
		if _, err := svc.NetworkPrefixUpdate(ctx, org, r.Network, r.Device, id, boolPtr(args["publish"] == "yes")); err != nil {
			return "", err
		}
		return "updated prefix " + id, nil
	case "x":
		if err := svc.NetworkPrefixRemove(ctx, org, r.Network, r.Device, id); err != nil {
			return "", err
		}
		return "removed prefix " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}
