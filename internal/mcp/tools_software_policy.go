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
		Name: "ndcli.software.require_package",
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
		Name: "ndcli.software.block_package",
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
		Name: "ndcli.software.waive_package",
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

	if err := s.svc.SoftwarePolicyUpdateContent(apiCtx, org, input.Name, input.Content); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "updated",
	}, fmt.Sprintf("Software policy '%s' content updated", input.Name))
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

	var outcomes []models.PackageActionOutcome
	switch op {
	case "require":
		outcomes, err = s.svc.SoftwarePolicyRequirePackages(apiCtx, org, input.Policy, input.Packages)
	case "block":
		outcomes, err = s.svc.SoftwarePolicyBlockPackages(apiCtx, org, input.Policy, input.Packages)
	case "waive":
		outcomes, err = s.svc.SoftwarePolicyWaivePackages(apiCtx, org, input.Policy, input.Packages)
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
	return s.successResult(map[string]interface{}{
		"policy":   input.Policy,
		"op":       op,
		"results":  results,
		"summary":  summary,
	}, summary)
}
