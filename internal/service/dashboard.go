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

// DeviceHealth returns the per-device telemetry drill-down. 404 maps to
// the API error path; the dashboard endpoint and this one share the
// same Redis snapshot reader on NDManager's side.
func (s *Service) DeviceHealth(ctx context.Context, org, device string) (*models.DeviceTelemetryResponse, error) {
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
	resp, err := s.api.Get(ctx, path, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.DeviceTelemetryResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}
