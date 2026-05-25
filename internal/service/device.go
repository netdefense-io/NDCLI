package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// DeviceListOpts are the optional filters and pagination knobs for
// (*Service).DeviceList. Empty fields are omitted from the API call.
//
// Time-window fields accept the relative shorthand ndcli already understands
// (e.g. "30m", "2h", "7d", "2w") or any ISO-8601 timestamp; both are routed
// through helpers.ParseTimeFilter.
type DeviceListOpts struct {
	Status          string
	OU              string
	Name            string
	SortBy          string
	Page            int
	PerPage         int
	HeartbeatAfter  string
	HeartbeatBefore string
	SyncedAfter     string
	SyncedBefore    string
	CreatedAfter    string
	CreatedBefore   string
	DriftStatus     string
}

// DeviceListResult mirrors the paginated device list response, with
// pagination echoed back as resolved (defaults applied) so callers don't have
// to re-derive them.
type DeviceListResult struct {
	Devices []models.Device
	Total   int
	Page    int
	PerPage int
	Quota   *models.Quota
}

// DeviceList returns a paginated list of devices for the organization.
func (s *Service) DeviceList(ctx context.Context, org string, opts DeviceListOpts) (*DeviceListResult, error) {
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
	if opts.Status != "" {
		params["status"] = opts.Status
	}
	if opts.OU != "" {
		params["ou"] = opts.OU
	}
	if opts.Name != "" {
		params["name"] = opts.Name
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}
	if opts.DriftStatus != "" {
		params["drift_status"] = opts.DriftStatus
	}

	timeFilters := []struct {
		raw, key, label string
	}{
		{opts.HeartbeatAfter, "heartbeat_after", "heartbeat_after"},
		{opts.HeartbeatBefore, "heartbeat_before", "heartbeat_before"},
		{opts.SyncedAfter, "synced_after", "synced_after"},
		{opts.SyncedBefore, "synced_before", "synced_before"},
		{opts.CreatedAfter, "created_after", "created_after"},
		{opts.CreatedBefore, "created_before", "created_before"},
	}
	for _, f := range timeFilters {
		if f.raw == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f.raw)
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f.label, err),
				Err:     err,
			}
		}
		params[f.key] = parsed
	}

	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}

	return &DeviceListResult{
		Devices: result.GetItems(),
		Total:   result.Total,
		Page:    page,
		PerPage: perPage,
		Quota:   result.Quota,
	}, nil
}

// DeviceGet returns a single device by name.
func (s *Service) DeviceGet(ctx context.Context, org, name string) (*models.Device, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var device models.Device
	if err := api.ParseResponse(resp, &device); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &device, nil
}

// DeviceApprove flips a PENDING device to ENABLED.
func (s *Service) DeviceApprove(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/approve", org, name), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// DeviceApprovalResult is the per-device outcome of DeviceApproveAll. Err is
// nil on success.
type DeviceApprovalResult struct {
	Name string
	Err  error
}

// DeviceApproveAll fetches every PENDING device (up to 500 — the cap matches
// the existing CLI behaviour) and approves each one, returning per-device
// results. The slice is empty when nothing was pending.
func (s *Service) DeviceApproveAll(ctx context.Context, org string) ([]DeviceApprovalResult, error) {
	listing, err := s.DeviceList(ctx, org, DeviceListOpts{
		Status:  models.DeviceStatusPending,
		PerPage: 500,
	})
	if err != nil {
		return nil, err
	}
	if len(listing.Devices) == 0 {
		return nil, nil
	}

	results := make([]DeviceApprovalResult, 0, len(listing.Devices))
	for _, d := range listing.Devices {
		results = append(results, DeviceApprovalResult{
			Name: d.Name,
			Err:  s.DeviceApprove(ctx, org, d.Name),
		})
	}
	return results, nil
}

// DeviceRename changes a device's name in place.
func (s *Service) DeviceRename(ctx context.Context, org, name, newName string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	if newName == "" {
		return &Error{Code: CodeInvalidInput, Message: "new name is required"}
	}
	payload := map[string]string{"new_name": newName}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/rename", org, name), payload)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// DeviceRemove deletes a device from management. The caller is responsible
// for any user-facing confirmation prompt.
func (s *Service) DeviceRemove(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s", org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// RebindTokenResult mirrors the issue-rebind-token endpoint response. The
// raw bootstrap token is single-use and only exposed once — callers must
// surface it without persistence.
type RebindTokenResult struct {
	BootstrapToken string `json:"bootstrap_token"`
	ExpiresAt      string `json:"expires_at"`
	Message        string `json:"message"`
}

// DeviceRebindToken issues a one-time signing-key re-bind token for the
// device. ttl is the desired validity window; NDManager caps it at 7 days.
func (s *Service) DeviceRebindToken(ctx context.Context, org, name string, ttl time.Duration) (*RebindTokenResult, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	if ttl <= 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "ttl must be positive"}
	}
	body := map[string]int{"ttl_seconds": int(ttl.Seconds())}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/issue-rebind-token", org, name), body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var parsed RebindTokenResult
	if err := api.ParseResponse(resp, &parsed); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &parsed, nil
}
