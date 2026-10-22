package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// SyncFilter selects the devices a sync operation applies to. NDManager
// matches Organization and OU as exact names, and Device and Template as
// regex patterns. Empty fields are omitted.
//
// Organization defaults to the caller's current org when empty; the caller
// supplies that fallback explicitly via SyncStatus / SyncApply.
//
// Schedule, when set, registers a recurring ScheduledTask spec instead of
// triggering an immediate sync.
type SyncFilter struct {
	Organization string
	Device       string
	OU           string
	DriftStatus  string
	Template     string
	Schedule     string // schedule name; when set, registers a recurring spec
}

// ExactNamePattern is the SyncFilter.Device pattern that selects one device
// and nothing else. NDManager searches with the pattern unanchored, so a bare
// "fw-01" also selects "fw-010" and "lab-fw-01".
func ExactNamePattern(name string) string {
	return "^" + regexp.QuoteMeta(name) + "$"
}

// SyncApplyResult bundles the parsed sync apply response with the raw HTTP
// status, so callers can distinguish full success (200), partial (207), and
// total failure (400) without re-issuing the request. Response is always
// populated when err is nil; StatusCode is the wire status.
type SyncApplyResult struct {
	Response   *models.SyncApplyResponse
	StatusCode int
}

// SyncApplyRefusal is the error a front-end returns, after showing the
// result, for a sync that every targeted device refused; nil otherwise. The
// per-device reasons stay in the result.
func SyncApplyRefusal(res *SyncApplyResult) error {
	if res == nil || !res.Response.AllRefused() {
		return nil
	}
	return &Error{Code: CodeAPIError, Message: "sync failed for all devices"}
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

	// 400 is the one error status that carries a sync result. Any other
	// refuses the request itself (403 ACCESS_DENIED or TOKEN_READ_ONLY, 404,
	// 422) with the usual error body.
	if resp.StatusCode > 400 {
		return nil, wrapAPI("%v", api.ParseError(resp))
	}

	// 200 / 207 / 400 all return the same body shape; decode directly so
	// non-2xx doesn't get folded into an error. DeviceName/Error strings in
	// the response reach the terminal via formatter.FormatSyncApply, so
	// route through api.DecodeJSON for the same cap+sanitize treatment
	// ParseResponse gives every other decode path.
	raw, err := api.ReadBody(resp.Body)
	if err != nil {
		return nil, wrapAPI(fmt.Sprintf("failed to read response: %v", err), err)
	}
	var body models.SyncApplyResponse
	decodeErr := api.DecodeJSON(bytes.NewReader(raw), &body)

	// The 400 sync result always names the refused devices. A 400 that names
	// none is a plain refusal of the request, read as the error it carries.
	if resp.StatusCode == 400 && (decodeErr != nil || len(body.Errors) == 0) {
		refusal := &http.Response{StatusCode: resp.StatusCode, Body: io.NopCloser(bytes.NewReader(raw))}
		return nil, wrapAPI("%v", api.ParseError(refusal))
	}
	if decodeErr != nil {
		return nil, wrapAPI(fmt.Sprintf("failed to parse response: %v", decodeErr), decodeErr)
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
	if resp.StatusCode >= 400 {
		return nil, wrapAPI("%v", api.ParseError(resp))
	}

	// ScheduleName/CreatedBy are server-supplied and formatter-rendered, so
	// the decode goes through api.DecodeJSON for its cap+sanitize treatment.
	var result models.ScheduledTaskRegisterResult
	if err := api.DecodeJSON(resp.Body, &result); err != nil {
		return nil, wrapAPI(fmt.Sprintf("failed to parse response: %v", err), err)
	}
	return &result, nil
}
