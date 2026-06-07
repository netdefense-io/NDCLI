package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// ouResource surfaces the organizational units within the active org and
// exposes the full OU mutation surface: create/rename/delete, device and
// template membership, plus a drill-in to OU-scoped variables.
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
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "description", Label: "Description"},
		}},
		{Key: "m", Label: "rename", Form: []registry.FormField{
			{Key: "new_name", Label: "New name", Required: true},
		}},
		{Key: "d", Label: "add-device", Form: []registry.FormField{
			{Key: "device", Label: "Device", Placeholder: "device name", Required: true},
		}},
		{Key: "D", Label: "remove-device", Form: []registry.FormField{
			{Key: "device", Label: "Device", OptionsFrom: "ou-devices", Required: true},
		}},
		{Key: "t", Label: "add-template", Form: []registry.FormField{
			{Key: "template", Label: "Template", OptionsFrom: "addable-templates", Required: true},
		}},
		{Key: "T", Label: "remove-template", Form: []registry.FormField{
			{Key: "template", Label: "Template", OptionsFrom: "ou-templates", Required: true},
		}},
		{Key: "V", Label: "variables", Nav: "variables"},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete OU {id}? (blocked if devices remain in it)"},
	}
}

func (ouResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		if _, err := svc.OUCreate(ctx, org, args["name"], args["description"]); err != nil {
			return "", err
		}
		return "created OU " + args["name"], nil
	case "m":
		if _, err := svc.OURename(ctx, org, id, args["new_name"]); err != nil {
			return "", err
		}
		return "renamed " + id + " to " + args["new_name"], nil
	case "d":
		if err := svc.OUAddDevice(ctx, org, id, args["device"]); err != nil {
			return "", err
		}
		return "added " + args["device"] + " to " + id, nil
	case "D":
		if err := svc.OURemoveDevice(ctx, org, id, args["device"]); err != nil {
			return "", err
		}
		return "removed " + args["device"] + " from " + id, nil
	case "t":
		if err := svc.OUTemplateAdd(ctx, org, id, args["template"]); err != nil {
			return "", err
		}
		return "added template " + args["template"] + " to " + id, nil
	case "T":
		if err := svc.OUTemplateRemove(ctx, org, id, args["template"]); err != nil {
			return "", err
		}
		return "removed template " + args["template"] + " from " + id, nil
	case "x":
		if err := svc.OUDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// FormOptions implements registry.FormOptioner: it resolves the dynamic select
// fields used by the membership actions, always returning names (never ids).
func (ouResource) FormOptions(ctx context.Context, svc *service.Service, org, id, actionKey, optionsFrom string) ([]string, error) {
	switch optionsFrom {
	case "ou-devices":
		res, err := svc.OUDeviceList(ctx, org, id)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Devices))
		for _, d := range res.Devices {
			names = append(names, d.Name)
		}
		return names, nil
	case "addable-templates":
		// All org templates minus the ones already on this OU (re-adding an
		// attached template would just fail with a duplicate error). PerPage 100
		// is the NDManager cap for the templates endpoint.
		res, err := svc.TemplateList(ctx, org, service.TemplateListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		attached, err := svc.OUTemplateList(ctx, org, id)
		if err != nil {
			return nil, err
		}
		have := make(map[string]bool, len(attached.Items))
		for _, t := range attached.Items {
			have[t.Name] = true
		}
		names := make([]string, 0, len(res.Templates))
		for _, t := range res.Templates {
			if !have[t.Name] {
				names = append(names, t.Name)
			}
		}
		return names, nil
	case "ou-templates":
		res, err := svc.OUTemplateList(ctx, org, id)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Items))
		for _, t := range res.Items {
			names = append(names, t.Name)
		}
		return names, nil
	}
	return nil, fmt.Errorf("unknown options source %q", optionsFrom)
}

// Navigate implements registry.Navigator: drill into the OU's scoped variables.
func (ouResource) Navigate(org, id, nav string) (registry.Resource, bool) {
	switch nav {
	case "variables":
		return ScopedVarResource{Scope: service.VarScopeOU, Entity: id, Name: "Variables", KindID: "ou-variable"}, true
	}
	return nil, false
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
