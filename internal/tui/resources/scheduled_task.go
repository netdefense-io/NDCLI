package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// ScheduledTaskResource is the child list reached by drilling into a schedule's
// "tasks" (the registry.Navigator on scheduleResource). It lists the task specs
// registered under one schedule and exposes per-task enable/disable/remove. The
// row id is the task CODE. No Describer — the list cells carry everything.
type ScheduledTaskResource struct {
	Schedule string
}

func (ScheduledTaskResource) Kind() string  { return "scheduled-task" }
func (ScheduledTaskResource) Title() string { return "Scheduled Tasks" }

func (ScheduledTaskResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "CODE", Width: 12},
		{Title: "KIND", Width: 8},
		{Title: "ENABLED", Width: 8},
		{Title: "LAST FIRED", Width: 0},
		{Title: "CREATED BY", Width: 0},
	}
}

func (r ScheduledTaskResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	tasks, err := svc.ScheduleTaskList(ctx, org, r.Schedule)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, registry.Row{
			ID: t.Code,
			Cells: []string{
				t.Code,
				t.Kind,
				yesNo(t.Enabled),
				agoPtr(t.LastFiredAt),
				uihelp.Default(t.CreatedBy, "—"),
			},
		})
	}
	// ScheduleTaskList is unpaginated — it returns every task spec for the
	// schedule in one call — so len(tasks) is the true total.
	return rows, len(tasks), nil
}

func (ScheduledTaskResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "enable"},
		{Key: "d", Label: "disable", Destructive: true,
			Prompt: "Disable scheduled task {id}?"},
		{Key: "x", Label: "remove", Destructive: true,
			Prompt: "Remove scheduled task {id}?"},
	}
}

func (ScheduledTaskResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "e":
		if _, err := svc.ScheduledTaskSetEnabledByCode(ctx, org, id, true); err != nil {
			return "", err
		}
		return "enabled " + id, nil
	case "d":
		if _, err := svc.ScheduledTaskSetEnabledByCode(ctx, org, id, false); err != nil {
			return "", err
		}
		return "disabled " + id, nil
	case "x":
		if err := svc.ScheduledTaskRemoveByCode(ctx, org, id); err != nil {
			return "", err
		}
		return "removed " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}
