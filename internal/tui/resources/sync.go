package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// syncResource surfaces per-device sync status and exposes the sync-apply
// action (single device or the whole org).
type syncResource struct{}

func (syncResource) Kind() string  { return "sync" }
func (syncResource) Title() string { return "Sync Status" }

func (syncResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "DEVICE", Width: 26},
		{Title: "OU", Width: 18},
		{Title: "AUTO-SYNC", Width: 10},
		{Title: "SYNCED", Width: 10},
		{Title: "STATUS", Width: 0},
	}
}

func (syncResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.SyncStatus(ctx, org, service.SyncFilter{Organization: org})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Items))
	for _, s := range res.Items {
		autoSync := "no"
		if s.AutoSync {
			autoSync = "yes"
		}
		status := "drift"
		if s.InSync {
			status = "in-sync"
		}
		if s.Error != nil && *s.Error != "" {
			status = "error"
		}
		rows = append(rows, registry.Row{
			ID: s.DeviceName,
			Cells: []string{
				s.DeviceName,
				s.GetOUsDisplay(),
				autoSync,
				relAgePtr(s.SyncedAt),
				status,
			},
		})
	}
	return rows, res.Total, nil
}

func (syncResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "s", Label: "sync apply", Destructive: true,
			Prompt: "Apply sync to {id}?"},
		{Key: "S", Label: "sync all", TargetsAll: true, Destructive: true,
			Prompt:      "Apply sync to ALL drifted devices in this organization?",
			BlastRadius: "Pushes the rendered config to every drifted device in this organization."},
	}
}

func (syncResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "s":
		res, err := svc.SyncApply(ctx, org, service.SyncFilter{Organization: org, Device: id}, false)
		if err != nil {
			return "", err
		}
		return summarizeSync(res), nil
	case "S":
		res, err := svc.SyncApply(ctx, org, service.SyncFilter{Organization: org}, false)
		if err != nil {
			return "", err
		}
		return summarizeSync(res), nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

func summarizeSync(res *service.SyncApplyResult) string {
	r := res.Response
	if r == nil {
		return "sync requested"
	}
	msg := fmt.Sprintf("sync: %d affected, %d task(s)", r.DevicesAffected, len(r.Tasks))
	if len(r.Errors) > 0 {
		msg += fmt.Sprintf(", %d error(s)", len(r.Errors))
	}
	return msg
}
