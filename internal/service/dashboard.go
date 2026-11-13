package service

import (
	"context"
	"fmt"
	"net/url"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// Dashboard returns the org-level roll-up backing both `ndcli dashboard`
// and NDWeb's landing page. Wire owner is NDManager
// (src/services/dashboard_service.py); see models/dashboard.go for the
// matching Go structs.
func (s *Service) Dashboard(ctx context.Context, org string) (*models.DashboardResponse, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	path := fmt.Sprintf("/api/v1/organizations/%s/dashboard", url.PathEscape(org))
	resp, err := s.api.Get(ctx, path, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.DashboardResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// DeviceHealthOpts selects what DeviceHealth asks for.
type DeviceHealthOpts struct {
	// Retained asks for the last-known telemetry, kept for days, where the
	// live one is missing: an expired snapshot is replaced by the last-known
	// one, marked stale, and each heavy block carries its age. It never
	// changes online. Off, the answer is live-only, as it always was. A server
	// that predates it is SERVER_TOO_OLD.
	Retained bool
}

// DeviceHealth returns the per-device telemetry drill-down. 404 maps to
// the API error path; the dashboard endpoint and this one share the
// same Redis snapshot reader on NDManager's side.
func (s *Service) DeviceHealth(ctx context.Context, org, device string, opts DeviceHealthOpts) (*models.DeviceTelemetryResponse, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if device == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	path := fmt.Sprintf(
		"/api/v1/organizations/%s/devices/%s/telemetry",
		url.PathEscape(org),
		url.PathEscape(device),
	)
	var params map[string]string
	if opts.Retained {
		params = map[string]string{"retained": "true"}
	}
	resp, err := s.api.Get(ctx, path, params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.DeviceTelemetryResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	// A server that predates retained telemetry ignores the parameter and
	// answers live-only, with no stale key; the answer is refused rather than
	// shown as what was asked for.
	if opts.Retained && result.Stale == nil {
		return nil, &Error{Code: CodeServerTooOld, Message: "this server predates retained telemetry: it answered with live telemetry only"}
	}
	return &result, nil
}
