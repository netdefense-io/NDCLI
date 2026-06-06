package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// orgResource surfaces the organizations the caller can see. Switching the
// active org is handled by the org switcher (press "o"), not by an action
// here; the only mutating action is delete.
type orgResource struct{}

func (orgResource) Kind() string  { return "org" }
func (orgResource) Title() string { return "Organizations" }

func (orgResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 26},
		{Title: "ROLE", Width: 8},
		{Title: "STATUS", Width: 12},
		{Title: "DEFAULT OU", Width: 20},
		{Title: "CREATED", Width: 0},
	}
}

func (orgResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.OrgList(ctx, service.OrgListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Orgs))
	for _, o := range res.Orgs {
		rows = append(rows, registry.Row{
			ID: o.Name,
			Cells: []string{
				o.Name,
				uihelp.Default(o.GetRole(), "—"),
				uihelp.Default(o.Status, "—"),
				uihelp.Default(o.GetDefaultOU(), "—"),
				ago(o.CreatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (orgResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "x", Label: "delete", Destructive: true,
			Prompt:      "Delete organization {id}? This cannot be undone.",
			BlastRadius: "Permanently deletes this organization and all its devices/templates."},
	}
}

func (orgResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.OrgDelete(ctx, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (orgResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	o, err := svc.OrgGet(ctx, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: o.Name},
		{Label: "Display name", Value: uihelp.Default(o.DisplayName, "—")},
		{Label: "Plan", Value: o.GetPlan()},
		{Label: "Your role", Value: uihelp.Default(o.GetRole(), "—")},
		{Label: "Status", Value: uihelp.Default(o.Status, "—")},
		{Label: "Default OU", Value: uihelp.Default(o.GetDefaultOU(), "—")},
		{Label: "Devices", Value: fmt.Sprintf("%d", o.DeviceCount)},
		{Label: "Members", Value: fmt.Sprintf("%d", o.MemberCount)},
		{Label: "Created", Value: fullTime(o.CreatedAt)},
	}
	sections := []registry.Section{{Title: "Organization", Fields: fields}}
	if o.Description != "" {
		sections = append(sections, registry.Section{Title: "Description", Text: o.Description})
	}
	return sections, nil
}
