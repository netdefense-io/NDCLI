package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// NOTE: ndcli auth login / logout / migrate stay CLI-only (interactive
// browser flow + local storage mutation). ndcli auth delete is intentionally
// not exposed via MCP — account deletion behind an LLM-driven flow needs
// stronger out-of-band confirmation than a tool call. Local config writes
// (ndcli config set / reset) are also CLI-only.

// registerAuthTools registers the auth/config tools.
func (s *Server) registerAuthTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.status",
		Description: "Check the current authentication status (storage backend, token info — value never returned).",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleAuthStatus)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.me",
		Description: "Fetch the authenticated user's profile and organization memberships from /api/v1/auth/me.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleAuthMe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.refresh",
		Description: "Force a refresh of the access token. Useful when the cached token has expired but the refresh token is still valid.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleAuthRefresh)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.config.show",
		Description: "Show the current NDCLI configuration (sensitive data redacted).",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleConfigShow)
}

func (s *Server) handleAuthStatus(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	authenticated := s.svc.AuthIsAuthenticated()
	status := "not_authenticated"
	if authenticated {
		// A successful RequireAuth means the token is valid (or was just
		// refreshed). Surface the distinction in the response.
		if err := s.svc.RequireAuth(); err != nil {
			status = "token_expired"
		} else {
			status = "authenticated"
		}
	}
	return s.successResult(map[string]interface{}{
		"authenticated": authenticated,
		"status":        status,
		"storage":       s.svc.AuthStorageName(),
		"token_info":    s.svc.AuthTokenSummary(),
	}, "")
}

func (s *Server) handleAuthMe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	me, err := s.svc.AuthMe(apiCtx)
	if err != nil {
		return s.errorResult(err)
	}
	orgs := make([]map[string]interface{}, 0, len(me.Organizations))
	for _, o := range me.Organizations {
		orgs = append(orgs, map[string]interface{}{
			"name":   o.Name,
			"role":   o.Role,
			"status": o.Status,
		})
	}
	return s.successResult(map[string]interface{}{
		"user": map[string]interface{}{
			"email":         me.Email,
			"name":          me.GetName(),
			"status":        me.Status,
			"created_at":    me.CreatedAt,
			"organizations": orgs,
		},
	}, "")
}

func (s *Server) handleAuthRefresh(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.AuthRefresh(); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"refreshed":  true,
		"token_info": s.svc.AuthTokenSummary(),
	}, "Access token refreshed")
}

func (s *Server) handleConfigShow(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg := config.Get()
	return s.successResult(map[string]interface{}{
		"config": map[string]interface{}{
			"controlplane": map[string]interface{}{
				"host":       cfg.Controlplane.Host,
				"ssl_verify": cfg.Controlplane.SSLVerify,
			},
			"organization": map[string]interface{}{
				"name": cfg.Organization.Name,
			},
			"output": map[string]interface{}{
				"format":   cfg.Output.Format,
				"timezone": cfg.Output.Timezone,
			},
			"oauth2": map[string]interface{}{
				"provider":  cfg.OAuth2.Provider,
				"domain":    cfg.OAuth2.Domain,
				"client_id": redact(cfg.OAuth2.ClientID),
			},
		},
	}, "")
}

// redact returns a redacted version of a string (first 4 + last 4 chars).
func redact(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
