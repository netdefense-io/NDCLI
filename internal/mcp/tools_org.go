package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type orgListInput struct {
	SortBy  string `json:"sort_by,omitempty"`
	Name    string `json:"name,omitempty"`
	Role    string `json:"role,omitempty"`
	Status  string `json:"status,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

type orgNameInput struct {
	Name    string `json:"name"`
	Confirm bool   `json:"confirm,omitempty"`
}

type orgCreateInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
}

type orgSetDefaultOUInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
}

type orgInviteSendInput struct {
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Role         string `json:"role,omitempty"`
}

type orgInviteOrgInput struct {
	OrgName string `json:"organization"`
	Confirm bool   `json:"confirm,omitempty"`
}

type orgInviteRevokeInput struct {
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type orgAccountListInput struct {
	Organization string `json:"organization,omitempty"`
}

type orgAccountDisableInput struct {
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Remove       bool   `json:"remove,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type orgAccountEnableInput struct {
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
}

type orgAccountRoleInput struct {
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Role         string `json:"role"`
}

// registerOrgTools registers every organization-level tool.
func (s *Server) registerOrgTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.list",
		Description: "List organizations the caller can access, with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"sort_by":  stringProperty("Sort field and direction (default name:asc)"),
				"name":     stringProperty("Filter by name (regex)"),
				"role":     stringEnumProperty("Filter by role", []string{"SU", "RW", "RO", "superuser", "readwrite", "readonly"}),
				"status":   stringEnumProperty("Filter by membership status", []string{"ENABLED", "DISABLED", "INVITED", "DECLINED"}),
				"page":     intProperty("Page number", 1),
				"per_page": intProperty("Items per page", 30),
			},
		},
	}, s.handleOrgList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.describe",
		Description: "Get full details for a single organization, including device/member counts and the agent token.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": stringProperty("Organization name"),
			},
			"required": []string{"name"},
		},
	}, s.handleOrgDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.create",
		Description: "Create a new organization. Returns the agent token in the response — surface it to the user once and do not log it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":         stringProperty("Organization name (must be unique)"),
				"display_name": stringProperty("Optional human-friendly display name"),
				"description":  stringProperty("Optional description"),
			},
			"required": []string{"name"},
		},
	}, s.handleOrgCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.delete",
		Description: "Permanently delete an organization. Requires confirm=true. Cannot be undone.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":    stringProperty("Organization name"),
				"confirm": confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleOrgDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.quota",
		Description: "Show the plan limits and current usage for an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": stringProperty("Organization name"),
			},
			"required": []string{"name"},
		},
	}, s.handleOrgQuota)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.set_default_ou",
		Description: "Set the OU new devices land in by default for an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"ou":           stringProperty("OU name"),
			},
			"required": []string{"ou"},
		},
	}, s.handleOrgSetDefaultOU)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.invite_send",
		Description: "Invite a user to join the organization at a given role.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"email":        stringProperty("Invitee email address"),
				"role":         stringEnumProperty("Role (default RO)", []string{"SU", "RW", "RO", "superuser", "readwrite", "readonly"}),
			},
			"required": []string{"email"},
		},
	}, s.handleOrgInviteSend)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.invite_list",
		Description: "List the caller's pending invitations (received and, for superusers, sent).",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleOrgInviteList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.invite_accept",
		Description: "Accept a pending invitation to join an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name"),
			},
			"required": []string{"organization"},
		},
	}, s.handleOrgInviteAccept)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.invite_decline",
		Description: "Decline a pending invitation. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"organization"},
		},
	}, s.handleOrgInviteDecline)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.invite_revoke",
		Description: "Revoke a pending invitation previously sent to an email. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"email":        stringProperty("Invitee email address"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"email"},
		},
	}, s.handleOrgInviteRevoke)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.account_list",
		Description: "List user accounts in an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
			},
		},
	}, s.handleOrgAccountList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.account_disable",
		Description: "Disable an account, optionally removing it permanently. remove=true cannot be re-enabled. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"email":        stringProperty("Account email"),
				"remove":       boolProperty("Permanently remove the account from the org (irreversible)"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"email"},
		},
	}, s.handleOrgAccountDisable)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.account_enable",
		Description: "Re-enable a previously disabled account.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"email":        stringProperty("Account email"),
			},
			"required": []string{"email"},
		},
	}, s.handleOrgAccountEnable)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.org.account_set_role",
		Description: "Change a user's role within the organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"email":        stringProperty("Account email"),
				"role":         stringEnumProperty("Role", []string{"SU", "RW", "RO", "superuser", "readwrite", "readonly"}),
			},
			"required": []string{"email", "role"},
		},
	}, s.handleOrgAccountSetRole)
}

func (s *Server) handleOrgList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OrgList(apiCtx, service.OrgListOpts{
		SortBy:  input.SortBy,
		Name:    input.Name,
		Role:    input.Role,
		Status:  input.Status,
		Page:    input.Page,
		PerPage: input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.Orgs))
	for _, o := range result.Orgs {
		items = append(items, orgSummary(&o))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"organizations": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleOrgDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	org, err := s.svc.OrgGet(apiCtx, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": orgFull(org),
	}, "")
}

func (s *Server) handleOrgCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	org, err := s.svc.OrgCreate(apiCtx, input.Name, input.DisplayName, input.Description)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization":  orgFull(org),
		"action":        "created",
		"agent_token":   org.Token,
		"token_warning": "Single-issue agent token — surface to operator once and do not log.",
	}, fmt.Sprintf("Organization '%s' created", input.Name))
}

func (s *Server) handleOrgDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete organization (irreversible)", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgDelete(apiCtx, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "deleted",
	}, fmt.Sprintf("Organization '%s' deleted", input.Name))
}

func (s *Server) handleOrgQuota(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	q, err := s.svc.OrgQuota(apiCtx, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"quota": q,
	}, "")
}

func (s *Server) handleOrgSetDefaultOU(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgSetDefaultOUInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgSetDefaultOU(apiCtx, org, input.OU); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"default_ou":   input.OU,
	}, fmt.Sprintf("Default OU for '%s' set to '%s'", org, input.OU))
}

func (s *Server) handleOrgInviteSend(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgInviteSendInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	role := input.Role
	if role == "" {
		role = "RO"
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgInviteSend(apiCtx, org, input.Email, role); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"email":        input.Email,
		"role":         role,
		"action":       "invited",
	}, fmt.Sprintf("Invitation sent to %s", input.Email))
}

func (s *Server) handleOrgInviteList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OrgInviteList(apiCtx)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"received": result.Received,
		"sent":     result.Sent,
	}, "")
}

func (s *Server) handleOrgInviteAccept(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgInviteOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgInviteAccept(apiCtx, input.OrgName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": input.OrgName,
		"action":       "accepted",
	}, fmt.Sprintf("Invitation to %s accepted", input.OrgName))
}

func (s *Server) handleOrgInviteDecline(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgInviteOrgInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("decline invitation to", input.OrgName)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgInviteDecline(apiCtx, input.OrgName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": input.OrgName,
		"action":       "declined",
	}, fmt.Sprintf("Invitation to %s declined", input.OrgName))
}

func (s *Server) handleOrgInviteRevoke(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgInviteRevokeInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("revoke invitation for", input.Email)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgInviteRevoke(apiCtx, org, input.Email); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"email":        input.Email,
		"action":       "revoked",
	}, fmt.Sprintf("Invitation to %s revoked", input.Email))
}

func (s *Server) handleOrgAccountList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgAccountListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.OrgAccountList(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}
	accounts := make([]map[string]interface{}, 0, len(result.Accounts))
	for _, a := range result.Accounts {
		accounts = append(accounts, accountSummary(&a))
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"accounts":     accounts,
		"quota":        result.Quota,
	}, "")
}

func (s *Server) handleOrgAccountDisable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgAccountDisableInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		action := "disable"
		if input.Remove {
			action = "permanently remove"
		}
		return s.previewResult(fmt.Sprintf("%s account", action), fmt.Sprintf("%s in %s", input.Email, org))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgAccountDisable(apiCtx, org, input.Email, input.Remove); err != nil {
		return s.errorResult(err)
	}
	action := "disabled"
	if input.Remove {
		action = "removed"
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"email":        input.Email,
		"action":       action,
	}, fmt.Sprintf("Account %s %s", input.Email, action))
}

func (s *Server) handleOrgAccountEnable(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgAccountEnableInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgAccountEnable(apiCtx, org, input.Email); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"organization": org,
		"email":        input.Email,
		"action":       "enabled",
	}, fmt.Sprintf("Account %s enabled", input.Email))
}

func (s *Server) handleOrgAccountSetRole(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[orgAccountRoleInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.OrgAccountSetRole(apiCtx, org, input.Email, input.Role); err != nil {
		return s.errorResult(err)
	}
	canonical, _ := service.NormalizeRole(input.Role)
	return s.successResult(map[string]interface{}{
		"organization": org,
		"email":        input.Email,
		"role":         canonical,
	}, fmt.Sprintf("Role set to %s for %s", canonical, input.Email))
}

func orgSummary(o *models.Organization) map[string]interface{} {
	return map[string]interface{}{
		"name":         o.Name,
		"display_name": o.DisplayName,
		"description":  o.Description,
		"status":       o.Status,
		"role":         o.GetRole(),
		"default_ou":   o.GetDefaultOU(),
		"plan":         o.GetPlan(),
		"created_at":   o.CreatedAt,
	}
}

func orgFull(o *models.Organization) map[string]interface{} {
	full := orgSummary(o)
	full["device_count"] = o.DeviceCount
	full["member_count"] = o.MemberCount
	full["member_counts_by_role"] = o.MemberCountsByRole
	full["member_counts_by_status"] = o.MemberCountsByStatus
	full["owners"] = o.Owners
	full["token"] = o.Token
	return full
}

func accountSummary(a *models.Account) map[string]interface{} {
	return map[string]interface{}{
		"email":      a.Email,
		"name":       a.Name,
		"role":       a.Role,
		"status":     a.Status,
		"created_at": a.CreatedAt,
		"last_login": a.LastLogin,
	}
}
