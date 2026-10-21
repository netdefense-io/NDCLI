package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// The codes NDManager refuses an on-demand backup with.
const (
	apiCodeBackupNotConfigured  = "BACKUP_NOT_CONFIGURED"
	apiCodeBackupConfigDisabled = "BACKUP_CONFIG_DISABLED"
	apiCodeBackupNoEligible     = "BACKUP_NO_ELIGIBLE_DEVICES"
)

// BackupRunOpts is the typed input of Service.BackupRun: which devices to back
// up now. It carries no time, schedule or payload, because the route has none:
// a backup requested here starts now, recurring backups run on the backup
// schedule, and the encryption key is resolved by the control plane.
type BackupRunOpts struct {
	Devices    []string // device names
	OUs        []string // OU names; expands to the OU's enabled devices
	AllDevices bool     // every device that has backup enabled; excludes Devices/OUs
}

// Validate refuses a target selection the route would refuse, before any
// request. Messages name fields, never flags: the CLI and the MCP tool share it.
func (o BackupRunOpts) Validate() error {
	if !o.AllDevices && len(o.Devices) == 0 && len(o.OUs) == 0 {
		return &Error{Code: CodeInvalidInput, Message: "name at least one device or OU, or target every device that has backup enabled"}
	}
	if o.AllDevices && (len(o.Devices) > 0 || len(o.OUs) > 0) {
		return &Error{Code: CodeInvalidInput, Message: "targeting every device that has backup enabled cannot be combined with named devices or OUs"}
	}
	for _, name := range o.Devices {
		if strings.TrimSpace(name) == "" {
			return &Error{Code: CodeInvalidInput, Message: "a device name must not be blank"}
		}
	}
	for _, name := range o.OUs {
		if strings.TrimSpace(name) == "" {
			return &Error{Code: CodeInvalidInput, Message: "an OU name must not be blank"}
		}
	}
	return nil
}

// BackupRun asks NDManager to back up devices now:
// POST /organizations/{org}/backups/run. NDManager creates one BACKUP task per
// targeted device that has backup enabled and answers which devices it left out
// and why. It is a route of its own because POST /organizations/{org}/tasks
// refuses BACKUP by design: the task's payload is the encryption key, which only
// the control plane resolves. Do not route a backup through Run.
//
// The tasks expire a minute after they are created, so a device that is offline
// ends EXPIRED instead of backing up hours later.
func (s *Service) BackupRun(ctx context.Context, org string, opts BackupRunOpts) (*models.BackupRunResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	body := models.BackupRunRequest{Targets: models.BackupRunTargets{
		Devices: opts.Devices,
		OUs:     opts.OUs,
		All:     opts.AllDevices,
	}}
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/backups/run", url.PathEscape(org))
	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.BackupRunResult
	if err := api.ParseResponse(resp, &result); err != nil {
		// A control plane that predates the route answers FastAPI's bare 404,
		// which carries no code; every refusal of the route itself does.
		if api.IsNotFoundError(err) && api.GetErrorCode(err) == "" {
			return nil, &Error{
				Code:    CodeServerTooOld,
				Message: "this NDManager predates on-demand backups: it has no route to start one",
				Err:     err,
			}
		}
		return nil, wrapAPI("%v", err)
	}
	// Both lists are always present on the wire; keep them arrays for -f json
	// and the MCP result even if a server leaves one out.
	if result.Tasks == nil {
		result.Tasks = []models.RunTaskItem{}
	}
	if result.Skipped == nil {
		result.Skipped = []models.BackupRunSkipped{}
	}
	return &result, nil
}

// BackupRunRefusal returns the devices a BACKUP_NO_ELIGIBLE_DEVICES refusal
// says it left out, looking through any wrapping. ok is true for that refusal
// even when the list is empty, which means no device in the organization has
// backup enabled; it is false for every other error.
func BackupRunRefusal(err error) (skipped []models.BackupRunSkipped, ok bool) {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != apiCodeBackupNoEligible {
		return nil, false
	}
	skipped = make([]models.BackupRunSkipped, 0, len(apiErr.Skipped))
	for _, d := range apiErr.Skipped {
		skipped = append(skipped, models.BackupRunSkipped{
			DeviceUUID: d.DeviceUUID,
			DeviceName: d.DeviceName,
			Code:       d.Code,
			Reason:     d.Reason,
		})
	}
	return skipped, true
}

// BackupRunVocabulary is how a front-end names the way forward from a refused
// on-demand backup; BackupRunAdvice decides which refusal gets which advice.
type BackupRunVocabulary struct {
	ConfigCreate string // create the organization's backup configuration
	ConfigEnable string // enable the organization's backup configuration
	DeviceEnable string // enable backup for a device
}

// BackupRunAdvice returns err with the way forward appended when it is a
// refusal that has one, in the caller's own vocabulary; any other error comes
// back untouched. The result still wraps err, so the code NDManager sent
// (api.GetErrorCode) and the service code survive.
//
// opts is the request that was refused, because BACKUP_NO_ELIGIBLE_DEVICES
// means different things for it. An empty skipped list answers "no device in the
// organization has backup enabled" only to a request for every such device; for
// named devices or OUs it means the targets resolved to no enabled device (an OU
// with no members, say), and enabling backup would not help.
func BackupRunAdvice(err error, opts BackupRunOpts, v BackupRunVocabulary) error {
	if err == nil {
		return nil
	}
	var hint string
	switch api.GetErrorCode(err) {
	case apiCodeBackupNotConfigured:
		hint = fmt.Sprintf("Create the backup configuration first: %s.", v.ConfigCreate)
	case apiCodeBackupConfigDisabled:
		hint = fmt.Sprintf("Enable it first: %s.", v.ConfigEnable)
	case apiCodeBackupNoEligible:
		skipped, _ := BackupRunRefusal(err)
		backupOff := len(skipped) == 0 && opts.AllDevices
		for _, d := range skipped {
			if d.Code == models.BackupSkipNotEnabled {
				backupOff = true
			}
		}
		if !backupOff {
			return err
		}
		hint = fmt.Sprintf("Backup is enabled per device: %s.", v.DeviceEnable)
	default:
		return err
	}
	// Advice given once is not given again by an outer layer.
	if strings.Contains(err.Error(), hint) {
		return err
	}
	code := CodeAPIError
	var svcErr *Error
	if errors.As(err, &svcErr) && svcErr.Code != "" {
		code = svcErr.Code
	}
	return &Error{Code: code, Message: err.Error() + "\n  Hint: " + hint, Err: err}
}
