package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// NOTE: Encryption-key tools are intentionally NOT registered here. The
// underlying service methods (BackupEncryptionKeySet / Remove) exist for
// the CLI but exposing them via MCP would let an LLM-driven flow plant or
// rotate per-device backup keys, which is exactly the kind of secret
// material we don't want in the LLM tool surface.

type backupOrgInput struct {
	Organization string `json:"organization,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type backupConfigCreateInput struct {
	Organization  string `json:"organization,omitempty"`
	S3Endpoint    string `json:"s3_endpoint"`
	S3Bucket      string `json:"s3_bucket"`
	S3KeyID       string `json:"s3_key_id"`
	S3AccessKey   string `json:"s3_access_key"`
	S3Folder      string `json:"s3_folder,omitempty"`
	EncryptionKey string `json:"encryption_key"`
	Confirm       bool   `json:"confirm,omitempty"`
}

type backupConfigUpdateInput struct {
	Organization  string `json:"organization,omitempty"`
	S3Endpoint    string `json:"s3_endpoint,omitempty"`
	S3Bucket      string `json:"s3_bucket,omitempty"`
	S3KeyID       string `json:"s3_key_id,omitempty"`
	S3AccessKey   string `json:"s3_access_key,omitempty"`
	S3Folder      string `json:"s3_folder,omitempty"`
	EncryptionKey string `json:"encryption_key,omitempty"`
	Confirm       bool   `json:"confirm,omitempty"`
}

// backupConfigSetScheduleInput is the input for the dedicated schedule
// attach/detach tool. ScheduleName empty → detach (null body).
type backupConfigSetScheduleInput struct {
	Organization string  `json:"organization,omitempty"`
	ScheduleName *string `json:"schedule_name"` // null/absent = detach
}

type backupStatusListInput struct {
	Organization string `json:"organization,omitempty"`
	EnabledOnly  bool   `json:"enabled_only,omitempty"`
	Status       string `json:"status,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type backupDeviceInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// registerBackupTools registers backup tools (config + status + per-device
// enable/disable). Encryption-key management is deliberately excluded.
func (s *Server) registerBackupTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_show",
		Description: "Show the org's backup configuration (S3 endpoint/bucket/schedule, status). Returns a 404 if no config has been created.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
			},
		},
	}, s.handleBackupConfigShow)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_create",
		Description: "Create the org's backup configuration. Carries S3 secrets and the org-default backup encryption key — only invoke this when the user has explicitly authorised exposing those values to MCP. Requires confirm=true. Attach a schedule separately with ndcli.backup.config_set_schedule.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"s3_endpoint":    stringProperty("S3 endpoint URL"),
				"s3_bucket":      stringProperty("S3 bucket name"),
				"s3_key_id":      stringProperty("S3 access key ID"),
				"s3_access_key":  stringProperty("S3 secret access key (sensitive)"),
				"s3_folder":      stringProperty("Optional folder prefix within the bucket"),
				"encryption_key": stringProperty("Org-default backup encryption key (sensitive)"),
				"confirm":        confirmProperty(),
			},
			"required": []string{"s3_endpoint", "s3_bucket", "s3_key_id", "s3_access_key", "encryption_key"},
		},
	}, s.handleBackupConfigCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_update",
		Description: "Update the org's S3/key backup configuration. At least one field must be set. Requires confirm=true. To change the attached schedule use ndcli.backup.config_set_schedule.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"s3_endpoint":    stringProperty("New S3 endpoint URL"),
				"s3_bucket":      stringProperty("New S3 bucket"),
				"s3_key_id":      stringProperty("New S3 access key ID"),
				"s3_access_key":  stringProperty("New S3 secret access key (sensitive)"),
				"s3_folder":      stringProperty("New folder prefix"),
				"encryption_key": stringProperty("New org-default encryption key (sensitive)"),
				"confirm":        confirmProperty(),
			},
		},
	}, s.handleBackupConfigUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_set_schedule",
		Description: "Attach the org's backup to a named Schedule (creates a BACKUP spec), or detach it. Provide schedule_name to attach; omit or set to null to detach. The named Schedule must already exist in the org (404 otherwise).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":  organizationProperty(),
				"schedule_name": stringProperty("Name of the Schedule to attach to. Omit or set to null to detach."),
			},
		},
	}, s.handleBackupConfigSetSchedule)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_delete",
		Description: "Delete the org's backup configuration. Disables backup for every device. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"confirm":      confirmProperty(),
			},
		},
	}, s.handleBackupConfigDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_enable",
		Description: "Enable the org's backup configuration (status → ENABLED).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
			},
		},
	}, s.handleBackupConfigEnable)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_disable",
		Description: "Disable the org's backup configuration (status → DISABLED). Per-device enable/disable state is preserved.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"confirm":      confirmProperty(),
			},
		},
	}, s.handleBackupConfigDisable)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.config_test",
		Description: "Test S3 connectivity using the configured credentials.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
			},
		},
	}, s.handleBackupConfigTest)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.status",
		Description: "List per-device backup status across the organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"enabled_only": boolProperty("Show only devices with backup enabled"),
				"status":       stringEnumProperty("Filter by last backup status", []string{"SUCCESS", "FAILED", "IN_PROGRESS"}),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page (1-100)", 30),
			},
		},
	}, s.handleBackupStatusList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.show",
		Description: "Show backup status for a single device.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"device"},
		},
	}, s.handleBackupShow)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.enable",
		Description: "Enable backup for a single device.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"device"},
		},
	}, s.handleBackupDeviceEnable)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.backup.disable",
		Description: "Disable backup for a single device. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"device"},
		},
	}, s.handleBackupDeviceDisable)
}

func (s *Server) handleBackupConfigShow(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	cfg, err := s.svc.BackupConfigGet(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"config": backupConfigSummary(cfg),
	}, "")
}

func (s *Server) handleBackupConfigCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupConfigCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("create backup configuration (sensitive secrets will be sent)", org)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	cfg, err := s.svc.BackupConfigCreate(apiCtx, org, service.BackupConfigCreateOpts{
		S3Endpoint:    input.S3Endpoint,
		S3Bucket:      input.S3Bucket,
		S3KeyID:       input.S3KeyID,
		S3AccessKey:   input.S3AccessKey,
		S3Folder:      input.S3Folder,
		EncryptionKey: input.EncryptionKey,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"config": backupConfigSummary(cfg),
		"action": "created",
	}, "Backup configuration created")
}

func (s *Server) handleBackupConfigUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupConfigUpdateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update backup configuration", org)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	cfg, err := s.svc.BackupConfigUpdate(apiCtx, org, service.BackupConfigUpdateOpts{
		S3Endpoint:    input.S3Endpoint,
		S3Bucket:      input.S3Bucket,
		S3KeyID:       input.S3KeyID,
		S3AccessKey:   input.S3AccessKey,
		S3Folder:      input.S3Folder,
		EncryptionKey: input.EncryptionKey,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"config": backupConfigSummary(cfg),
		"action": "updated",
	}, "Backup configuration updated")
}

func (s *Server) handleBackupConfigDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete backup configuration (also disables backup for every device)", org)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.BackupConfigDelete(apiCtx, org); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"action": "deleted"}, "Backup configuration deleted")
}

func (s *Server) handleBackupConfigEnable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.BackupConfigSetStatus(apiCtx, org, true); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"status": "ENABLED"}, "Backup configuration enabled")
}

func (s *Server) handleBackupConfigDisable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("disable backup configuration", org)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.BackupConfigSetStatus(apiCtx, org, false); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"status": "DISABLED"}, "Backup configuration disabled")
}

func (s *Server) handleBackupConfigTest(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.BackupConfigTest(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"success": result.Success,
		"message": result.Message,
	}, result.Message)
}

func (s *Server) handleBackupStatusList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupStatusListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.BackupStatusList(apiCtx, org, service.BackupStatusOpts{
		EnabledOnly: input.EnabledOnly,
		Status:      input.Status,
		Page:        input.Page,
		PerPage:     input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, deviceBackupSummary(&it))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"devices":       items,
		"enabled_count": result.EnabledCount,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleBackupShow(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupDeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	st, err := s.svc.BackupStatusGet(apiCtx, org, input.Device)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"device": deviceBackupSummary(st),
	}, "")
}

func (s *Server) handleBackupDeviceEnable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupDeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.BackupSetEnabled(apiCtx, org, input.Device, true); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"device":  input.Device,
		"enabled": true,
	}, fmt.Sprintf("Backup enabled for device: %s", input.Device))
}

func (s *Server) handleBackupDeviceDisable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupDeviceInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("disable backup for device", input.Device)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.BackupSetEnabled(apiCtx, org, input.Device, false); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"device":  input.Device,
		"enabled": false,
	}, fmt.Sprintf("Backup disabled for device: %s", input.Device))
}

func (s *Server) handleBackupConfigSetSchedule(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[backupConfigSetScheduleInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	// Resolve the effective schedule name (nil pointer = detach).
	targetName := ""
	if input.ScheduleName != nil {
		targetName = *input.ScheduleName
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.BackupConfigSetSchedule(apiCtx, org, targetName)
	if err != nil {
		return s.errorResult(err)
	}

	if result.Attached != nil {
		a := result.Attached
		return s.successResult(map[string]interface{}{
			"action":        "attached",
			"code":          a.Code,
			"kind":          a.Kind,
			"schedule_name": a.ScheduleName,
			"enabled":       a.Enabled,
			"created_by":    a.CreatedBy,
			"created_at":    a.CreatedAt,
		}, fmt.Sprintf("Backup attached to schedule %q — spec code: %s", a.ScheduleName, a.Code))
	}
	d := result.Detached
	return s.successResult(map[string]interface{}{
		"action":            "detached",
		"detached":          d.Detached,
		"organization_name": d.OrganizationName,
	}, fmt.Sprintf("Backup detached from schedule (org: %s)", d.OrganizationName))
}

func backupConfigSummary(c *models.BackupConfig) map[string]interface{} {
	return map[string]interface{}{
		"s3_endpoint":        c.S3Endpoint,
		"s3_bucket":          c.S3Bucket,
		"s3_key_id":          c.S3KeyID,
		"s3_prefix":          c.S3Prefix,
		"attached_schedule":  c.AttachedSchedule,
		"scheduled":          c.Scheduled,
		"status":             c.Status,
		"has_encryption_key": c.HasEncryptionKey,
		"organization":       c.Organization,
		"created_at":         c.CreatedAt,
		"updated_at":         c.UpdatedAt,
	}
}

func deviceBackupSummary(d *models.DeviceBackupStatus) map[string]interface{} {
	return map[string]interface{}{
		"device_name":                 d.DeviceName,
		"enabled":                     d.Enabled,
		"has_encryption_key_override": d.HasEncryptionKeyOverride,
		"last_backup_at":              d.LastBackupAt,
		"last_backup_status":          d.LastBackupStatus,
		"last_backup_message":         d.LastBackupMessage,
		"organization":                d.Organization,
	}
}
