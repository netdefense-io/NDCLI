package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// SoftwarePolicyListOpts mirrors the filters accepted by the API.
type SoftwarePolicyListOpts struct {
	Name    string
	SortBy  string
	Page    int
	PerPage int
}

// SoftwarePolicyListResult is the paginated response.
type SoftwarePolicyListResult struct {
	Policies []models.SoftwarePolicy
	Total    int
	Page     int
	PerPage  int
}

// SoftwarePolicyList returns the paginated software policy list.
func (s *Service) SoftwarePolicyList(ctx context.Context, org string, opts SoftwarePolicyListOpts) (*SoftwarePolicyListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.Name != "" {
		params["name"] = opts.Name
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}

	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.SoftwarePolicyListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &SoftwarePolicyListResult{
		Policies: result.Items,
		Total:    result.Total,
		Page:     page,
		PerPage:  perPage,
	}, nil
}

// SoftwarePolicyGet fetches a single policy (with content).
func (s *Service) SoftwarePolicyGet(ctx context.Context, org, name string) (*models.SoftwarePolicy, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies/%s", org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var sp models.SoftwarePolicy
	if err := api.ParseResponse(resp, &sp); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &sp, nil
}

// SoftwarePolicyCreateOpts holds creation fields.
type SoftwarePolicyCreateOpts struct {
	Name    string
	Content string
}

// SoftwarePolicyCreate creates a new policy.
func (s *Service) SoftwarePolicyCreate(ctx context.Context, org string, opts SoftwarePolicyCreateOpts) (*models.SoftwarePolicy, error) {
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	if opts.Content == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "software policy content is required"}
	}
	payload := map[string]interface{}{
		"name":    opts.Name,
		"content": opts.Content,
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var sp models.SoftwarePolicy
	if err := api.ParseResponse(resp, &sp); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &sp, nil
}

// SoftwarePolicyUpdateContent replaces a policy's content.
func (s *Service) SoftwarePolicyUpdateContent(ctx context.Context, org, name, content string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies/%s/content", org, name), map[string]string{"content": content})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SoftwarePolicyRename renames a policy.
func (s *Service) SoftwarePolicyRename(ctx context.Context, org, name, newName string) error {
	if name == "" || newName == "" {
		return &Error{Code: CodeInvalidInput, Message: "software policy name and new name are required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies/%s/rename", org, name), map[string]string{"new_name": newName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SoftwarePolicyDelete removes a policy.
func (s *Service) SoftwarePolicyDelete(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies/%s", org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// softwarePolicyMutate is the shared body for require/block/waive: it
// fetches the policy, applies a per-package mutation function, and
// re-PUTs the content only when something actually changed. The
// outcome list is returned in the same order the caller passed
// `packages` so the CLI can render `package N → action` line by line.
func (s *Service) softwarePolicyMutate(
	ctx context.Context,
	org, name string,
	packages []string,
	apply func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome,
) ([]models.PackageActionOutcome, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	if len(packages) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "at least one package name is required"}
	}

	sp, err := s.SoftwarePolicyGet(ctx, org, name)
	if err != nil {
		return nil, err
	}
	content, err := models.ParseSoftwarePolicyContent(sp.Content)
	if err != nil {
		return nil, &Error{Code: CodeInvalidInput, Message: err.Error(), Err: err}
	}

	outcomes := make([]models.PackageActionOutcome, 0, len(packages))
	anyChanged := false
	for _, pkg := range packages {
		o := apply(content, pkg)
		outcomes = append(outcomes, o)
		if o.Changed() {
			anyChanged = true
		}
	}

	if anyChanged {
		// Server validators (SOFTWARE_PACKAGE_NAME_PATTERN, length cap,
		// intra-doc dup) run on the marshalled content here — letting
		// the server be the source of truth keeps us from drifting on
		// the regex.
		if err := s.SoftwarePolicyUpdateContent(ctx, org, name, content.Marshal()); err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

// SoftwarePolicyRequirePackages adds each package to the policy's
// Present list. A package already required is a no-op; a package
// currently in Absent is moved (the outcome reports From=blocked).
func (s *Service) SoftwarePolicyRequirePackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Require(pkg)
	})
}

// SoftwarePolicyBlockPackages adds each package to the policy's
// Absent list, mirror of Require.
func (s *Service) SoftwarePolicyBlockPackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Block(pkg)
	})
}

// SoftwarePolicyWaivePackages removes each package from whichever list
// it sits in. A package not specified anywhere is a no-op.
func (s *Service) SoftwarePolicyWaivePackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Waive(pkg)
	})
}

// TemplateAddSoftwarePolicy attaches a software policy to a template.
// Lives here (rather than template.go) so the policy domain stays
// self-contained and template.go doesn't have to grow each time a new
// thing becomes template-attachable.
func (s *Service) TemplateAddSoftwarePolicy(ctx context.Context, org, templateName, policyName string) error {
	if templateName == "" || policyName == "" {
		return &Error{Code: CodeInvalidInput, Message: "template name and software policy name are required"}
	}
	resp, err := s.api.Post(ctx,
		fmt.Sprintf("/api/v1/organizations/%s/templates/%s/software-policies", org, templateName),
		map[string]string{"software_policy_name": policyName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// TemplateRemoveSoftwarePolicy detaches a software policy from a template.
func (s *Service) TemplateRemoveSoftwarePolicy(ctx context.Context, org, templateName, policyName string) error {
	if templateName == "" || policyName == "" {
		return &Error{Code: CodeInvalidInput, Message: "template name and software policy name are required"}
	}
	resp, err := s.api.Delete(ctx,
		fmt.Sprintf("/api/v1/organizations/%s/templates/%s/software-policies/%s", org, templateName, policyName))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
