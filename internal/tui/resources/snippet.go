package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// snippetResource surfaces configuration snippets. It is read-only in the TUI
// for v1 — editing content and priority is handled by the cli. Describe powers
// the generic detail fallback via SnippetGet.
type snippetResource struct{}

func (snippetResource) Kind() string  { return "snippet" }
func (snippetResource) Title() string { return "Snippets" }

func (snippetResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 28},
		{Title: "PRIORITY", Width: 10},
		{Title: "TYPE", Width: 16},
		{Title: "UPDATED", Width: 0},
	}
}

func (snippetResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.SnippetList(ctx, org, service.SnippetListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Snippets))
	for _, sn := range res.Snippets {
		rows = append(rows, registry.Row{
			ID: sn.Name,
			Cells: []string{
				sn.Name,
				fmt.Sprintf("%d", sn.Priority),
				uihelp.Default(sn.Type, "—"),
				ago(sn.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (snippetResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "edit", Shell: []string{"snippet", "edit", "{id}", "-o", "{org}"}},
		{Key: "x", Label: "delete", Destructive: true, Prompt: "Delete snippet {id}?"},
	}
}

func (snippetResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.SnippetDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (snippetResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	sn, err := svc.SnippetGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: sn.Name},
		{Label: "Type", Value: uihelp.Default(sn.Type, "—")},
		{Label: "Priority", Value: fmt.Sprintf("%d", sn.Priority)},
		{Label: "Organization", Value: uihelp.Default(sn.Organization, "—")},
		{Label: "Created", Value: fullTime(sn.CreatedAt)},
		{Label: "Updated", Value: fullTime(sn.UpdatedAt)},
	}
	sections := []registry.Section{{Title: "Snippet", Fields: fields}}
	if sn.Content != "" {
		sections = append(sections, registry.Section{Title: "Content", Text: sn.Content})
	}
	return sections, nil
}
