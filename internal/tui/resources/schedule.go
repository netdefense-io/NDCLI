package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// scheduleResource surfaces the org's cadence schedules (cron + timezone). It
// is read-only in the TUI for v1 — enable/disable, create and delete live on
// the `ndcli schedule` CLI. Describe powers the generic detail fallback and
// includes the nested scheduled-task specs via ScheduleGet.
type scheduleResource struct{}

func (scheduleResource) Kind() string  { return "schedule" }
func (scheduleResource) Title() string { return "Schedules" }

func (scheduleResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 24},
		{Title: "CRON", Width: 18},
		{Title: "TZ", Width: 16},
		{Title: "ENABLED", Width: 9},
		{Title: "CREATED", Width: 0},
	}
}

func (scheduleResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	// ScheduleList is not paginated; page/perPage are ignored.
	res, err := svc.ScheduleList(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Schedules))
	for _, s := range res.Schedules {
		rows = append(rows, registry.Row{
			ID: s.Name,
			Cells: []string{
				s.Name,
				uihelp.Truncate(s.Schedule, 18),
				uihelp.Default(s.Timezone, "—"),
				enabledLabel(s.Enabled),
				ago(s.CreatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (scheduleResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "enable"},
		{Key: "d", Label: "disable", Destructive: true,
			Prompt: "Disable schedule {id}?"},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete schedule {id}? Removes all its task specs."},
	}
}

func (scheduleResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "e":
		if _, err := svc.ScheduleSetEnabled(ctx, org, id, true); err != nil {
			return "", err
		}
		return "enabled " + id, nil
	case "d":
		if _, err := svc.ScheduleSetEnabled(ctx, org, id, false); err != nil {
			return "", err
		}
		return "disabled " + id, nil
	case "x":
		if err := svc.ScheduleDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (scheduleResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	sch, err := svc.ScheduleGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: sch.Name},
		{Label: "Cron", Value: uihelp.Default(sch.Schedule, "—")},
		{Label: "Timezone", Value: uihelp.Default(sch.Timezone, "—")},
		{Label: "Enabled", Value: enabledLabel(sch.Enabled)},
		{Label: "Last fired", Value: agoPtr(sch.LastFiredAt)},
		{Label: "Next run", Value: agoPtr(sch.NextRunAt)},
		{Label: "Created by", Value: uihelp.Default(sch.CreatedBy, "—")},
		{Label: "Created", Value: fullTime(sch.CreatedAt)},
		{Label: "Updated", Value: fullTime(sch.UpdatedAt)},
	}
	sections := []registry.Section{{Title: "Schedule", Fields: fields}}

	if len(sch.ScheduledTasks) > 0 {
		taskFields := make([]registry.Field, 0, len(sch.ScheduledTasks))
		for _, t := range sch.ScheduledTasks {
			taskFields = append(taskFields, registry.Field{
				Label: t.Code,
				Value: fmt.Sprintf("%s · %s", t.Kind, enabledLabel(t.Enabled)),
			})
		}
		sections = append(sections, registry.Section{Title: "Registered tasks", Fields: taskFields})
	}
	return sections, nil
}

// enabledLabel renders a plain on/off label for list cells and detail fields.
func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
