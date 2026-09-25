package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// The 4 scopes × 5 ops would be 20 tools. We collapse to 6 ops with a
// `scope` enum input, which keeps the LLM tool catalogue tractable while
// still exercising every endpoint via the service layer.
type varScopeKey struct {
	Scope        string `json:"scope"` // org|ou|template|device
	Organization string `json:"organization,omitempty"`
	Entity       string `json:"entity,omitempty"` // ou name / template name / device name
}

type varListInput struct {
	varScopeKey
	Name    string `json:"name,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

type varNameInput struct {
	varScopeKey
	Name    string `json:"name"`
	Confirm bool   `json:"confirm,omitempty"`
}

type varCreateInput struct {
	varScopeKey
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

type varSetInput struct {
	varScopeKey
	Name        string  `json:"name"`
	Value       *string `json:"value,omitempty"`
	Description *string `json:"description,omitempty"`
	Confirm     bool    `json:"confirm,omitempty"`
}

type varOverviewInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

var variableScopeEnum = []string{"org", "ou", "template", "device"}

// scopeProperty is the standard scope enum property for variable tools.
func scopeProperty() map[string]interface{} {
	return stringEnumProperty("Variable scope", variableScopeEnum)
}

// entityProperty is the standard entity property (OU/template/device name —
// required for non-org scopes).
func entityProperty() map[string]interface{} {
	return stringProperty("Entity name (OU/template/device — required for non-org scopes; ignored for org)")
}

// resolveVariableScope validates the scope string and returns the typed
// service value. Also enforces that non-org scopes carry an entity.
func resolveVariableScope(s, entity string) (service.VariableScope, error) {
	scope := service.VariableScope(s)
	if !scope.IsValid() {
		return "", &service.Error{Code: service.CodeInvalidInput, Message: fmt.Sprintf("invalid scope: %q (use org/ou/template/device)", s)}
	}
	if scope.RequiresEntity() && entity == "" {
		return "", &service.Error{Code: service.CodeInvalidInput, Message: fmt.Sprintf("entity is required for %s-scope variables", s)}
	}
	return scope, nil
}

// errMCPSecretValueWrite is the refusal for any MCP call that would carry
// a secret variable's literal value over the tool-call transport — the
// whole reason `ndcli variable ... --value-stdin` and the interactive
// no-echo prompt exist is that a value typed into a tool call argument
// instead sits in the calling LLM's context and transcript for the life
// of the session.
var errMCPSecretValueWrite = &service.Error{
	Code:    service.CodeInvalidInput,
	Message: "this variable's value is secret — set it with 'ndcli variable <scope> create/set ... --value-stdin' or the interactive prompt, not over MCP",
}

// resolveVariableSecretStatus reports whether the variable at (scope, org,
// entity, name) is secret. This is a security control: a confirmed "no
// such variable" (404) reads as "not secret" — the common
// case of a caller creating that variable right now — but any other
// lookup failure (a timeout, a 5xx, a network blip, ...) is returned as
// an error rather than silently read as "not secret". Failing open on an
// ambiguous lookup would be exactly the failure mode where the plaintext
// value this feature exists to keep off the MCP transport goes through
// anyway; the caller refuses the write on any non-nil error here, the
// same as it does when the status comes back true.
//
// This read and the write it gates are not atomic: another actor could
// flip the variable's secret flag between this GET and the subsequent
// create/set. Accepted — NDManager has no compare-and-swap primitive for
// this field, and the caller who could hit that window is already
// authorized to change the flag directly.
func resolveVariableSecretStatus(ctx context.Context, svc *service.Service, scope service.VariableScope, org, entity, name string) (bool, error) {
	v, err := svc.VariableGet(ctx, scope, org, entity, name)
	if err == nil {
		return v.Secret, nil
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

// registerVariableTools registers the variable tool set.
func (s *Server) registerVariableTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.list",
		Description: "List variables at a given scope (org, ou, template, device). Non-org scopes require entity (OU/template/device name).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope":        scopeProperty(),
				"organization": organizationProperty(),
				"entity":       entityProperty(),
				"name":         stringProperty("Filter by name pattern (regex)"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 50),
			},
			"required": []string{"scope"},
		},
	}, s.handleVariableList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.describe",
		Description: "Show a single variable's value/description/scope.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope":        scopeProperty(),
				"organization": organizationProperty(),
				"entity":       entityProperty(),
				"name":         stringProperty("Variable name"),
			},
			"required": []string{"scope", "name"},
		},
	}, s.handleVariableDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.create",
		Description: "Create a variable at a given scope. Always refuses `secret=true` — a secret's value must never travel as an MCP tool argument. Also refuses a plain create/override where the org-scope variable of that name is already secret. Create or set a secret variable with 'ndcli variable ... create/set ... --value-stdin' or the interactive prompt instead.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope":        scopeProperty(),
				"organization": organizationProperty(),
				"entity":       entityProperty(),
				"name":         stringProperty("Variable name"),
				"value":        stringProperty("Variable value"),
				"description":  stringProperty("Description (optional)"),
				"secret":       boolProperty("Always refused over MCP — org-scope secrets can only be created via the CLI's --value-stdin or interactive prompt"),
			},
			"required": []string{"scope", "name", "value"},
		},
	}, s.handleVariableCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.set",
		Description: "Update a variable's value and/or description. Requires confirm=true. Refuses to write a new value to a variable that is already secret — set it with 'ndcli variable ... --value-stdin' or the interactive prompt instead; a description-only update is unaffected.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope":        scopeProperty(),
				"organization": organizationProperty(),
				"entity":       entityProperty(),
				"name":         stringProperty("Variable name"),
				"value":        stringProperty("New value (omit to leave unchanged)"),
				"description":  stringProperty("New description (omit to leave unchanged)"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"scope", "name"},
		},
	}, s.handleVariableSet)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.delete",
		Description: "Delete a variable. Deleting an org-scope variable cascades to overrides at OU/template/device — the response includes the cascade count for situational awareness. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope":        scopeProperty(),
				"organization": organizationProperty(),
				"entity":       entityProperty(),
				"name":         stringProperty("Variable name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"scope", "name"},
		},
	}, s.handleVariableDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.variable.overview",
		Description: "List every variable in an organization with all its definitions across scopes (org/ou/template/device).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Filter by name pattern (regex)"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 50),
			},
		},
	}, s.handleVariableOverview)
}

func (s *Server) handleVariableList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	scope, err := resolveVariableScope(input.Scope, input.Entity)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.VariableList(apiCtx, scope, org, input.Entity, service.VariableListOpts{
		NameFilter: input.Name,
		Page:       input.Page,
		PerPage:    input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Variables))
	for _, v := range result.Variables {
		items = append(items, variableSummary(&v))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"scope":     input.Scope,
		"entity":    input.Entity,
		"variables": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleVariableDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	scope, err := resolveVariableScope(input.Scope, input.Entity)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	v, err := s.svc.VariableGet(apiCtx, scope, org, input.Entity, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"variable": variableSummary(v),
	}, "")
}

func (s *Server) handleVariableCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	scope, err := resolveVariableScope(input.Scope, input.Entity)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// A literal secret value must never travel as an MCP tool argument:
	// refuse outright when this call is marking the variable
	// secret, and — since secret status is inherited by every override —
	// also when it would silently create an override of an org-scope
	// variable that is already secret. A lookup failure that isn't a
	// confirmed 404 fails closed (refuses) rather than proceeding as if
	// the variable were known not to be secret.
	if input.Secret {
		return s.errorResult(errMCPSecretValueWrite)
	}
	if scope != service.VarScopeOrg {
		secret, err := resolveVariableSecretStatus(apiCtx, s.svc, service.VarScopeOrg, org, "", input.Name)
		if err != nil {
			return s.errorResult(err)
		}
		if secret {
			return s.errorResult(errMCPSecretValueWrite)
		}
	}

	v, err := s.svc.VariableCreate(apiCtx, scope, org, input.Entity, service.VariableCreateOpts{
		Name:        input.Name,
		Value:       input.Value,
		Description: input.Description,
		Secret:      input.Secret,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"variable": variableSummary(v),
		"action":   "created",
	}, fmt.Sprintf("Variable '%s' created at %s scope", input.Name, input.Scope))
}

func (s *Server) handleVariableSet(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varSetInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	scope, err := resolveVariableScope(input.Scope, input.Entity)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// Same refusal as create: a value write to a variable that is
	// already secret must go through the CLI's --value-stdin/prompt path,
	// never an MCP tool argument. A description-only update (Value == nil)
	// is unaffected. Checked before the confirm gate so an about-to-be-
	// refused call never even reaches the preview. As in create, a lookup
	// failure that isn't a confirmed 404 fails closed.
	//
	// Consequence: a confirm=false preview of a value write is no longer a
	// pure local, zero-request call (same tradeoff scheduling's `at`
	// preview makes for the device-timezone echo) — it now makes the same
	// GET the confirmed call would, sharing this handler's one apiTimeout
	// budget with the PATCH that follows on the confirmed call. Accepted:
	// refusing before the preview matters more than keeping the preview
	// network-free, and a single JSON GET+PATCH pair sits nowhere near the
	// 30s budget in practice.
	if input.Value != nil {
		secret, err := resolveVariableSecretStatus(apiCtx, s.svc, scope, org, input.Entity, input.Name)
		if err != nil {
			return s.errorResult(err)
		}
		if secret {
			return s.errorResult(errMCPSecretValueWrite)
		}
	}

	if !input.Confirm {
		return s.previewResult("update variable", input.Name)
	}

	v, err := s.svc.VariableSet(apiCtx, scope, org, input.Entity, input.Name, service.VariableSetOpts{
		Value:       input.Value,
		Description: input.Description,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"variable": variableSummary(v),
		"action":   "updated",
	}, fmt.Sprintf("Variable '%s' updated", input.Name))
}

func (s *Server) handleVariableDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	scope, err := resolveVariableScope(input.Scope, input.Entity)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// For org-scope, surface the cascade count even on the preview path so
	// the caller can decide whether to confirm.
	overrides := 0
	if scope == service.VarScopeOrg {
		overrides = s.svc.VariableCountNonOrgDefinitions(apiCtx, org, input.Name)
	}

	if !input.Confirm {
		target := input.Name
		if overrides > 0 {
			target = fmt.Sprintf("%s (cascades to %d override(s))", input.Name, overrides)
		}
		return s.previewResult("delete variable", target)
	}

	if err := s.svc.VariableDelete(apiCtx, scope, org, input.Entity, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":          input.Name,
		"scope":         input.Scope,
		"entity":        input.Entity,
		"cascade_count": overrides,
		"action":        "deleted",
	}, fmt.Sprintf("Variable '%s' deleted", input.Name))
}

func (s *Server) handleVariableOverview(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[varOverviewInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.VariableOverview(apiCtx, org, service.VariableOverviewOpts{
		NameFilter: input.Name,
		Page:       input.Page,
		PerPage:    input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Variables))
	for _, v := range result.Variables {
		defs := make([]map[string]interface{}, 0, len(v.Definitions))
		for _, d := range v.Definitions {
			defs = append(defs, map[string]interface{}{
				"scope":       d.Scope,
				"scope_name":  d.ScopeName,
				"value":       d.Value,
				"description": d.Description,
				"secret":      d.Secret,
			})
		}
		items = append(items, map[string]interface{}{
			"name":        v.Name,
			"definitions": defs,
		})
	}
	return s.successResultWithPagination(map[string]interface{}{
		"variables": items,
	}, result.Page, result.PerPage, result.Total)
}

func variableSummary(v *models.Variable) map[string]interface{} {
	return map[string]interface{}{
		"name":        v.Name,
		"value":       v.Value,
		"description": v.Description,
		"scope":       v.Scope,
		"scope_name":  v.ScopeName,
		"secret":      v.Secret,
		"created_at":  v.CreatedAt,
		"updated_at":  v.UpdatedAt,
	}
}
