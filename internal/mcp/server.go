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
)

// Server is the MCP server for NDCLI
type Server struct {
	mcpServer   *mcp.Server
	authManager *auth.Manager
	apiClient   *api.Client
	config      *config.Config
	logger      *log.Logger
}

// NewServer creates a new MCP server
func NewServer() (*Server, error) {
	// Set up logging to stderr (stdout is used for MCP protocol)
	logger := log.New(os.Stderr, "[netdefense-mcp] ", log.LstdFlags)

	// Load configuration
	if err := config.Load(""); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize auth manager
	authMgr := auth.GetManager()

	// Initialize API client
	apiClient := api.NewClientFromConfig(authMgr)

	// Create MCP server with implementation info
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "ndcli",
			Version: config.Version,
		},
		nil, // ServerOptions
	)

	s := &Server{
		mcpServer:   mcpServer,
		authManager: authMgr,
		apiClient:   apiClient,
		config:      config.Get(),
		logger:      logger,
	}

	// Register all tools
	s.registerDeviceTools()
	s.registerOrgTools()
	s.registerOUTools()
	s.registerSyncTools()
	s.registerTaskTools()
	s.registerAuthTools()
	s.registerSnippetTools()
	s.registerTemplateTools()

	// Register all resources
	s.registerResources()

	return s, nil
}

// Serve starts the MCP server on stdio transport
func (s *Server) Serve() error {
	s.logger.Println("Starting MCP server...")
	return s.mcpServer.Run(context.Background(), &mcp.StdioTransport{})
}

// checkAuth verifies the user is authenticated
func (s *Server) checkAuth() error {
	if !s.authManager.IsAuthenticated() {
		return &ToolError{
			Code:    "NOT_AUTHENTICATED",
			Message: "Not authenticated. Please run 'ndcli auth login' first.",
		}
	}

	// Verify token is valid
	_, err := s.authManager.GetAccessToken()
	if err != nil {
		return &ToolError{
			Code:    "AUTH_FAILED",
			Message: fmt.Sprintf("Authentication failed: %v. Please run 'ndcli auth login' to re-authenticate.", err),
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

// errorResult creates an error tool result
func (s *Server) errorResult(err error) (*mcp.CallToolResult, error) {
	response := ToolResponse{
		Success: false,
		Error: &ErrorInfo{
			Message: err.Error(),
		},
	}

	// Add error code if available
	if toolErr, ok := err.(*ToolError); ok {
		response.Error.Code = toolErr.Code
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
