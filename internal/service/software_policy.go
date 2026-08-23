package service

import (
	"context"
	"errors"
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

// SoftwarePolicyUpdateContent replaces a policy's content and returns
// any advisory warnings the server attached to the write.
//
// Warnings are not errors: the update has already committed by the time
// NDManager computes them. They report a conflict between this policy
// and a sibling policy that lands on the same device — the same
// conflict that will hard-fail at merge time — surfaced now, while the
// operator is still here to act on it. An empty slice is the normal
// case.
func (s *Service) SoftwarePolicyUpdateContent(ctx context.Context, org, name, content string) ([]string, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/software-policies/%s/content", org, name), map[string]string{"content": content})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var out struct {
		Warnings []string `json:"warnings"`
	}
	// The status check has to stay strict — a 4xx/5xx is a real failure.
	// The body decode does not: by the time the server has sent 2xx the
	// write has committed, so an empty or unexpected body must not be
	// reported to the operator as a failed update. A server that doesn't
	// send `warnings` at all simply yields none.
	if err := api.ParseResponse(resp, &out); err != nil {
		if apiErr := (*api.APIError)(nil); errors.As(err, &apiErr) {
			return nil, wrapAPI("%v", err)
		}
		return nil, nil
	}
	return out.Warnings, nil
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
) ([]models.PackageActionOutcome, []string, error) {
	if name == "" {
		return nil, nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}
	if len(packages) == 0 {
		return nil, nil, &Error{Code: CodeInvalidInput, Message: "at least one package name is required"}
	}

	sp, err := s.SoftwarePolicyGet(ctx, org, name)
	if err != nil {
		return nil, nil, err
	}
	content, err := models.ParseSoftwarePolicyContent(sp.Content)
	if err != nil {
		return nil, nil, &Error{Code: CodeInvalidInput, Message: err.Error(), Err: err}
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
		warnings, err := s.SoftwarePolicyUpdateContent(ctx, org, name, content.Marshal())
		if err != nil {
			return outcomes, nil, err
		}
		return outcomes, warnings, nil
	}
	return outcomes, nil, nil
}

// SoftwarePolicyRequirePackages adds each package to the policy's
// Present list. A package already required is a no-op; a package
// currently in Absent is moved (the outcome reports From=blocked).
func (s *Service) SoftwarePolicyRequirePackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, []string, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Require(pkg)
	})
}

// SoftwarePolicyBlockPackages adds each package to the policy's
// Absent list, mirror of Require.
func (s *Service) SoftwarePolicyBlockPackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, []string, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Block(pkg)
	})
}

// SoftwarePolicyWaivePackages removes each package from whichever list
// it sits in. A package not specified anywhere is a no-op.
func (s *Service) SoftwarePolicyWaivePackages(ctx context.Context, org, name string, packages []string) ([]models.PackageActionOutcome, []string, error) {
	return s.softwarePolicyMutate(ctx, org, name, packages, func(c *models.SoftwarePolicyContent, pkg string) models.PackageActionOutcome {
		return c.Waive(pkg)
	})
}

// softwarePolicyEntryMutate is the shared body for the repository and
// external-package verbs: fetch, apply one named-entry mutation,
// re-PUT only if it changed anything. Same shape as
// softwarePolicyMutate, but over the entry axis rather than the
// present/absent axis.
func (s *Service) softwarePolicyEntryMutate(
	ctx context.Context,
	org, name string,
	apply func(c *models.SoftwarePolicyContent) models.EntryActionOutcome,
) (models.EntryActionOutcome, []string, error) {
	var zero models.EntryActionOutcome
	if name == "" {
		return zero, nil, &Error{Code: CodeInvalidInput, Message: "software policy name is required"}
	}

	sp, err := s.SoftwarePolicyGet(ctx, org, name)
	if err != nil {
		return zero, nil, err
	}
	content, err := models.ParseSoftwarePolicyContent(sp.Content)
	if err != nil {
		return zero, nil, &Error{Code: CodeInvalidInput, Message: err.Error(), Err: err}
	}

	outcome := apply(content)
	if !outcome.Changed() {
		return outcome, nil, nil
	}
	// Every syntactic rule — name pattern, URL scheme, priority range
	// and uniqueness, signature shape, entry caps — is the server's to
	// enforce. Mirroring them here would just create a second place to
	// drift from.
	warnings, err := s.SoftwarePolicyUpdateContent(ctx, org, name, content.Marshal())
	if err != nil {
		return outcome, nil, err
	}
	return outcome, warnings, nil
}

// SoftwarePolicySetRepository adds a custom pkg repository to the
// policy, or updates the existing entry with the same name. Nil patch
// fields keep whatever the existing entry has — the stored entry is
// replaced wholesale, so a caller that restated only the field it
// meant to change would otherwise reset everything else. Re-running
// with identical values is a no-op and skips the round-trip.
func (s *Service) SoftwarePolicySetRepository(ctx context.Context, org, name string, patch models.RepositoryPatch) (models.EntryActionOutcome, []string, error) {
	if patch.Name == "" {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "repository name is required"}
	}
	if patch.URL != nil && *patch.URL == "" {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "repository url is required"}
	}
	if patch.Signature != nil {
		if err := ValidateRepositorySignature(*patch.Signature); err != nil {
			return models.EntryActionOutcome{}, nil, err
		}
	}

	var applyErr error
	outcome, warnings, err := s.softwarePolicyEntryMutate(ctx, org, name, func(c *models.SoftwarePolicyContent) models.EntryActionOutcome {
		out, err := c.ApplyRepositoryPatch(patch)
		if err != nil {
			applyErr = err
			// no-change keeps softwarePolicyEntryMutate from PUTting a
			// document the patch never successfully modified.
			return models.EntryActionOutcome{Name: patch.Name, Action: "no-change"}
		}
		return out
	})
	if applyErr != nil {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: applyErr.Error(), Err: applyErr}
	}
	return outcome, warnings, err
}

// ValidateRepositorySignature enforces the internal consistency of a
// signature block. It lives in the service layer rather than in the
// cli, so the MCP surface cannot submit a combination the cli would
// have rejected — per the service-layer convention, every front-end
// gets the same rules.
//
// What it deliberately does NOT check: whether the organization is
// allowed to use an unverified repository, and whether the fingerprint
// hex or PEM body is well-formed. Those are the server's, and
// duplicating them here would create a second place to drift from.
func ValidateRepositorySignature(sig models.RepositorySignature) error {
	switch sig.Type {
	case "fingerprints":
		if len(sig.Fingerprints) == 0 {
			return &Error{Code: CodeInvalidInput, Message: "signature type fingerprints requires at least one fingerprint"}
		}
		if sig.Pubkey != "" {
			return &Error{Code: CodeInvalidInput, Message: "signature type fingerprints cannot also carry a public key"}
		}
		for _, fp := range sig.Fingerprints {
			if fp.Fingerprint == "" {
				return &Error{Code: CodeInvalidInput, Message: "fingerprint value is required"}
			}
		}
	case "pubkey":
		if sig.Pubkey == "" {
			return &Error{Code: CodeInvalidInput, Message: "signature type pubkey requires a public key"}
		}
		if len(sig.Fingerprints) > 0 {
			return &Error{Code: CodeInvalidInput, Message: "signature type pubkey cannot also carry fingerprints"}
		}
	case "none":
		if len(sig.Fingerprints) > 0 || sig.Pubkey != "" {
			return &Error{Code: CodeInvalidInput, Message: "signature type none cannot carry signature material"}
		}
	default:
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("unknown signature type %q: expected fingerprints, pubkey, or none", sig.Type)}
	}
	return nil
}

// SoftwarePolicyRemoveRepository drops a repository entry by name.
func (s *Service) SoftwarePolicyRemoveRepository(ctx context.Context, org, name, repoName string) (models.EntryActionOutcome, []string, error) {
	if repoName == "" {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "repository name is required"}
	}
	return s.softwarePolicyEntryMutate(ctx, org, name, func(c *models.SoftwarePolicyContent) models.EntryActionOutcome {
		return c.RemoveRepository(repoName)
	})
}

// SoftwarePolicySetExternal adds an external (URL-installed) package to
// the policy, or updates the existing entry with the same name. Nil
// patch fields keep whatever the existing entry has, for the same
// reason as SoftwarePolicySetRepository.
func (s *Service) SoftwarePolicySetExternal(ctx context.Context, org, name string, patch models.ExternalPatch) (models.EntryActionOutcome, []string, error) {
	switch {
	case patch.Name == "":
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "external package name is required"}
	case patch.Version != nil && *patch.Version == "":
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "external package version is required"}
	case patch.URL != nil && *patch.URL == "":
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "external package url is required"}
	}

	var applyErr error
	outcome, warnings, err := s.softwarePolicyEntryMutate(ctx, org, name, func(c *models.SoftwarePolicyContent) models.EntryActionOutcome {
		out, err := c.ApplyExternalPatch(patch)
		if err != nil {
			applyErr = err
			return models.EntryActionOutcome{Name: patch.Name, Action: "no-change"}
		}
		return out
	})
	if applyErr != nil {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: applyErr.Error(), Err: applyErr}
	}
	return outcome, warnings, err
}

// SoftwarePolicyRemoveExternal drops an external package entry by name.
func (s *Service) SoftwarePolicyRemoveExternal(ctx context.Context, org, name, pkgName string) (models.EntryActionOutcome, []string, error) {
	if pkgName == "" {
		return models.EntryActionOutcome{}, nil, &Error{Code: CodeInvalidInput, Message: "external package name is required"}
	}
	return s.softwarePolicyEntryMutate(ctx, org, name, func(c *models.SoftwarePolicyContent) models.EntryActionOutcome {
		return c.RemoveExternal(pkgName)
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
