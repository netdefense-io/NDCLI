package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// accountResource surfaces the user accounts in an organization. The list
// exposes enable/disable actions; the rest of member management (role changes,
// invites) stays on the cli/MCP surface. The account list endpoint returns the
// full set in one shot, so there is no pagination to thread through.
type accountResource struct{}

func (accountResource) Kind() string  { return "account" }
func (accountResource) Title() string { return "Accounts" }

func (accountResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "EMAIL", Width: 30},
		{Title: "NAME", Width: 20},
		{Title: "ROLE", Width: 8},
		{Title: "STATUS", Width: 10},
		{Title: "LAST LOGIN", Width: 0},
	}
}

func (accountResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.OrgAccountList(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Accounts))
	for _, a := range res.Accounts {
		rows = append(rows, registry.Row{
			ID: a.Email,
			Cells: []string{
				a.Email,
				uihelp.Default(a.Name, "—"),
				uihelp.Default(a.Role, "—"),
				uihelp.Default(a.Status, "—"),
				ago(a.LastLogin),
			},
		})
	}
	return rows, len(rows), nil
}

func (accountResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "enable"},
		{Key: "d", Label: "disable", Destructive: true,
			Prompt: "Disable account {id}?"},
		{Key: "R", Label: "role", Form: []registry.FormField{
			{Key: "role", Label: "Role", Options: []string{"RO", "RW", "SU"}, Default: "RO"},
		}},
	}
}

func (accountResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "e":
		if err := svc.OrgAccountEnable(ctx, org, id); err != nil {
			return "", err
		}
		return "enabled " + id, nil
	case "d":
		if err := svc.OrgAccountDisable(ctx, org, id, false); err != nil {
			return "", err
		}
		return "disabled " + id, nil
	case "R":
		role := args["role"]
		if err := svc.OrgAccountSetRole(ctx, org, id, role); err != nil {
			return "", err
		}
		return "set role of " + id + " to " + role, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}
