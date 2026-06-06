package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// templateResource surfaces configuration templates and the actions that
// author them: create/update/delete, snippet and software-policy attachment,
// plus a drill-in to template-scoped variables. Describe powers the generic
// detail fallback, including the template's attached snippets.
type templateResource struct{}

func (templateResource) Kind() string  { return "template" }
func (templateResource) Title() string { return "Templates" }

func (templateResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 26},
		{Title: "POSITION", Width: 10},
		{Title: "SNIPPETS", Width: 10},
		{Title: "CREATED", Width: 0},
	}
}

func (templateResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.TemplateList(ctx, org, service.TemplateListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Templates))
	for _, t := range res.Templates {
		rows = append(rows, registry.Row{
			ID: t.Name,
			Cells: []string{
				t.Name,
				uihelp.Default(t.Position, "—"),
				fmt.Sprintf("%d", t.SnippetCount),
				ago(t.CreatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (templateResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "description", Label: "Description"},
			{Key: "position", Label: "Position", Options: []string{"PREPEND", "APPEND"}, Default: "PREPEND"},
		}},
		{Key: "u", Label: "update", Form: []registry.FormField{
			{Key: "name", Label: "New name", Placeholder: "(leave blank to keep)"},
			{Key: "description", Label: "Description"},
			{Key: "position", Label: "Position", Options: []string{"(unchanged)", "PREPEND", "APPEND"}, Default: "(unchanged)"},
		}},
		{Key: "s", Label: "add-snippet", Form: []registry.FormField{
			{Key: "snippet", Label: "Snippet", OptionsFrom: "addable-snippets", Required: true},
		}},
		{Key: "S", Label: "remove-snippet", Form: []registry.FormField{
			{Key: "snippet", Label: "Snippet", OptionsFrom: "template-snippets", Required: true},
		}},
		{Key: "w", Label: "add-software", Form: []registry.FormField{
			{Key: "policy", Label: "Policy", OptionsFrom: "addable-software", Required: true},
		}},
		{Key: "W", Label: "remove-software", Form: []registry.FormField{
			{Key: "policy", Label: "Policy", OptionsFrom: "template-software", Required: true},
		}},
		{Key: "V", Label: "variables", Nav: "variables"},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete template {id}?"},
	}
}

func (templateResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		opts := service.TemplateCreateOpts{Name: args["name"], Description: args["description"], Position: args["position"]}
		if _, err := svc.TemplateCreate(ctx, org, opts); err != nil {
			return "", err
		}
		return "created template " + args["name"], nil
	case "u":
		opts := service.TemplateUpdateOpts{NewName: args["name"], Description: args["description"]}
		if args["position"] != "(unchanged)" {
			opts.Position = args["position"]
		}
		if opts.NewName == "" && opts.Description == "" && opts.Position == "" {
			return "", fmt.Errorf("no updates specified")
		}
		newName, err := svc.TemplateUpdate(ctx, org, id, opts)
		if err != nil {
			return "", err
		}
		return "updated " + newName, nil
	case "s":
		if err := svc.TemplateAddSnippet(ctx, org, id, args["snippet"]); err != nil {
			return "", err
		}
		return "added snippet " + args["snippet"] + " to " + id, nil
	case "S":
		if err := svc.TemplateRemoveSnippet(ctx, org, id, args["snippet"]); err != nil {
			return "", err
		}
		return "removed snippet " + args["snippet"] + " from " + id, nil
	case "w":
		if err := svc.TemplateAddSoftwarePolicy(ctx, org, id, args["policy"]); err != nil {
			return "", err
		}
		return "added software " + args["policy"] + " to " + id, nil
	case "W":
		if err := svc.TemplateRemoveSoftwarePolicy(ctx, org, id, args["policy"]); err != nil {
			return "", err
		}
		return "removed software " + args["policy"] + " from " + id, nil
	case "x":
		if err := svc.TemplateDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// FormOptions implements registry.FormOptioner: it resolves the dynamic select
// fields used by the attach/detach actions, always returning names.
func (templateResource) FormOptions(ctx context.Context, svc *service.Service, org, id, actionKey, optionsFrom string) ([]string, error) {
	switch optionsFrom {
	case "addable-snippets":
		res, err := svc.SnippetList(ctx, org, service.SnippetListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Snippets))
		for _, s := range res.Snippets {
			names = append(names, s.Name)
		}
		return names, nil
	case "addable-software":
		res, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Policies))
		for _, p := range res.Policies {
			names = append(names, p.Name)
		}
		return names, nil
	case "template-snippets":
		t, err := svc.TemplateGet(ctx, org, id)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(t.Snippets))
		for _, s := range t.Snippets {
			names = append(names, s.Name)
		}
		return names, nil
	case "template-software":
		// models.Template doesn't carry its attached software policies, so the
		// attachment is read from the reverse mapping: each software policy's
		// single-policy GET populates TemplateNames (the list endpoint omits it).
		list, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(list.Policies))
		for _, p := range list.Policies {
			pol, err := svc.SoftwarePolicyGet(ctx, org, p.Name)
			if err != nil {
				return nil, err
			}
			for _, tn := range pol.TemplateNames {
				if tn == id {
					names = append(names, pol.Name)
					break
				}
			}
		}
		return names, nil
	}
	return nil, fmt.Errorf("unknown options source %q", optionsFrom)
}

// Navigate implements registry.Navigator: drill into the template's scoped
// variables.
func (templateResource) Navigate(org, id, nav string) (registry.Resource, bool) {
	switch nav {
	case "variables":
		return ScopedVarResource{Scope: service.VarScopeTemplate, Entity: id, Name: "Variables", KindID: "template-variable"}, true
	}
	return nil, false
}

// Describe implements registry.Describer.
func (templateResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	t, err := svc.TemplateGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: t.Name},
		{Label: "Position", Value: uihelp.Default(t.Position, "—")},
		{Label: "Snippets", Value: fmt.Sprintf("%d", t.SnippetCount)},
		{Label: "Created by", Value: uihelp.Default(t.CreatedBy, "—")},
		{Label: "Created", Value: fullTime(t.CreatedAt)},
		{Label: "Updated", Value: fullTime(t.UpdatedAt)},
	}
	sections := []registry.Section{{Title: "Template", Fields: fields}}
	if t.Description != "" {
		sections = append(sections, registry.Section{Title: "Description", Text: t.Description})
	}
	if len(t.Snippets) > 0 {
		names := make([]string, 0, len(t.Snippets))
		for _, s := range t.Snippets {
			names = append(names, s.Name)
		}
		sections = append(sections, registry.Section{Title: "Snippets", Text: strings.Join(names, "\n")})
	}
	return sections, nil
}
