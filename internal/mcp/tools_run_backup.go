package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// backupRunMCPVocabulary is how this surface names the way forward from a
// refused on-demand backup; service.BackupRunAdvice decides which refusal gets
// which. The configuration carries storage secrets, so no tool creates it.
var backupRunMCPVocabulary = service.BackupRunVocabulary{
	ConfigCreate: "`ndcli backup config set` on the command line (the configuration carries storage secrets, so no tool creates it)",
	ConfigEnable: "ndcli.backup.config_enable",
	DeviceEnable: "ndcli.backup.enable",
}

// runBackupInput is the input of ndcli.run.backup. Only the targeting fields
// and confirm are in the tool's schema. At, Schedule, ScheduledAt and Payload
// are read only to be refused: every sibling ndcli.run.* tool takes `at`, so a
// caller may pass it here, and ignoring it would start the backup now when a
// deferred one was asked for.
type runBackupInput struct {
	Organization string   `json:"organization,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	OUs          []string `json:"ous,omitempty"`
	Org          bool     `json:"org,omitempty"`
	Confirm      bool     `json:"confirm,omitempty"`

	At          string          `json:"at,omitempty"`
	Schedule    string          `json:"schedule,omitempty"`
	ScheduledAt string          `json:"scheduled_at,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// backupSkipCodeGuide lists the codes a skipped device can carry, for the tool
// description. The list is open: a newer control plane may add codes.
const backupSkipCodeGuide = "BACKUP_NOT_ENABLED (backup is off for the device), " +
	"DEVICE_NOT_ENABLED (the device is disabled or waiting for approval, so no task can be sent to it), " +
	"NO_ENCRYPTION_KEY (neither the device nor the organization has a backup encryption key), " +
	"BACKUP_IN_FLIGHT (a backup of the device is already waiting or running), " +
	"TASK_CREATE_FAILED (the task could not be created; repeat the request)"

// registerRunBackupTool registers ndcli.run.backup, the LLM-facing twin of
// `ndcli run backup`. It is not built by makeRunHandler: a backup does not go
// through POST /tasks (which refuses BACKUP) but through its own route, and it
// has no payload, time or schedule, so it takes none of the scheduling
// parameters the other ndcli.run.* tools share.
func (s *Server) registerRunBackupTool() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.run.backup",
		Description: "Back up devices now: start a configuration backup on the targeted devices. " +
			"Only devices that have backup enabled are backed up. A targeted device that has backup off, is disabled or " +
			"waiting for approval, has a backup already waiting or running, or has no encryption key is not, and comes back in `skipped` with a " +
			"`code` and a `reason` (" + backupSkipCodeGuide + "); show the reason as it is, and treat a code not " +
			"listed as a skip too. A request that starts no backup is an error, BACKUP_NO_ELIGIBLE_DEVICES, whose " +
			"data carries the same `skipped` list. Name devices or ous, or set org=true for every device that has " +
			"backup enabled; one of the three is required. The backup starts now and cannot be deferred or " +
			"scheduled: there is no `at`, `timezone` or `schedule` parameter, and one is refused. Recurring " +
			"backups are set with ndcli.backup.config_set_schedule. Each task expires a minute after it is " +
			"created, so an offline device ends EXPIRED instead of backing up later; follow the tasks with " +
			"ndcli.task.list (type BACKUP). The encryption key is resolved by the control plane and is never " +
			"passed here. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"devices":      stringArrayProperty("Device names to back up (repeatable). A device without backup enabled is skipped, not backed up."),
				"ous":          stringArrayProperty("OU names; expands to the OU's enabled devices that have backup enabled"),
				"org":          boolProperty("Back up every device in the org that has backup enabled (excludes devices and ous)"),
				"confirm":      confirmProperty(),
			},
		},
	}, s.handleRunBackup)
}

func (s *Server) handleRunBackup(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[runBackupInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	return s.runBackup(ctx, input)
}

// runBackup holds the validation and confirm-gated execution of
// ndcli.run.backup, split out from handleRunBackup so it can be exercised
// without a live authenticated Service (see newTestServer).
func (s *Server) runBackup(ctx context.Context, input *runBackupInput) (*mcp.CallToolResult, error) {
	if err := refuseBackupDeferral(input); err != nil {
		return s.errorResult(err)
	}
	opts := service.BackupRunOpts{Devices: input.Devices, OUs: input.OUs, AllDevices: input.Org}
	if err := opts.Validate(); err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	if !input.Confirm {
		return s.previewResultWithNote("back up", backupRunScope(opts),
			"Only devices that have backup enabled are backed up; the others are reported in skipped. "+
				"The backup starts now, and each task expires a minute after it is created, so an offline device is not backed up later.",
			nil)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()
	result, err := s.svc.BackupRun(apiCtx, org, opts)
	if err != nil {
		return s.backupRunError(err, opts)
	}
	data, summary := backupRunResponse(result)
	return s.successResult(data, summary)
}

// refuseBackupDeferral refuses the parameters that would defer, schedule or
// load a backup. They are not in the schema; a caller used to the other
// ndcli.run.* tools may still send one.
func refuseBackupDeferral(in *runBackupInput) error {
	var given []string
	if strings.TrimSpace(in.At) != "" {
		given = append(given, "at")
	}
	if strings.TrimSpace(in.Schedule) != "" {
		given = append(given, "schedule")
	}
	if strings.TrimSpace(in.ScheduledAt) != "" {
		given = append(given, "scheduled_at")
	}
	if p := strings.TrimSpace(string(in.Payload)); p != "" && p != "null" {
		given = append(given, "payload")
	}
	if len(given) == 0 {
		return nil
	}
	return &service.Error{
		Code: service.CodeInvalidInput,
		Message: fmt.Sprintf("ndcli.run.backup does not take %s: a backup requested here starts now and cannot be deferred or scheduled, "+
			"and its payload is set by the control plane. Recurring backups are set with ndcli.backup.config_set_schedule.",
			strings.Join(given, ", ")),
	}
}

// backupRunScope says, for the preview, what the backup would cover.
func backupRunScope(opts service.BackupRunOpts) string {
	if opts.AllDevices {
		return "every device that has backup enabled"
	}
	devices, ous := len(opts.Devices), len(opts.OUs)
	switch {
	case devices > 0 && ous > 0:
		return countOf(devices, "device") + " + " + countOf(ous, "OU")
	case devices == 1:
		return opts.Devices[0]
	case devices > 1:
		return countOf(devices, "device")
	case ous == 1:
		return "OU " + opts.OUs[0]
	default:
		return countOf(ous, "OU")
	}
}

// countOf is "1 device", "2 devices", "1 OU", "3 OUs".
func countOf(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// backupSkippedData renders the skipped devices as named fields, the shape of
// a run's task rows (`device`, `device_uuid`) plus the code and the reason.
func backupSkippedData(skipped []models.BackupRunSkipped) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(skipped))
	for _, d := range skipped {
		rows = append(rows, map[string]interface{}{
			"device":      d.DeviceName,
			"device_uuid": d.DeviceUUID,
			"code":        d.Code,
			"reason":      d.Reason,
		})
	}
	return rows
}

// backupRunResponse renders an executed on-demand backup. The tasks and the
// skipped devices are both named data; the summary only counts them.
func backupRunResponse(result *models.BackupRunResult) (map[string]interface{}, string) {
	tasks := make([]map[string]interface{}, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		tasks = append(tasks, map[string]interface{}{
			"task":        t.Task,
			"device":      t.DeviceName,
			"device_uuid": t.DeviceUUID,
			"status":      t.Status,
			"expires_at":  t.ExpiresAt,
		})
	}
	data := map[string]interface{}{
		"type":         result.Type,
		"organization": result.Organization,
		"total":        result.Total,
		"tasks":        tasks,
		"skipped":      backupSkippedData(result.Skipped),
	}
	summary := fmt.Sprintf("%d backup task(s) started", result.Total)
	if n := len(result.Skipped); n > 0 {
		summary += fmt.Sprintf("; %d device(s) not backed up, the reason for each is in skipped", n)
	}
	return data, summary
}

// backupRunError turns a service failure into the tool's error result: the way
// forward in this surface's words, and, for a request that started nothing, the
// per-device reasons as data. opts is the request that was refused; what an
// empty list means depends on it (see service.BackupRunAdvice).
func (s *Server) backupRunError(err error, opts service.BackupRunOpts) (*mcp.CallToolResult, error) {
	err = service.BackupRunAdvice(err, opts, backupRunMCPVocabulary)
	if skipped, ok := service.BackupRunRefusal(err); ok {
		return s.errorResultWithData(err, map[string]interface{}{"skipped": backupSkippedData(skipped)})
	}
	return s.errorResult(err)
}
