package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

type snippetListInput struct {
	Organization  string `json:"organization,omitempty"`
	Type          string `json:"type,omitempty"`
	Name          string `json:"name,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	Page          int    `json:"page,omitempty"`
	PerPage       int    `json:"per_page,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
}

type snippetIDInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type snippetCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Content      string `json:"content"`
	Priority     int    `json:"priority,omitempty"`
}

type snippetUpdateContentInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Content      string `json:"content"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type snippetRenameInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	NewName      string `json:"new_name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type snippetSetPriorityInput struct {
	Organization string `json:"organization,omitempty"`
	Name         string `json:"name"`
	Priority     int    `json:"priority"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type snippetPullInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	Name         string `json:"name"`
	ConfigType   string `json:"config_type,omitempty"`
	AutoCreate   bool   `json:"auto_create,omitempty"`
	Overwrite    bool   `json:"overwrite,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

var snippetTypeEnum = []string{
	"USER", "GROUP", "ALIAS", "RULE",
	"UNBOUND_HOST_OVERRIDE", "UNBOUND_DOMAIN_FORWARD", "UNBOUND_HOST_ALIAS", "UNBOUND_ACL",
}

// registerSnippetTools registers every snippet tool.
func (s *Server) registerSnippetTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.list",
		Description: "List snippets in an organization with optional filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"type":           stringEnumProperty("Filter by snippet type", snippetTypeEnum),
				"name":           stringProperty("Filter by name (regex)"),
				"sort_by":        stringProperty("Sort field and direction (default priority:asc)"),
				"page":           intProperty("Page number", 1),
				"per_page":       intProperty("Items per page (max 100)", 50),
				"created_after":  stringProperty("Filter by created date after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"created_before": stringProperty("Filter by created date before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"updated_after":  stringProperty("Filter by updated date after (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
				"updated_before": stringProperty("Filter by updated date before (e.g., 30m, 2h, 7d, 2w or ISO 8601)"),
			},
		},
	}, s.handleSnippetList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.describe",
		Description: "Get a snippet's metadata and content.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Snippet name"),
			},
			"required": []string{"name"},
		},
	}, s.handleSnippetDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.create",
		Description: "Create a new snippet. Content must be supplied inline.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Snippet name"),
				"type":         stringEnumProperty("Snippet type", snippetTypeEnum),
				"content":      stringProperty("Snippet content (XML or whatever the type expects)"),
				"priority":     intProperty("Priority 1-60000 (default 1000)", 1000),
			},
			"required": []string{"name", "type", "content"},
		},
	}, s.handleSnippetCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.update_content",
		Description: "Replace a snippet's content. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Snippet name"),
				"content":      stringProperty("New snippet content"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name", "content"},
		},
	}, s.handleSnippetUpdateContent)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.rename",
		Description: "Rename a snippet. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Current snippet name"),
				"new_name":     stringProperty("New snippet name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name", "new_name"},
		},
	}, s.handleSnippetRename)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.set_priority",
		Description: "Update a snippet's priority (1-60000). Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Snippet name"),
				"priority":     intProperty("Priority 1-60000", 1000),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name", "priority"},
		},
	}, s.handleSnippetSetPriority)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.delete",
		Description: "Delete a snippet. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"name":         stringProperty("Snippet name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleSnippetDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.snippet.pull",
		Description: "Ask a device to pull a config object back to the platform as a snippet. Returns the task code; poll ndcli.task.describe to retrieve the result. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"device":       stringProperty("Device name"),
				"name":         stringProperty("Snippet name on the device (matching is type-specific)"),
				"config_type":  stringEnumProperty("Config type to pull (default ALIAS)", snippetTypeEnum),
				"auto_create":  boolProperty("Create snippet in DB if it doesn't exist"),
				"overwrite":    boolProperty("Update snippet in DB if it already exists"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"device", "name"},
		},
	}, s.handleSnippetPull)
}

func (s *Server) handleSnippetList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.SnippetList(apiCtx, org, service.SnippetListOpts{
		Type:          input.Type,
		Name:          input.Name,
		SortBy:        input.SortBy,
		Page:          input.Page,
		PerPage:       input.PerPage,
		CreatedAfter:  input.CreatedAfter,
		CreatedBefore: input.CreatedBefore,
		UpdatedAfter:  input.UpdatedAfter,
		UpdatedBefore: input.UpdatedBefore,
	})
	if err != nil {
		return s.errorResult(err)
	}

	items := make([]map[string]interface{}, 0, len(result.Snippets))
	for _, sn := range result.Snippets {
		items = append(items, snippetSummary(&sn))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"snippets": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleSnippetDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	snippet, err := s.svc.SnippetGet(apiCtx, org, input.Name)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"snippet": snippetFull(snippet),
	}, "")
}

func (s *Server) handleSnippetCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	snippet, err := s.svc.SnippetCreate(apiCtx, org, service.SnippetCreateOpts{
		Name:     input.Name,
		Type:     input.Type,
		Content:  input.Content,
		Priority: input.Priority,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"snippet": snippetSummary(snippet),
		"action":  "created",
	}, fmt.Sprintf("Snippet '%s' created", input.Name))
}

func (s *Server) handleSnippetUpdateContent(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetUpdateContentInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update content of snippet", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SnippetUpdateContent(apiCtx, org, input.Name, input.Content); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "updated",
	}, fmt.Sprintf("Snippet '%s' content updated", input.Name))
}

func (s *Server) handleSnippetRename(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetRenameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("rename snippet", fmt.Sprintf("%s → %s", input.Name, input.NewName))
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SnippetRename(apiCtx, org, input.Name, input.NewName); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":     input.Name,
		"new_name": input.NewName,
		"action":   "renamed",
	}, fmt.Sprintf("Snippet renamed: %s → %s", input.Name, input.NewName))
}

func (s *Server) handleSnippetSetPriority(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetSetPriorityInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("set snippet priority to %d", input.Priority), input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SnippetSetPriority(apiCtx, org, input.Name, input.Priority); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":     input.Name,
		"priority": input.Priority,
	}, fmt.Sprintf("Priority set to %d for '%s'", input.Priority, input.Name))
}

func (s *Server) handleSnippetDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetIDInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete snippet", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SnippetDelete(apiCtx, org, input.Name); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"name":   input.Name,
		"action": "deleted",
	}, fmt.Sprintf("Snippet '%s' deleted", input.Name))
}

func (s *Server) handleSnippetPull(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[snippetPullInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult(fmt.Sprintf("pull %s '%s' from", input.ConfigType, input.Name), input.Device)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	pull, err := s.svc.SnippetPull(apiCtx, org, input.Device, service.SnippetPullOpts{
		Name:       input.Name,
		ConfigType: input.ConfigType,
		AutoCreate: input.AutoCreate,
		Overwrite:  input.Overwrite,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"task":        pull.Task,
		"name":        pull.Name,
		"config_type": pull.ConfigType,
		"status":      pull.Status,
		"hint":        "Poll ndcli.task.describe with this task code to retrieve the result.",
	}, fmt.Sprintf("Pull task %s created for %s", pull.Task, input.Name))
}

func snippetSummary(s *models.Snippet) map[string]interface{} {
	return map[string]interface{}{
		"name":         s.Name,
		"type":         s.Type,
		"priority":     s.Priority,
		"organization": s.Organization,
		"created_at":   s.CreatedAt,
		"updated_at":   s.UpdatedAt,
	}
}

func snippetFull(s *models.Snippet) map[string]interface{} {
	full := snippetSummary(s)
	full["content"] = s.Content
	return full
}
