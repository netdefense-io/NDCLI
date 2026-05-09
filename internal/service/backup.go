package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// BackupConfigGet returns the org's backup configuration. A 404 is preserved
// so callers can use errors.As to detect "no config yet".
func (s *Service) BackupConfigGet(ctx context.Context, org string) (*models.BackupConfig, error) {
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var cfg models.BackupConfig
	if err := api.ParseResponse(resp, &cfg); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &cfg, nil
}

// BackupConfigExists is a convenience wrapper for "is there a config?". It
// only returns true when the GET succeeds; transport-level errors fold to
// false (caller should re-fetch via BackupConfigGet to surface them).
func (s *Service) BackupConfigExists(ctx context.Context, org string) bool {
	_, err := s.BackupConfigGet(ctx, org)
	if err == nil {
		return true
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
		return false
	}
	return false
}

// BackupConfigCreateOpts holds the required fields for creating an org
// backup configuration. S3AccessKey and EncryptionKey carry secrets — never
// log them.
type BackupConfigCreateOpts struct {
	S3Endpoint    string
	S3Bucket      string
	S3KeyID       string
	S3AccessKey   string
	S3Folder      string // mapped to s3_prefix
	Schedule      string
	EncryptionKey string
}

// BackupConfigCreate creates a new backup configuration. All fields except
// S3Folder are required.
func (s *Service) BackupConfigCreate(ctx context.Context, org string, opts BackupConfigCreateOpts) (*models.BackupConfig, error) {
	switch {
	case opts.S3Endpoint == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "s3_endpoint is required"}
	case opts.S3Bucket == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "s3_bucket is required"}
	case opts.S3KeyID == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "s3_key_id is required"}
	case opts.S3AccessKey == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "s3_access_key is required"}
	case opts.Schedule == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule is required"}
	case opts.EncryptionKey == "":
		return nil, &Error{Code: CodeInvalidInput, Message: "encryption_key is required"}
	}
	payload := map[string]string{
		"s3_endpoint":    opts.S3Endpoint,
		"s3_bucket":      opts.S3Bucket,
		"s3_key_id":      opts.S3KeyID,
		"s3_access_key":  opts.S3AccessKey,
		"schedule":       opts.Schedule,
		"encryption_key": opts.EncryptionKey,
	}
	if opts.S3Folder != "" {
		payload["s3_prefix"] = opts.S3Folder
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var cfg models.BackupConfig
	if err := api.ParseResponse(resp, &cfg); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &cfg, nil
}

// BackupConfigUpdateOpts holds the optional fields for an update PATCH-style
// operation (the underlying endpoint is a PUT but only sends provided
// fields). Empty strings mean "no change".
type BackupConfigUpdateOpts struct {
	S3Endpoint    string
	S3Bucket      string
	S3KeyID       string
	S3AccessKey   string
	S3Folder      string
	Schedule      string
	EncryptionKey string
}

// BackupConfigUpdate updates the org's backup configuration. At least one
// field must be set.
func (s *Service) BackupConfigUpdate(ctx context.Context, org string, opts BackupConfigUpdateOpts) (*models.BackupConfig, error) {
	payload := map[string]string{}
	if opts.S3Endpoint != "" {
		payload["s3_endpoint"] = opts.S3Endpoint
	}
	if opts.S3Bucket != "" {
		payload["s3_bucket"] = opts.S3Bucket
	}
	if opts.S3KeyID != "" {
		payload["s3_key_id"] = opts.S3KeyID
	}
	if opts.S3AccessKey != "" {
		payload["s3_access_key"] = opts.S3AccessKey
	}
	if opts.S3Folder != "" {
		payload["s3_prefix"] = opts.S3Folder
	}
	if opts.Schedule != "" {
		payload["schedule"] = opts.Schedule
	}
	if opts.EncryptionKey != "" {
		payload["encryption_key"] = opts.EncryptionKey
	}
	if len(payload) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "no update fields provided"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var cfg models.BackupConfig
	if err := api.ParseResponse(resp, &cfg); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &cfg, nil
}

// BackupConfigDelete removes the backup configuration. This implicitly
// disables backup for every device in the org.
func (s *Service) BackupConfigDelete(ctx context.Context, org string) error {
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config", org))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// BackupConfigSetStatus flips the org backup config to ENABLED or DISABLED.
func (s *Service) BackupConfigSetStatus(ctx context.Context, org string, enabled bool) error {
	status := "DISABLED"
	if enabled {
		status = "ENABLED"
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config/status", org), map[string]string{"status": status})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// BackupConfigTest runs an S3 connection test against the configured
// endpoint/credentials.
func (s *Service) BackupConfigTest(ctx context.Context, org string) (*models.BackupConfigTestResponse, error) {
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/backup-config/test", org), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.BackupConfigTestResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// BackupStatusOpts collects the filters for listing per-device backup
// statuses.
type BackupStatusOpts struct {
	EnabledOnly bool
	Status      string // SUCCESS, FAILED, IN_PROGRESS
	Page        int
	PerPage     int
}

// BackupStatusResult mirrors the device backup list with resolved defaults.
type BackupStatusResult struct {
	Items        []models.DeviceBackupStatus
	Total        int
	EnabledCount int
	Page         int
	PerPage      int
}

// BackupStatusList returns the per-device backup status across the org.
func (s *Service) BackupStatusList(ctx context.Context, org string, opts BackupStatusOpts) (*BackupStatusResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 30
	}
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.EnabledOnly {
		params["enabled"] = "true"
	}
	if opts.Status != "" {
		params["status"] = opts.Status
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/backups", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.DeviceBackupListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &BackupStatusResult{
		Items:        result.Items,
		Total:        result.Total,
		EnabledCount: result.EnabledCount,
		Page:         page,
		PerPage:      perPage,
	}, nil
}

// BackupStatusGet returns the backup status for a single device.
func (s *Service) BackupStatusGet(ctx context.Context, org, deviceName string) (*models.DeviceBackupStatus, error) {
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup", org, deviceName), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var st models.DeviceBackupStatus
	if err := api.ParseResponse(resp, &st); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &st, nil
}

// BackupSetEnabled enables/disables backup for a single device.
func (s *Service) BackupSetEnabled(ctx context.Context, org, deviceName string, enabled bool) error {
	if deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup", org, deviceName), map[string]bool{"enabled": enabled})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// BackupEncryptionKeySet sets a per-device encryption key override. Caller
// is responsible for sourcing the key (typically from a secure prompt).
//
// This method is exported so the CLI can use it; the MCP tool intentionally
// does not expose it (sensitive secret material).
func (s *Service) BackupEncryptionKeySet(ctx context.Context, org, deviceName, key string) error {
	if deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	if key == "" {
		return &Error{Code: CodeInvalidInput, Message: "encryption key cannot be empty"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup/encryption-key", org, deviceName), map[string]string{"encryption_key": key})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// BackupEncryptionKeyRemove removes the per-device encryption key override
// (the device falls back to the org-default key).
//
// CLI-only — not exposed via MCP.
func (s *Service) BackupEncryptionKeyRemove(ctx context.Context, org, deviceName string) error {
	if deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/backup/encryption-key", org, deviceName))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
