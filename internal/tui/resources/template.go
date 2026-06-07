package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/netdefense-io/NDCLI/internal/models"
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
		// Only snippets not already attached — adding an attached one would just
		// fail with a duplicate error at the API.
		res, err := svc.SnippetList(ctx, org, service.SnippetListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		t, err := svc.TemplateGet(ctx, org, id)
		if err != nil {
			return nil, err
		}
		attached := make(map[string]bool, len(t.Snippets))
		for _, s := range t.Snippets {
			attached[s.Name] = true
		}
		names := make([]string, 0, len(res.Snippets))
		for _, s := range res.Snippets {
			if !attached[s.Name] {
				names = append(names, s.Name)
			}
		}
		return names, nil
	case "addable-software":
		// Only policies not already attached (see addable-snippets).
		res, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		attached, err := templateAttachedPolicySet(ctx, svc, org, id, res.Policies)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Policies))
		for _, p := range res.Policies {
			if !attached[p.Name] {
				names = append(names, p.Name)
			}
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
		list, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{PerPage: 100})
		if err != nil {
			return nil, err
		}
		attached, err := templateAttachedPolicySet(ctx, svc, org, id, list.Policies)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(attached))
		for n := range attached {
			names = append(names, n)
		}
		sort.Strings(names)
		return names, nil
	}
	return nil, fmt.Errorf("unknown options source %q", optionsFrom)
}

// templateAttachedPolicySet returns the set of software-policy names (from the
// given list) attached to the template. models.Template doesn't carry its
// attached policies, so attachment is read from the reverse mapping: each
// policy's single-policy GET populates TemplateNames (the list endpoint omits
// it). The per-policy GETs run concurrently (bounded) so the picker stays
// responsive even with many policies.
func templateAttachedPolicySet(ctx context.Context, svc *service.Service, org, template string, policies []models.SoftwarePolicy) (map[string]bool, error) {
	var (
		mu       sync.Mutex
		attached = make(map[string]bool)
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, 8)
	)
	for _, p := range policies {
		name := p.Name
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			pol, err := svc.SoftwarePolicyGet(ctx, org, name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, tn := range pol.TemplateNames {
				if tn == template {
					attached[name] = true
					break
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return attached, nil
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
