package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// deviceResource surfaces managed firewall devices. Drilling into a row opens
// the bespoke health screen (handled by the app); Describe powers the generic
// detail fallback. Connect/ping/firmware are parameterised: connect shells out
// to the ndcli binary, ping and firmware collect a small form.
type deviceResource struct{}

func (deviceResource) Kind() string  { return "device" }
func (deviceResource) Title() string { return "Devices" }

func (deviceResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 24},
		{Title: "STATUS", Width: 10},
		{Title: "ONLINE", Width: 8},
		{Title: "OU", Width: 16},
		{Title: "VERSION", Width: 12},
		{Title: "HEARTBEAT", Width: 10},
		{Title: "DRIFT", Width: 0},
	}
}

func (deviceResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.DeviceList(ctx, org, service.DeviceListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Devices))
	for _, d := range res.Devices {
		rows = append(rows, registry.Row{
			ID: d.Name,
			Cells: []string{
				d.Name,
				d.Status,
				uihelp.OnlineLabel(d.Online),
				d.GetOUsDisplay(),
				uihelp.Default(d.Version, "—"),
				relAge(d.Heartbeat),
				uihelp.Default(d.DriftStatus, "—"),
			},
		})
	}
	return rows, res.Total, nil
}

func (deviceResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "a", Label: "approve"},
		{Key: "A", Label: "approve-all", TargetsAll: true, Destructive: true,
			Prompt:      "Approve ALL pending devices in this organization?",
			BlastRadius: "Approves every PENDING device in this organization."},
		{Key: "R", Label: "restart", Destructive: true,
			Prompt: "Restart device {id}? It will reboot."},
		{Key: "P", Label: "poweroff", Destructive: true,
			Prompt: "Power off device {id}? It will shut down."},
		{Key: "p", Label: "ping", Form: []registry.FormField{
			{Key: "host", Label: "Host/IP", Required: true, Placeholder: "1.1.1.1"},
			{Key: "count", Label: "Count", Default: "4"},
		}},
		{Key: "f", Label: "firmware", Form: []registry.FormField{
			{Key: "mode", Label: "Mode", Options: []string{"minor", "major"}},
			{Key: "reboot", Label: "Reboot", Options: []string{"yes", "no"}},
			{Key: "dry_run", Label: "Dry run", Options: []string{"no", "yes"}},
		}},
		{Key: "c", Label: "connect", Shell: []string{"device", "connect", "{id}", "-o", "{org}"}},
		{Key: "s", Label: "sync", Prompt: "Sync (apply the rendered config) to {id}?"},
		{Key: "x", Label: "remove", Destructive: true,
			Prompt: "Remove device {id} from management? This cannot be undone."},
	}
}

func (deviceResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "a":
		if err := svc.DeviceApprove(ctx, org, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("approved %s", id), nil
	case "A":
		results, err := svc.DeviceApproveAll(ctx, org)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return "no pending devices to approve", nil
		}
		ok, failed := 0, 0
		for _, r := range results {
			if r.Err != nil {
				failed++
			} else {
				ok++
			}
		}
		if failed > 0 {
			return "", fmt.Errorf("approved %d, %d failed", ok, failed)
		}
		return fmt.Sprintf("approved %d device%s", ok, uihelp.Plural(ok)), nil
	case "R":
		if _, err := svc.Run(ctx, org, service.RunOpts{Type: models.TaskTypeReboot, Devices: []string{id}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("restart task queued for %s", id), nil
	case "P":
		if _, err := svc.Run(ctx, org, service.RunOpts{Type: models.TaskTypeShutdown, Devices: []string{id}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("poweroff task queued for %s", id), nil
	case "p":
		return devicePing(ctx, svc, org, id, args)
	case "f":
		return deviceFirmware(ctx, svc, org, id, args)
	case "s":
		res, err := svc.SyncApply(ctx, org, service.SyncFilter{Organization: org, Device: id}, false)
		if err != nil {
			return "", err
		}
		return summarizeSync(res), nil
	case "x":
		if err := svc.DeviceRemove(ctx, org, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("removed %s", id), nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

func devicePing(ctx context.Context, svc *service.Service, org, id string, args map[string]string) (string, error) {
	host := args["host"]
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	payload := map[string]interface{}{"target": host}
	if c := args["count"]; c != "" && c != "4" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 1 || n > 1000 {
			return "", fmt.Errorf("count must be a number between 1 and 1000")
		}
		payload["count"] = n
	}
	if _, err := svc.Run(ctx, org, service.RunOpts{Type: models.TaskTypePing, Devices: []string{id}, Payload: payload}); err != nil {
		return "", err
	}
	return fmt.Sprintf("ping %s queued for %s", host, id), nil
}

func deviceFirmware(ctx context.Context, svc *service.Service, org, id string, args map[string]string) (string, error) {
	mode := args["mode"]
	if mode != "minor" && mode != "major" {
		return "", fmt.Errorf(`mode must be "minor" or "major"`)
	}
	reboot := args["reboot"] != "no"
	dryRun := args["dry_run"] == "yes"
	if mode == "major" && !reboot {
		return "", fmt.Errorf("major firmware upgrades require a reboot")
	}
	payload := map[string]interface{}{
		"mode":        mode,
		"reboot":      reboot,
		"check_first": true,
		"dry_run":     dryRun,
	}
	if _, err := svc.Run(ctx, org, service.RunOpts{Type: models.TaskTypeFirmwareUpgrade, Devices: []string{id}, Payload: payload}); err != nil {
		return "", err
	}
	return fmt.Sprintf("firmware %s upgrade queued for %s", mode, id), nil
}

// Describe implements registry.Describer.
func (deviceResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	d, err := svc.DeviceGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "Name", Value: d.Name},
		{Label: "UUID", Value: d.UUID},
		{Label: "Status", Value: d.Status},
		{Label: "Online", Value: uihelp.OnlineLabel(d.Online)},
		{Label: "Organization", Value: d.Organization},
		{Label: "OUs", Value: d.GetOUsDisplay()},
		{Label: "Agent version", Value: uihelp.Default(d.Version, "—")},
		{Label: "Heartbeat", Value: fullTime(d.Heartbeat)},
		{Label: "Drift status", Value: uihelp.Default(d.DriftStatus, "—")},
		{Label: "Auto-sync", Value: fmt.Sprintf("%t", d.AutoSync)},
		{Label: "Created", Value: fullTime(d.CreatedAt)},
	}
	if d.SyncedAt != nil {
		fields = append(fields, registry.Field{Label: "Synced at", Value: agoPtr(d.SyncedAt)})
	}
	return []registry.Section{{Title: "Device", Fields: fields}}, nil
}
