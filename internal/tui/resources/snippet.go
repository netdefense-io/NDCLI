package resources

import (
	"context"
	"fmt"
	"strconv"

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
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "type", Label: "Type", Default: "USER", Options: []string{
				"USER", "GROUP", "ALIAS", "RULE",
				"UNBOUND_HOST_OVERRIDE", "UNBOUND_DOMAIN_FORWARD",
				"UNBOUND_HOST_ALIAS", "UNBOUND_ACL",
				"ZABBIX_SETTINGS", "ZABBIX_USERPARAMETER", "ZABBIX_ALIAS",
			}},
			{Key: "content", Label: "Content", Required: true},
			{Key: "priority", Label: "Priority", Default: "1000"},
		}},
		{Key: "e", Label: "edit", Shell: []string{"snippet", "edit", "{id}", "-o", "{org}"}},
		{Key: "m", Label: "rename", Form: []registry.FormField{
			{Key: "new_name", Label: "New name", Required: true},
		}},
		{Key: "p", Label: "priority", Form: []registry.FormField{
			{Key: "priority", Label: "Priority", Required: true, Placeholder: "1-60000"},
		}},
		{Key: "x", Label: "delete", Destructive: true, Prompt: "Delete snippet {id}?"},
	}
}

func (snippetResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		prio := 0
		if p := args["priority"]; p != "" {
			n, err := strconv.Atoi(p)
			if err != nil {
				return "", fmt.Errorf("invalid priority %q: %w", p, err)
			}
			prio = n
		}
		if _, err := svc.SnippetCreate(ctx, org, service.SnippetCreateOpts{
			Name:     args["name"],
			Type:     args["type"],
			Content:  args["content"],
			Priority: prio,
		}); err != nil {
			return "", err
		}
		return "created snippet " + args["name"], nil
	case "m":
		newName := args["new_name"]
		if err := svc.SnippetRename(ctx, org, id, newName); err != nil {
			return "", err
		}
		return "renamed " + id + " to " + newName, nil
	case "p":
		prio, err := strconv.Atoi(args["priority"])
		if err != nil {
			return "", fmt.Errorf("invalid priority %q: %w", args["priority"], err)
		}
		if err := svc.SnippetSetPriority(ctx, org, id, prio); err != nil {
			return "", err
		}
		return "set priority of " + id + " to " + args["priority"], nil
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
