package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/models"
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
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "display_name", Label: "Display name"},
			{Key: "description", Label: "Description"},
		}},
		{Key: "i", Label: "invite", Form: []registry.FormField{
			{Key: "email", Label: "Email", Required: true},
			{Key: "role", Label: "Role", Options: []string{"RO", "RW", "SU"}, Default: "RO"},
		}},
		{Key: "u", Label: "default-ou", Form: []registry.FormField{
			{Key: "ou", Label: "Default OU", OptionsFrom: "ous", Required: true},
		}},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt:      "Delete organization {id}? This cannot be undone.",
			BlastRadius: "Permanently deletes this organization and all its devices/templates."},
	}
}

func (orgResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		name := args["name"]
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		if _, err := svc.OrgCreate(ctx, name, args["display_name"], args["description"]); err != nil {
			return "", err
		}
		return "created org " + name, nil
	case "i":
		email := args["email"]
		if email == "" {
			return "", fmt.Errorf("email is required")
		}
		if err := svc.OrgInviteSend(ctx, id, email, args["role"]); err != nil {
			return "", err
		}
		return "invited " + email + " to " + id, nil
	case "u":
		ou := args["ou"]
		if ou == "" {
			return "", fmt.Errorf("default OU is required")
		}
		if err := svc.OrgSetDefaultOU(ctx, id, ou); err != nil {
			return "", err
		}
		return "set default OU of " + id + " to " + ou, nil
	case "x":
		if err := svc.OrgDelete(ctx, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// FormOptions implements registry.FormOptioner. It resolves the "ous" token used
// by the default-ou action into the list of OU names in the selected org (id).
func (orgResource) FormOptions(ctx context.Context, svc *service.Service, org, id, actionKey, optionsFrom string) ([]string, error) {
	switch optionsFrom {
	case "ous":
		res, err := svc.OUList(ctx, id, service.OUListOpts{PageSize: 500})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.OUs))
		for _, ou := range res.OUs {
			names = append(names, ou.Name)
		}
		return names, nil
	}
	return nil, fmt.Errorf("unknown options source %q", optionsFrom)
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
	// Best-effort quota roll-up; never let a quota fetch failure break describe.
	if q, err := svc.OrgQuota(ctx, id); err == nil && q != nil {
		plan := "—"
		if q.Plan != nil {
			plan = uihelp.Default(q.Plan.DisplayName, q.Plan.Name)
		}
		sections = append(sections, registry.Section{Title: "Quota", Fields: []registry.Field{
			{Label: "Plan", Value: uihelp.Default(plan, "—")},
			{Label: "Devices", Value: orgQuotaUsage(q.Devices)},
			{Label: "Users", Value: orgQuotaUsage(q.Users)},
			{Label: "VPN networks", Value: orgQuotaUsage(q.VpnNetworks)},
			{Label: "Snippets", Value: orgQuotaUsage(q.Snippets)},
		}})
	}
	return sections, nil
}

// orgQuotaUsage renders a "used / limit" usage string, or "used (unlimited)"
// for plans without a cap on that resource.
func orgQuotaUsage(q models.Quota) string {
	if q.Unlimited {
		return fmt.Sprintf("%d (unlimited)", q.Used)
	}
	return fmt.Sprintf("%d / %d", q.Used, q.Limit)
}
