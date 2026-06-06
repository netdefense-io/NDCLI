package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// softwareResource surfaces software policies — reusable package inventory
// documents (present/required + absent/blocked) attached to templates. It is
// read-only in the TUI for v1; mutation (require/block/waive) stays on the cli.
// Describe powers the generic detail fallback via the single-policy GET.
type softwareResource struct{}

func (softwareResource) Kind() string  { return "software" }
func (softwareResource) Title() string { return "Software Policies" }

func (softwareResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 28},
		{Title: "PRESENT", Width: 10},
		{Title: "ABSENT", Width: 10},
		{Title: "UPDATED", Width: 0},
	}
}

func (softwareResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Policies))
	for _, p := range res.Policies {
		present, absent := policyCounts(p.Content)
		rows = append(rows, registry.Row{
			ID: p.Name,
			Cells: []string{
				p.Name,
				fmt.Sprintf("%d", present),
				fmt.Sprintf("%d", absent),
				ago(p.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (softwareResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "edit", Shell: []string{"software", "edit", "{id}", "-o", "{org}"}},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete software policy {id}?"},
	}
}

func (softwareResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.SoftwarePolicyDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (softwareResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	p, err := svc.SoftwarePolicyGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	content, _ := models.ParseSoftwarePolicyContent(p.Content)
	if content == nil {
		content = &models.SoftwarePolicyContent{}
	}
	fields := []registry.Field{
		{Label: "Name", Value: p.Name},
		{Label: "Organization", Value: uihelp.Default(p.Organization, "—")},
		{Label: "Present", Value: fmt.Sprintf("%d", len(content.Present))},
		{Label: "Absent", Value: fmt.Sprintf("%d", len(content.Absent))},
		{Label: "Templates", Value: uihelp.Default(strings.Join(p.TemplateNames, ", "), "—")},
		{Label: "Created", Value: fullTime(p.CreatedAt)},
		{Label: "Updated", Value: fullTime(p.UpdatedAt)},
	}
	sections := []registry.Section{{Title: "Software Policy", Fields: fields}}
	if len(content.Present) > 0 {
		sections = append(sections, registry.Section{
			Title: "Required packages",
			Text:  strings.Join(content.Present, "\n"),
		})
	}
	if len(content.Absent) > 0 {
		sections = append(sections, registry.Section{
			Title: "Blocked packages",
			Text:  strings.Join(content.Absent, "\n"),
		})
	}
	return sections, nil
}

// policyCounts parses a policy's content document and returns the number of
// present/required and absent/blocked packages. List responses omit Content,
// so a missing or unparseable document yields zero counts rather than an error.
func policyCounts(raw string) (present, absent int) {
	c, err := models.ParseSoftwarePolicyContent(raw)
	if err != nil || c == nil {
		return 0, 0
	}
	return len(c.Present), len(c.Absent)
}
