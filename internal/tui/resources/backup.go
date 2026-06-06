package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// backupResource surfaces the per-device backup status across the org. The
// list exposes per-device enable/disable; managing encryption keys stays on
// the CLI. Describe powers the generic detail fallback via BackupStatusGet.
type backupResource struct{}

func (backupResource) Kind() string  { return "backup" }
func (backupResource) Title() string { return "Backup Status" }

func (backupResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "DEVICE", Width: 24},
		{Title: "BACKUP", Width: 10},
		{Title: "KEY", Width: 10},
		{Title: "LAST", Width: 12},
		{Title: "STATUS", Width: 0},
	}
}

func (backupResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.BackupStatusList(ctx, org, service.BackupStatusOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Items))
	for _, b := range res.Items {
		rows = append(rows, registry.Row{
			ID: b.DeviceName,
			Cells: []string{
				b.DeviceName,
				backupEnabledLabel(b.Enabled),
				backupKeyLabel(b.HasEncryptionKeyOverride),
				agoPtr(b.LastBackupAt),
				uihelp.Default(b.LastBackupStatus, "—"),
			},
		})
	}
	return rows, res.Total, nil
}

func (backupResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "e", Label: "enable"},
		{Key: "d", Label: "disable", Destructive: true,
			Prompt: "Disable backups for {id}?"},
	}
}

func (backupResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "e":
		if err := svc.BackupSetEnabled(ctx, org, id, true); err != nil {
			return "", err
		}
		return "backups enabled for " + id, nil
	case "d":
		if err := svc.BackupSetEnabled(ctx, org, id, false); err != nil {
			return "", err
		}
		return "backups disabled for " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (backupResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	b, err := svc.BackupStatusGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Device", Value: b.DeviceName},
		{Label: "Backup", Value: backupEnabledLabel(b.Enabled)},
		{Label: "Encryption key", Value: backupKeyLabel(b.HasEncryptionKeyOverride)},
		{Label: "Last backup", Value: backupLastBackup(b.LastBackupAt)},
		{Label: "Last status", Value: uihelp.Default(b.LastBackupStatus, "—")},
		{Label: "Organization", Value: uihelp.Default(b.Organization, "—")},
	}
	sections := []registry.Section{{Title: "Backup", Fields: fields}}
	if b.LastBackupMessage != "" {
		sections = append(sections, registry.Section{Title: "Last message", Text: b.LastBackupMessage})
	}
	return sections, nil
}

// backupEnabledLabel renders the per-device backup toggle.
func backupEnabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// backupKeyLabel reports which encryption key the device backs up with:
// "custom" when a per-device override is set, otherwise the org-default key.
func backupKeyLabel(override bool) string {
	if override {
		return "custom"
	}
	return "org"
}

// backupLastBackup renders the nilable last-backup timestamp for the detail
// view (fullTime takes a value; LastBackupAt is a pointer).
func backupLastBackup(t *models.FlexibleTime) string {
	if t == nil {
		return "—"
	}
	return fullTime(*t)
}
