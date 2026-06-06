package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// ouResource surfaces the organizational units within the active org. It is
// read-only in the TUI for v1 — OU mutations (create/rename/delete, device and
// template membership) are handled by the cli surface, not by actions here.
type ouResource struct{}

func (ouResource) Kind() string  { return "ou" }
func (ouResource) Title() string { return "Organizational Units" }

func (ouResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 24},
		{Title: "STATUS", Width: 12},
		{Title: "DEVICES", Width: 8},
		{Title: "TEMPLATES", Width: 10},
		{Title: "PARENT", Width: 0},
	}
}

func (ouResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.OUList(ctx, org, service.OUListOpts{Page: page, PageSize: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.OUs))
	for _, ou := range res.OUs {
		rows = append(rows, registry.Row{
			ID: ou.Name,
			Cells: []string{
				ou.Name,
				uihelp.Default(ou.Status, "—"),
				fmt.Sprintf("%d", ou.GetDeviceCount()),
				fmt.Sprintf("%d", ou.TemplateCount),
				uihelp.Default(ou.ParentOU, "—"),
			},
		})
	}
	return rows, res.Total, nil
}

func (ouResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete OU {id}? (blocked if devices remain in it)"},
	}
}

func (ouResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.OUDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (ouResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	ou, err := svc.OUGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: ou.Name},
		{Label: "Display name", Value: uihelp.Default(ou.DisplayName, "—")},
		{Label: "Organization", Value: ou.Organization},
		{Label: "Status", Value: uihelp.Default(ou.Status, "—")},
		{Label: "Parent OU", Value: uihelp.Default(ou.ParentOU, "—")},
		{Label: "Devices", Value: fmt.Sprintf("%d", ou.GetDeviceCount())},
		{Label: "Templates", Value: fmt.Sprintf("%d", ou.TemplateCount)},
		{Label: "Created", Value: fullTime(ou.CreatedAt)},
	}
	sections := []registry.Section{{Title: "Organizational Unit", Fields: fields}}
	if ou.Description != "" {
		sections = append(sections, registry.Section{Title: "Description", Text: ou.Description})
	}
	return sections, nil
}
