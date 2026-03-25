package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// registerAuthTools registers all auth-related tools
func (s *Server) registerAuthTools() {
	// ndcli.auth.status
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.status",
		Description: "Check the current authentication status",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleAuthStatus)

	// ndcli.auth.me
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.auth.me",
		Description: "Get information about the currently authenticated user",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleAuthMe)

	// ndcli.config.show
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.config.show",
		Description: "Show the current NDCLI configuration (sensitive data redacted)",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, s.handleConfigShow)
}

func (s *Server) handleAuthStatus(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	isAuthenticated := s.authManager.IsAuthenticated()

	status := "not_authenticated"
	if isAuthenticated {
		// Try to verify token is valid
		_, err := s.authManager.GetAccessToken()
		if err != nil {
			status = "token_expired"
		} else {
			status = "authenticated"
		}
	}

	tokenSummary := s.authManager.GetTokenSummary()

	return s.successResult(map[string]interface{}{
		"authenticated": isAuthenticated,
		"status":        status,
		"storage":       s.authManager.GetStorageName(),
		"token_info":    tokenSummary,
	}, "")
}

func (s *Server) handleAuthMe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check auth
	if err := s.checkAuth(); err != nil {
		return s.errorResult(err)
	}

	// Get user info
	userInfo, err := s.authManager.GetUserInfo()
	if err != nil {
		return s.errorResult(&ToolError{Code: "AUTH_ERROR", Message: "Failed to get user info: " + err.Error()})
	}

	if userInfo == nil {
		return s.errorResult(&ToolError{Code: "AUTH_ERROR", Message: "No user info available"})
	}

	return s.successResult(map[string]interface{}{
		"user": map[string]interface{}{
			"email":          userInfo.Email,
			"name":           userInfo.Name,
			"nickname":       userInfo.Nickname,
			"picture":        userInfo.Picture,
			"email_verified": userInfo.EmailVerified,
		},
	}, "")
}

func (s *Server) handleConfigShow(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg := config.Get()

	// Return config with sensitive data redacted
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
				"format": cfg.Output.Format,
			},
			"oauth2": map[string]interface{}{
				"provider":  cfg.OAuth2.Provider,
				"domain":    cfg.OAuth2.Domain,
				"client_id": redact(cfg.OAuth2.ClientID),
			},
		},
	}, "")
}

// redact returns a redacted version of a string
func redact(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
