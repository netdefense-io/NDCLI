package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Software policies are NetDefense's reusable package inventory: each
// policy carries a JSON `{present, absent}` document and gets attached
// to templates so sync flows install/uninstall the listed OPNsense
// plugins and FreeBSD packages on every device under those templates.

type softwarePolicyListInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name,omitempty"`
	SortBy       string `json:"sort_by,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type softwarePolicyIDInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type softwarePolicyCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	// Content is optional: when omitted the policy is created empty
	// ({"present":[],"absent":[]}) and the LLM (or operator) then uses
	// require_package / block_package to populate it.
	Content string `json:"content,omitempty"`
}

type softwarePolicyUpdateContentInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Content      string `json:"content"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type softwarePolicyRenameInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	NewName      string `json:"new_name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type templateSoftwarePolicyInput struct {
	Organization       string `json:"organization,omitempty"`
	Template           string `json:"template"`
	SoftwarePolicyName string `json:"software_policy_name"`
	Confirm            bool   `json:"confirm,omitempty"`
}

type softwarePolicyPackageMutationInput struct {
	Organization string   `json:"organization,omitempty"`
	Policy       string   `json:"policy"`
	Packages     []string `json:"packages"`
	Confirm      bool     `json:"confirm,omitempty"`
}

// softwarePolicySetRepositoryInput mirrors the repositories[] wire
// entry. Fingerprints are objects rather than strings: the wire format
// carries {function, fingerprint} pairs, and flattening them to
// "sha256:hex" here would only mean parsing them apart again.
type softwarePolicySetRepositoryInput struct {
	Organization string `json:"organization,omitempty"`
	Policy       string `json:"policy"`
	Name         string `json:"name"`
	// Omitted fields keep whatever the existing entry has — the stored
	// entry is replaced wholesale, so anything not carried forward would
	// be reset. On a new entry url and signature are required; priority
	// defaults to 0 and enabled to true.
	URL       *string `json:"url,omitempty"`
	Priority  *int    `json:"priority,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
	Signature *struct {
		Type         string `json:"type"`
		Fingerprints []struct {
			Function    string `json:"function,omitempty"`
			Fingerprint string `json:"fingerprint"`
		} `json:"fingerprints,omitempty"`
		Pubkey string `json:"pubkey,omitempty"`
	} `json:"signature,omitempty"`
	Confirm bool `json:"confirm,omitempty"`
}

type softwarePolicySetExternalInput struct {
	Organization string `json:"organization,omitempty"`
	Policy       string  `json:"policy"`
	Name         string  `json:"name"`
	Version      *string `json:"version,omitempty"`
	URL          *string `json:"url,omitempty"`
	Force        *bool   `json:"force,omitempty"`
	Confirm      bool    `json:"confirm,omitempty"`
}

// softwarePolicyEntryRemoveInput removes one named repository or
// external-package entry.
type softwarePolicyEntryRemoveInput struct {
	Organization string `json:"organization,omitempty"`
	Policy       string `json:"policy"`
	Name         string `json:"name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

func (s *Server) registerSoftwarePolicyTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.list",
		Description: "List software policies in an organization with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Filter by name (regex)"),
				"sort_by":      stringProperty("Sort field and direction (default name:asc)"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page (max 100)", 50),
			},
		},
	}, s.handleSoftwarePolicyList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.describe",
		Description: "Get a software policy's metadata and full content.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Software policy name"),
			},
			"required": []string{"name"},
		},
	}, s.handleSoftwarePolicyDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.create",
		Description: `Create a new software policy. The policy is created empty by default; use ndcli.software.require_package / block_package to add packages. Pass 'content' explicitly only if you need to bulk-seed the JSON document.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Software policy name"),
				"content":      stringProperty(`Optional bulk-seed content as JSON: {"present": ["pkg1", ...], "absent": ["pkg2", ...]}. Omit to create an empty policy.`),
			},
			"required": []string{"name"},
		},
	}, s.handleSoftwarePolicyCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.update_content",
		Description: "Replace a software policy's content. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Software policy name"),
				"content":      stringProperty(`New content as JSON: {"present": [...], "absent": [...]}`),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name", "content"},
		},
	}, s.handleSoftwarePolicyUpdateContent)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.rename",
		Description: "Rename a software policy. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Current software policy name"),
				"new_name":     stringProperty("New software policy name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name", "new_name"},
		},
	}, s.handleSoftwarePolicyRename)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.delete",
		Description: "Delete a software policy. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Software policy name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleSoftwarePolicyDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.add_software",
		Description: "Attach a software policy to a template. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":         organizationProperty(),
				"template":             stringProperty("Template name"),
				"software_policy_name": stringProperty("Software policy name"),
				"confirm":              confirmProperty(),
			},
			"required": []string{"template", "software_policy_name"},
		},
	}, s.handleTemplateAddSoftwarePolicy)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.template.remove_software",
		Description: "Detach a software policy from a template. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":         organizationProperty(),
				"template":             stringProperty("Template name"),
				"software_policy_name": stringProperty("Software policy name"),
				"confirm":              confirmProperty(),
			},
			"required": []string{"template", "software_policy_name"},
		},
	}, s.handleTemplateRemoveSoftwarePolicy)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.set_repository",
		Description: `Add or update a custom package repository in a software policy. Repositories are configured on the device before any package work, so packages served only by a custom repository can then be named in require_package. The entry is identified by 'name': calling this again with the same name updates it in place, and any field you omit keeps its stored value rather than resetting. 'url' and 'signature' are required only when adding a repository that does not exist yet. Identical values are a no-op. Signature configuration is required; type "none" is accepted only for organizations that have unverified repositories enabled. Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"name":         stringProperty("Repository name — also the identity used to update an existing entry"),
				"url":          stringProperty("Repository URL. ${ABI} is substituted on the device; send it literally, unexpanded"),
				"priority":     intProperty("Repository priority, 0-10. Must be unique within the policy", 0),
				"enabled":      boolProperty("Whether the repository is enabled on the device (default true)"),
				"signature": map[string]interface{}{
					"type":        "object",
					"description": `Trust configuration. type "fingerprints" requires the fingerprints array; "pubkey" requires a PEM key by value; "none" disables verification and needs the organization flag.`,
					"properties": map[string]interface{}{
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Signature type",
							"enum":        []string{"fingerprints", "pubkey", "none"},
						},
						"fingerprints": map[string]interface{}{
							"type":        "array",
							"description": "Trusted key fingerprints",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"function":    stringProperty("Hash function (sha256)"),
									"fingerprint": stringProperty("Fingerprint, 64 hex characters"),
								},
								"required": []string{"fingerprint"},
							},
						},
						"pubkey": stringProperty("PEM-encoded public key, by value"),
					},
					"required": []string{"type"},
				},
				"confirm": confirmProperty(),
			},
			"required": []string{"policy", "name"},
		},
	}, s.handleSoftwarePolicySetRepository)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.remove_repository",
		Description: `Remove a custom package repository from a software policy. Packages already installed from it stay installed — this is not an uninstall. Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"name":         stringProperty("Repository name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "name"},
		},
	}, s.handleSoftwarePolicyRemoveRepository)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.set_external",
		Description: `Add or update an external (URL-installed) package in a software policy, for software no repository serves. 'version' and 'url' are required when adding a package that does not exist yet; omitted fields keep their stored values. Version matters because pkg add takes a URL, not a package identity — the policy has to state what the URL delivers so the agent can tell installed from not-installed. The same name at a new version replaces the entry. Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"name":         stringProperty("Package name the URL is expected to deliver"),
				"version":      stringProperty("Package version the URL is expected to deliver"),
				"url":          stringProperty("Package URL to install from"),
				"force":        boolProperty("Pass -f to pkg add — reinstall even when pkg considers it installed"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "name"},
		},
	}, s.handleSoftwarePolicySetExternal)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.remove_external",
		Description: `Remove an external package from a software policy. Like remove_repository this is not an uninstall — use block_package to have it removed from devices. Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"name":         stringProperty("External package name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "name"},
		},
	}, s.handleSoftwarePolicyRemoveExternal)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.require_package",
		Description: `Mark one or more packages as required by a software policy. Required packages get installed on every device the policy covers. A package already required is a no-op; a package currently blocked by the same policy is moved (block → require). Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"packages":     stringArrayProperty("Package names to require (variadic)"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "packages"},
		},
	}, s.handleSoftwarePolicyRequirePackage)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.block_package",
		Description: `Mark one or more packages as blocked by a software policy. Blocked packages get uninstalled on every device the policy covers. A package already blocked is a no-op; a package currently required by the same policy is moved (require → block). Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"packages":     stringArrayProperty("Package names to block (variadic)"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "packages"},
		},
	}, s.handleSoftwarePolicyBlockPackage)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.software.waive_package",
		Description: `Stop having an opinion about one or more packages — removes each from whichever list (required or blocked) it sits in. Does NOT uninstall or reinstall anything on devices; just stops the policy from caring. Requires confirm=true.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"policy":       stringProperty("Software policy name"),
				"packages":     stringArrayProperty("Package names to waive (variadic)"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"policy", "packages"},
		},
	}, s.handleSoftwarePolicyWaivePackage)
}

func (s *Server) handleSoftwarePolicyList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.SoftwarePolicyList(apiCtx, org, service.SoftwarePolicyListOpts{
		Name:    input.Name,
		SortBy:  input.SortBy,
		Page:    input.Page,
		PerPage: input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.Policies))
	for _, p := range result.Policies {
		items = append(items, softwarePolicySummary(&p))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"software_policies": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleSoftwarePolicyDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	policy, err := s.svc.SoftwarePolicyGet(apiCtx, org, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"software_policy": softwarePolicyFull(policy),
	}, "")
}

func (s *Server) handleSoftwarePolicyCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	content := input.Content
	if content == "" {
		content = models.EmptySoftwarePolicyContent
	}
	policy, err := s.svc.SoftwarePolicyCreate(apiCtx, org, service.SoftwarePolicyCreateOpts{
		Name:    input.Name,
		Content: content,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"software_policy": softwarePolicySummary(policy),
		"action":          "created",
	}, fmt.Sprintf("Software policy '%s' created", input.Name))
}

func (s *Server) handleSoftwarePolicyUpdateContent(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyUpdateContentInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update content of software policy", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	warnings, err := s.svc.SoftwarePolicyUpdateContent(apiCtx, org, input.Name, input.Content)
	if err != nil {
		return s.errorResult(err)
	}
	data := map[string]interface{}{
		"name":   input.Name,
		"action": "updated",
	}
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	return s.successResult(data, fmt.Sprintf("Software policy '%s' content updated", input.Name))
}

func (s *Server) handleSoftwarePolicyRename(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyRenameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("rename software policy", fmt.Sprintf("%s → %s", input.Name, input.NewName))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SoftwarePolicyRename(apiCtx, org, input.Name, input.NewName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":     input.Name,
		"new_name": input.NewName,
		"action":   "renamed",
	}, fmt.Sprintf("Software policy renamed: %s → %s", input.Name, input.NewName))
}

func (s *Server) handleSoftwarePolicyDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete software policy", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SoftwarePolicyDelete(apiCtx, org, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "deleted",
	}, fmt.Sprintf("Software policy '%s' deleted", input.Name))
}

func (s *Server) handleTemplateAddSoftwarePolicy(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateSoftwarePolicyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("attach software policy '%s' to template", input.SoftwarePolicyName), input.Template)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TemplateAddSoftwarePolicy(apiCtx, org, input.Template, input.SoftwarePolicyName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template":             input.Template,
		"software_policy_name": input.SoftwarePolicyName,
		"action":               "attached",
	}, fmt.Sprintf("Software policy '%s' attached to template '%s'", input.SoftwarePolicyName, input.Template))
}

func (s *Server) handleTemplateRemoveSoftwarePolicy(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[templateSoftwarePolicyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("detach software policy '%s' from template", input.SoftwarePolicyName), input.Template)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TemplateRemoveSoftwarePolicy(apiCtx, org, input.Template, input.SoftwarePolicyName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"template":             input.Template,
		"software_policy_name": input.SoftwarePolicyName,
		"action":               "detached",
	}, fmt.Sprintf("Software policy '%s' detached from template '%s'", input.SoftwarePolicyName, input.Template))
}

func softwarePolicySummary(p *models.SoftwarePolicy) map[string]interface{} {
	return map[string]interface{}{
		"name":         p.Name,
		"organization": p.Organization,
		"created_at":   p.CreatedAt,
		"updated_at":   p.UpdatedAt,
	}
}

func softwarePolicyFull(p *models.SoftwarePolicy) map[string]interface{} {
	full := softwarePolicySummary(p)
	full["content"] = p.Content
	// Templates this policy is currently attached to. NDManager only
	// populates the field on the describe (GET) response — list omits
	// it. Surface it explicitly so the LLM can summarise the impact
	// radius of the policy without a second tool call.
	if p.TemplateNames != nil {
		full["template_names"] = p.TemplateNames
	}
	return full
}

func (s *Server) handleSoftwarePolicyRequirePackage(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleSoftwarePolicyPackageMutation(ctx, req, "require")
}

func (s *Server) handleSoftwarePolicyBlockPackage(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleSoftwarePolicyPackageMutation(ctx, req, "block")
}

func (s *Server) handleSoftwarePolicyWaivePackage(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleSoftwarePolicyPackageMutation(ctx, req, "waive")
}

func (s *Server) handleSoftwarePolicyPackageMutation(ctx context.Context, req *mcp.CallToolRequest, op string) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyPackageMutationInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if input.Policy == "" {
		return s.errorResult(fmt.Errorf("policy name is required"))
	}
	if len(input.Packages) == 0 {
		return s.errorResult(fmt.Errorf("at least one package name is required"))
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		preview := fmt.Sprintf("%s package(s) %v in policy", op, input.Packages)
		return s.previewResult(preview, input.Policy)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	var (
		outcomes []models.PackageActionOutcome
		warnings []string
	)
	switch op {
	case "require":
		outcomes, warnings, err = s.svc.SoftwarePolicyRequirePackages(apiCtx, org, input.Policy, input.Packages)
	case "block":
		outcomes, warnings, err = s.svc.SoftwarePolicyBlockPackages(apiCtx, org, input.Policy, input.Packages)
	case "waive":
		outcomes, warnings, err = s.svc.SoftwarePolicyWaivePackages(apiCtx, org, input.Policy, input.Packages)
	default:
		return s.errorResult(fmt.Errorf("internal error: unknown op %q", op))
	}
	if err != nil {
		return s.errorResult(err)
	}

	// Translate outcomes into a structured payload the LLM can summarize.
	// Keep keys snake_case to match the rest of the MCP surface.
	results := make([]map[string]interface{}, 0, len(outcomes))
	changed, moved, noop := 0, 0, 0
	for _, o := range outcomes {
		entry := map[string]interface{}{
			"package": o.Package,
			"action":  o.Action,
		}
		if o.From != "" {
			entry["from"] = string(o.From)
		}
		results = append(results, entry)
		switch o.Action {
		case "no-change":
			noop++
		case "moved":
			moved++
		default:
			changed++
		}
	}
	summary := fmt.Sprintf("%d change(s), %d move(s), %d no-op(s)", changed, moved, noop)
	data := map[string]interface{}{
		"policy":  input.Policy,
		"op":      op,
		"results": results,
		"summary": summary,
	}
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	return s.successResult(data, summary)
}

func (s *Server) handleSoftwarePolicySetRepository(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicySetRepositoryInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("set repository '%s' in policy", input.Name), input.Policy)
	}

	patch := models.RepositoryPatch{
		Name:     input.Name,
		URL:      input.URL,
		Priority: input.Priority,
		Enabled:  input.Enabled,
	}
	if input.Signature != nil {
		signature := models.RepositorySignature{
			Type:   input.Signature.Type,
			Pubkey: input.Signature.Pubkey,
		}
		for _, fp := range input.Signature.Fingerprints {
			function := fp.Function
			if function == "" {
				function = "sha256"
			}
			signature.Fingerprints = append(signature.Fingerprints,
				models.RepositoryFingerprint{Function: function, Fingerprint: fp.Fingerprint})
		}
		patch.Signature = &signature
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	outcome, warnings, err := s.svc.SoftwarePolicySetRepository(apiCtx, org, input.Policy, patch)
	if err != nil {
		return s.errorResult(err)
	}
	return s.entryOutcomeResult(input.Policy, "repository", outcome, warnings)
}

func (s *Server) handleSoftwarePolicyRemoveRepository(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleSoftwarePolicyEntryRemove(ctx, req, "repository")
}

func (s *Server) handleSoftwarePolicyRemoveExternal(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleSoftwarePolicyEntryRemove(ctx, req, "external")
}

func (s *Server) handleSoftwarePolicyEntryRemove(ctx context.Context, req *mcp.CallToolRequest, kind string) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicyEntryRemoveInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("remove %s '%s' from policy", kind, input.Name), input.Policy)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	var (
		outcome  models.EntryActionOutcome
		warnings []string
	)
	if kind == "repository" {
		outcome, warnings, err = s.svc.SoftwarePolicyRemoveRepository(apiCtx, org, input.Policy, input.Name)
	} else {
		outcome, warnings, err = s.svc.SoftwarePolicyRemoveExternal(apiCtx, org, input.Policy, input.Name)
	}
	if err != nil {
		return s.errorResult(err)
	}
	return s.entryOutcomeResult(input.Policy, kind, outcome, warnings)
}

func (s *Server) handleSoftwarePolicySetExternal(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[softwarePolicySetExternalInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("set external package '%s' in policy", input.Name), input.Policy)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	outcome, warnings, err := s.svc.SoftwarePolicySetExternal(apiCtx, org, input.Policy, models.ExternalPatch{
		Name:    input.Name,
		Version: input.Version,
		URL:     input.URL,
		Force:   input.Force,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.entryOutcomeResult(input.Policy, "external", outcome, warnings)
}

// entryOutcomeResult is the shared success payload for the four
// repository / external-package tools. Warnings are advisory — the
// write already landed — so they ride alongside the outcome rather
// than turning it into an error.
func (s *Server) entryOutcomeResult(policy, kind string, outcome models.EntryActionOutcome, warnings []string) (*mcp.CallToolResult, error) {
	data := map[string]interface{}{
		"policy": policy,
		"kind":   kind,
		"name":   outcome.Name,
		"action": outcome.Action,
	}
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	return s.successResult(data, fmt.Sprintf("%s '%s': %s", kind, outcome.Name, outcome.Action))
}
