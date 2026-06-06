package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// templateResource surfaces configuration templates. It is read-only in the
// TUI for v1 — templates are authored via the cli/MCP surfaces. Describe powers
// the generic detail fallback, including the template's attached snippets.
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
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete template {id}?"},
	}
}

func (templateResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.TemplateDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
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
