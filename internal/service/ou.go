package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// OUListOpts mirrors the filters accepted by GET /api/v1/organizations/{org}/ous.
type OUListOpts struct {
	Status        string // "all" or "enabled"
	Name          string
	SortBy        string // name, device_count, created_at, updated_at
	SortOrder     string // asc, desc
	Page          int
	PageSize      int
	CreatedAfter  string
	CreatedBefore string
}

// OUListResult bundles paginated OUs with the resolved page/per-page.
type OUListResult struct {
	OUs      []models.OrganizationalUnit
	Total    int
	Page     int
	PageSize int
}

// OUList returns a paginated list of OUs in the org.
func (s *Service) OUList(ctx context.Context, org string, opts OUListOpts) (*OUListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	params := map[string]string{
		"page":      strconv.Itoa(page),
		"page_size": strconv.Itoa(pageSize),
	}
	if opts.Status != "" {
		params["status"] = opts.Status
	} else {
		params["status"] = "all"
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}
	if opts.SortOrder != "" {
		params["sort_order"] = opts.SortOrder
	}
	if opts.Name != "" {
		params["name"] = opts.Name
	}
	for _, f := range [][2]string{
		{opts.CreatedAfter, "created_after"},
		{opts.CreatedBefore, "created_before"},
	} {
		if f[0] == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f[0])
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f[1], err),
				Err:     err,
			}
		}
		params[f[1]] = parsed
	}

	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.OUListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &OUListResult{
		OUs:      result.OUs,
		Total:    result.Total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// OUGet returns a single OU by name (includes devices + templates).
func (s *Service) OUGet(ctx context.Context, org, name string) (*models.OrganizationalUnit, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ou, nil
}

// OUCreate creates a new OU. description is optional.
func (s *Service) OUCreate(ctx context.Context, org, name, description string) (*models.OrganizationalUnit, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	payload := map[string]string{"name": name}
	if description != "" {
		payload["description"] = description
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ou, nil
}

// OUDelete removes an OU. The underlying api.APIError is preserved (via
// service.Error.Unwrap) so callers can extract BlockingResources on 409.
func (s *Service) OUDelete(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OURename updates the name of an OU.
func (s *Service) OURename(ctx context.Context, org, oldName, newName string) (*models.OrganizationalUnit, error) {
	if oldName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	if newName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "new name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, oldName), map[string]string{"name": newName})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ou, nil
}

// OUDeviceList returns the devices currently in an OU. Reuses the device list
// endpoint with the ou filter.
func (s *Service) OUDeviceList(ctx context.Context, org, ouName string) (*DeviceListResult, error) {
	if ouName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	return s.DeviceList(ctx, org, DeviceListOpts{OU: ouName, PerPage: 500})
}

// OUAddDevice attaches a device to an OU.
func (s *Service) OUAddDevice(ctx context.Context, org, ouName, deviceName string) error {
	if ouName == "" || deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou and device names are required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/devices", org, ouName), map[string]string{"device_name": deviceName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OURemoveDevice detaches a device from an OU.
func (s *Service) OURemoveDevice(ctx context.Context, org, ouName, deviceName string) error {
	if ouName == "" || deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou and device names are required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/devices/%s", org, ouName, deviceName))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OUTemplateList returns the templates assigned to an OU.
type OUTemplateListResult struct {
	Items []models.Template
	Total int
}

func (s *Service) OUTemplateList(ctx context.Context, org, ouName string) (*OUTemplateListResult, error) {
	if ouName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "ou name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates", org, ouName), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var body struct {
		Items []models.Template `json:"items"`
		Total int               `json:"total"`
	}
	if err := api.ParseResponse(resp, &body); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &OUTemplateListResult{Items: body.Items, Total: body.Total}, nil
}

// OUTemplateAdd attaches a template to an OU.
func (s *Service) OUTemplateAdd(ctx context.Context, org, ouName, templateName string) error {
	if ouName == "" || templateName == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou and template names are required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates", org, ouName), map[string]string{"template": templateName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// OUTemplateRemove detaches a template from an OU.
func (s *Service) OUTemplateRemove(ctx context.Context, org, ouName, templateName string) error {
	if ouName == "" || templateName == "" {
		return &Error{Code: CodeInvalidInput, Message: "ou and template names are required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates/%s", org, ouName, templateName))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
