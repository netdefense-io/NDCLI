package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// SyncFilter selects the devices a sync operation applies to. All fields are
// regex patterns interpreted by NDManager. Empty fields are omitted.
//
// Organization defaults to the caller's current org when empty; the caller
// supplies that fallback explicitly via SyncStatus / SyncApply.
//
// Schedule, when set, registers a recurring ScheduledTask spec instead of
// triggering an immediate sync. The org filter must be an exact name (not a
// regex) when scheduling, per the API contract.
type SyncFilter struct {
	Organization string
	Device       string
	OU           string
	DriftStatus  string
	Template     string
	Schedule     string // schedule name; when set, registers a recurring spec
}

// SyncApplyResult bundles the parsed sync apply response with the raw HTTP
// status, so callers can distinguish full success (200), partial (207), and
// total failure (400) without re-issuing the request. Response is always
// populated when err is nil; StatusCode is the wire status.
type SyncApplyResult struct {
	Response   *models.SyncApplyResponse
	StatusCode int
}

// SyncStatus returns the per-device sync status matching the filter.
// defaultOrg is used when filter.Organization is empty.
func (s *Service) SyncStatus(ctx context.Context, defaultOrg string, filter SyncFilter) (*models.SyncStatusResponse, error) {
	params := buildSyncParams(defaultOrg, filter)
	resp, err := s.api.Get(ctx, "/api/v1/sync/status", params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.SyncStatusResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// SyncApply triggers a sync for every device matching the filter. The
// response carries both successes (Tasks) and per-device errors (Errors); a
// 400 status with non-empty errors means every targeted device failed.
func (s *Service) SyncApply(ctx context.Context, defaultOrg string, filter SyncFilter, force bool) (*SyncApplyResult, error) {
	params := buildSyncParams(defaultOrg, filter)
	if force {
		params["force"] = "true"
	}
	resp, err := s.api.PostWithParams(ctx, "/api/v1/sync", params, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	defer resp.Body.Close()

	// 200 / 207 / 400 all return the same body shape; decode directly so
	// non-2xx doesn't get folded into an error.
	var body models.SyncApplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, wrapAPI(fmt.Sprintf("failed to parse response: %v", err), err)
	}
	return &SyncApplyResult{Response: &body, StatusCode: resp.StatusCode}, nil
}

func buildSyncParams(defaultOrg string, filter SyncFilter) map[string]string {
	params := map[string]string{}
	if filter.Device != "" {
		params["device"] = filter.Device
	}
	if filter.OU != "" {
		params["ou"] = filter.OU
	}
	if filter.Template != "" {
		params["template"] = filter.Template
	}
	if filter.Organization != "" {
		params["organization"] = filter.Organization
	} else if defaultOrg != "" {
		params["organization"] = defaultOrg
	}
	if filter.DriftStatus != "" {
		params["drift_status"] = filter.DriftStatus
	}
	if filter.Schedule != "" {
		params["schedule"] = filter.Schedule
	}
	return params
}

// SyncRegisterSpec triggers sync with a "schedule" query param, which causes
// NDManager to register a recurring ScheduledTask spec and return a 201
// descriptor instead of running sync immediately.
func (s *Service) SyncRegisterSpec(ctx context.Context, defaultOrg string, filter SyncFilter, force bool) (*models.ScheduledTaskRegisterResult, error) {
	if filter.Schedule == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule name is required for spec registration"}
	}
	params := buildSyncParams(defaultOrg, filter)
	if force {
		params["force"] = "true"
	}
	resp, err := s.api.PostWithParams(ctx, "/api/v1/sync", params, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	defer resp.Body.Close()

	var result models.ScheduledTaskRegisterResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, wrapAPI(fmt.Sprintf("failed to parse response: %v", err), err)
	}
	return &result, nil
}
