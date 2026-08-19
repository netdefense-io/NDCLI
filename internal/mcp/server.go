package mcp

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Server is the MCP server for NDCLI
type Server struct {
	mcpServer       *mcp.Server
	authManager     *auth.Manager
	apiClient       *api.Client
	svc             *service.Service
	config          *config.Config
	logger          *log.Logger
	consoleSessions *ConsoleSessionManager
}

// NewServer creates a new MCP server
func NewServer() (*Server, error) {
	// Set up logging to stderr (stdout is used for MCP protocol)
	logger := log.New(os.Stderr, "[netdefense-mcp] ", log.LstdFlags)

	// Load configuration
	if err := config.Load(""); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Static PAT via NDCLI_TOKEN — skips OAuth2/keyring entirely, mirroring
	// the branch in cli/root.go and internal/tui/run.go. A malformed token
	// fails startup loudly rather than silently falling back to the keyring
	// (which would surface a confusing "please log in" instead of "your
	// token is bad").
	staticProvider, err := auth.StaticProviderFromEnv()
	if err != nil {
		return nil, err
	}

	var authMgr *auth.Manager
	var apiClient *api.Client
	var svc *service.Service
	cfg := config.Get()

	if staticProvider != nil {
		// authMgr stays nil — there is no keyring/OAuth2 session in static-PAT
		// mode, matching the CLI/TUI convention. The service layer is wired
		// directly to the provider so RequireAuth() (used by every tool
		// handler) still works correctly instead of always reporting
		// "not authenticated".
		apiClient = api.NewClientFromConfig(staticProvider)
		svc = service.NewFromProvider(apiClient, staticProvider, cfg)
	} else {
		authMgr = auth.GetManager()
		apiClient = api.NewClientFromConfig(authMgr)
		svc = service.New(apiClient, authMgr, cfg)
	}

	// Create MCP server with implementation info
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "ndcli",
			Version: config.Version,
		},
		nil, // ServerOptions
	)

	s := &Server{
		mcpServer:       mcpServer,
		authManager:     authMgr,
		apiClient:       apiClient,
		svc:             svc,
		config:          cfg,
		logger:          logger,
		consoleSessions: newConsoleSessionManager(),
	}

	// Register all tools
	s.registerDeviceTools()
	s.registerOrgTools()
	s.registerOUTools()
	s.registerSyncTools()
	s.registerTaskTools()
	s.registerRunTools()
	s.registerAuthTools()
	s.registerTokenTools()
	s.registerSnippetTools()
	s.registerSoftwarePolicyTools()
	s.registerTemplateTools()
	s.registerNetworkTools()
	s.registerVariableTools()
	s.registerBackupTools()
	s.registerDeviceHealthTool()
	s.registerScheduleTools()
	s.registerConsoleTools()

	// Register all resources
	s.registerResources()

	return s, nil
}

// Serve starts the MCP server on stdio transport.
// It closes all console sessions on exit to prevent relay leaks.
func (s *Server) Serve() error {
	s.logger.Println("Starting MCP server...")
	defer s.consoleSessions.CloseAll()
	return s.mcpServer.Run(context.Background(), &mcp.StdioTransport{})
}

// checkAuth verifies the user is authenticated, refreshing an expired access
// token transparently when a refresh token is still available. It delegates
// to the shared service-layer gate (svc.RequireAuth) rather than touching
// s.authManager directly, so it behaves correctly in both auth modes:
// s.authManager is nil under static-PAT (NDCLI_TOKEN) auth — the same
// s.authManager.GetAccessToken() call this used to make would nil-pointer
// panic in that mode, and its old "Please run 'ndcli auth login'" message
// would have been wrong advice for a PAT-configured headless server anyway.
// svc.RequireAuth() already knows about both auth implementations (see
// internal/service/service.go and internal/auth/static.go).
//
// Under static-PAT (NDCLI_TOKEN) auth this check is presence, not validity:
// StaticTokenProvider.GetAccessToken() never errors, so a set-but-since-
// revoked/expired PAT still passes checkAuth() and only surfaces as an
// actual 401 mid-request, which triggers ForceRefresh() and returns
// ErrStaticTokenRejected (internal/auth/static.go). Same as the pre-existing
// CLI behavior — not a regression, just worth knowing when reading this gate.
func (s *Server) checkAuth() error {
	if err := s.svc.RequireAuth(); err != nil {
		return &ToolError{
			Code:    "AUTH_FAILED",
			Message: err.Error(),
		}
	}
	return nil
}

// requireInteractiveAuth rejects the call when the server is running under
// static-PAT (NDCLI_TOKEN) auth — s.authManager is nil in that mode (see
// NewServer). Mirrors cli/root.go's isTokenMutationCommand gate: token
// create/revoke must not be reachable with nothing but a bearer PAT,
// because svc.RequireAuth() alone can't tell "real interactive OAuth2
// session" apart from "static token present" — StaticTokenProvider always
// satisfies RequireAuth(). Used by the handlers that mint or destroy
// credentials (ndcli.auth.token_create / ndcli.auth.token_revoke); read-only
// token operations (ndcli.auth.token_list) intentionally do not call this,
// matching the CLI's own policy.
func (s *Server) requireInteractiveAuth() error {
	if s.authManager == nil {
		return &ToolError{
			Code:    "INTERACTIVE_AUTH_REQUIRED",
			Message: "token create/revoke requires interactive OAuth2 authentication and cannot run under NDCLI_TOKEN static-PAT auth — unset NDCLI_TOKEN on the host running netdefense-mcp, run 'ndcli auth login', and restart the server (or use 'ndcli auth token create/revoke' directly).",
		}
	}
	return nil
}

// getOrganization returns the organization from input or config
func (s *Server) getOrganization(inputOrg string) (string, error) {
	if inputOrg != "" {
		return inputOrg, nil
	}
	if s.config.Organization.Name != "" {
		return s.config.Organization.Name, nil
	}
	return "", &ToolError{
		Code:    "ORG_REQUIRED",
		Message: "Organization is required. Provide 'organization' parameter or set default via 'ndcli config set organization.name <org>'.",
	}
}

// ToolError represents a tool execution error
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string {
	return e.Message
}

// ToolResponse is the standard response format for tools
type ToolResponse struct {
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Message    string      `json:"message,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
	Error      *ErrorInfo  `json:"error,omitempty"`
}

// Pagination info for list responses
type Pagination struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// ErrorInfo for error responses
type ErrorInfo struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// successResult creates a successful tool result
func (s *Server) successResult(data interface{}, message string) (*mcp.CallToolResult, error) {
	response := ToolResponse{
		Success: true,
		Data:    data,
		Message: message,
	}
	return s.jsonResult(response, false)
}

// successResultWithPagination creates a successful tool result with pagination
func (s *Server) successResultWithPagination(data interface{}, page, perPage, total int) (*mcp.CallToolResult, error) {
	response := ToolResponse{
		Success: true,
		Data:    data,
		Pagination: &Pagination{
			Page:    page,
			PerPage: perPage,
			Total:   total,
		},
	}
	return s.jsonResult(response, false)
}

// errorResult creates an error tool result. Recognises both the legacy
// *ToolError (from pre-service handlers) and the unified *service.Error so
// the wire-level error code stays stable across the migration.
func (s *Server) errorResult(err error) (*mcp.CallToolResult, error) {
	response := ToolResponse{
		Success: false,
		Error: &ErrorInfo{
			Message: err.Error(),
		},
	}

	switch e := err.(type) {
	case *ToolError:
		response.Error.Code = e.Code
	case *service.Error:
		response.Error.Code = e.Code
	}

	return s.jsonResult(response, true)
}

// previewResult creates a preview result for destructive operations without confirm
func (s *Server) previewResult(action, target string) (*mcp.CallToolResult, error) {
	response := ToolResponse{
		Success: true,
		Data: map[string]interface{}{
			"preview": true,
			"action":  action,
			"target":  target,
		},
		Message: fmt.Sprintf("Preview: Would %s '%s'. Set confirm=true to execute.", action, target),
	}
	return s.jsonResult(response, false)
}

// jsonResult creates a JSON-formatted tool result
func (s *Server) jsonResult(response interface{}, isError bool) (*mcp.CallToolResult, error) {
	content, err := marshalJSON(response)
	if err != nil {
		content = fmt.Sprintf(`{"success":false,"error":{"message":"Failed to marshal response: %s"}}`, err.Error())
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: content,
			},
		},
		IsError: isError,
	}, nil
}

// contextWithTimeout creates a context with a reasonable timeout
func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), apiTimeout)
}
