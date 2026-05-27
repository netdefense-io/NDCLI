package mcp

// NOTE on parity:
//   ndcli auth token list   → ndcli.auth.token_list   (MCP-parity: included)
//   ndcli auth token create → ndcli.auth.token_create (MCP-parity: included;
//     the MCP transport can display the raw token in the tool result — the
//     operator invoking the agent is responsible for capturing it)
//   ndcli auth token revoke → ndcli.auth.token_revoke (MCP-parity: included)

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TokenCreateInput is the input for ndcli.auth.token_create
type TokenCreateInput struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Org       string `json:"org,omitempty"`
	ExpiresIn string `json:"expires_in,omitempty"`
}

// TokenRevokeInput is the input for ndcli.auth.token_revoke
type TokenRevokeInput struct {
	Name    string `json:"name"`
	Confirm bool   `json:"confirm,omitempty"`
}

// registerTokenTools registers personal-access-token tools.
func (s *Server) registerTokenTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.token_list",
		Description: "List personal access tokens for the authenticated user.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleTokenList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.token_create",
		Description: "Create a new personal access token. The raw token value is returned once — capture it immediately.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":       stringProperty("Token name (unique identifier)"),
				"scope":      stringEnumProperty("Token scope", []string{"RW", "RO"}),
				"org":        stringProperty("Restrict token to a specific organization (optional)"),
				"expires_in": stringEnumProperty("Token lifetime", []string{"30d", "60d", "90d", "180d", "365d", "never"}),
			},
			"required": []string{"name", "scope"},
		},
	}, s.handleTokenCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.token_revoke",
		Description: "Revoke a personal access token by name. Set confirm=true to execute; without it returns a preview.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":    stringProperty("Token name to revoke"),
				"confirm": confirmProperty(),
			},
			"required": []string{"name"},
		},
	}, s.handleTokenRevoke)
}

func (s *Server) handleTokenList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	tokens, err := s.svc.TokenList(apiCtx)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResultWithPagination(tokens, 1, len(tokens), len(tokens))
}

func (s *Server) handleTokenCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	input, err := parseInput[TokenCreateInput](req.Params.Arguments)
	if err != nil {
		return s.errorResult(err)
	}

	expiresIn := input.ExpiresIn
	if expiresIn == "" {
		expiresIn = "90d"
	}

	opts := service.TokenCreateOpts{
		Name:      input.Name,
		Scope:     input.Scope,
		Org:       input.Org,
		ExpiresIn: expiresIn,
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.TokenCreate(apiCtx, opts)
	if err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"token":   result.Token.Token,
		"name":    result.Token.Name,
		"scope":   result.Token.Scope,
		"warning": "Copy the token value now — it will NOT be returned again.",
	}, "Personal access token created")
}

func (s *Server) handleTokenRevoke(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}

	input, err := parseInput[TokenRevokeInput](req.Params.Arguments)
	if err != nil {
		return s.errorResult(err)
	}

	if !input.Confirm {
		return s.previewResult("revoke token", input.Name)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TokenRevoke(apiCtx, input.Name); err != nil {
		return s.errorResult(err)
	}

	return s.successResult(map[string]interface{}{
		"revoked": true,
		"name":    input.Name,
	}, "Token revoked")
}
