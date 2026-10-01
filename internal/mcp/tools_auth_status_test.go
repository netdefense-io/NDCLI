package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// sessionAuthStub satisfies the service's auth manager interface with a preset
// classification. service.New only takes the concrete *auth.Manager, so the
// stub goes in through NewFromProvider.
type sessionAuthStub struct {
	status      *models.SessionStatus
	accessErr   error
	accessCalls int
}

func (s *sessionAuthStub) IsAuthenticated() bool                { return s.status.State == models.SessionValid }
func (s *sessionAuthStub) SessionStatus() *models.SessionStatus { return s.status }
func (s *sessionAuthStub) GetAccessToken() (string, error) {
	s.accessCalls++
	return "tok", s.accessErr
}
func (s *sessionAuthStub) ForceRefresh() error { return nil }

// GetUserInfo mirrors StaticTokenProvider: a personal access token has no cached
// identity, so it errors, and the resource omits its user block.
func (s *sessionAuthStub) GetUserInfo() (*models.UserInfo, error) {
	if s.status.State == models.SessionStatic {
		return nil, errors.New("no cached user info for static-token auth")
	}
	return &models.UserInfo{Email: "alice@example.com", Name: "Alice", Subject: "sub-1"}, nil
}
func (s *sessionAuthStub) GetTokenSummary() map[string]interface{} {
	return map[string]interface{}{
		"email":       "alice@example.com",
		"subject":     "sub-1",
		"is_expired":  s.status.State != models.SessionValid,
		"has_refresh": s.status.HasRefresh,
	}
}
func (s *sessionAuthStub) GetStorageName() string { return "stub-storage" }
func (s *sessionAuthStub) Logout() error          { return nil }

func newSessionServer(stub *sessionAuthStub) *Server {
	client := api.NewClient("https://example.invalid", false, &fakeAuthProvider{})
	cfg := &config.Config{}
	return &Server{svc: service.NewFromProvider(client, stub, cfg), config: cfg}
}

type authStatusCase struct {
	name        string
	status      *models.SessionStatus
	accessErr   error
	wantAuth    bool
	wantStatus  string
	wantRenewal bool // RequireAuth (and so a refresh) is attempted
	wantUser    bool // the resource carries a user block
}

// authStatusCases are the states the tool and the resource must agree on.
// "authenticated" means the next tool call will work, which is why the
// refreshable rows are decided by whether the refresh itself succeeds.
var authStatusCases = []authStatusCase{
	{"valid", &models.SessionStatus{State: models.SessionValid}, nil, true, "authenticated", true, true},
	{"refreshable and the refresh works", &models.SessionStatus{State: models.SessionRefreshable, HasRefresh: true}, nil, true, "authenticated", true, true},
	{"refreshable but the refresh fails", &models.SessionStatus{State: models.SessionRefreshable, HasRefresh: true}, errors.New("refresh token revoked"), false, "auth_failed", true, false},
	{"expired without a refresh token", &models.SessionStatus{State: models.SessionExpired}, errors.New("no refresh token"), false, "session_expired", false, false},
	{"nothing stored", &models.SessionStatus{State: models.SessionNone}, errors.New("no tokens"), false, "not_authenticated", false, false},
	{"static token", &models.SessionStatus{State: models.SessionStatic}, nil, true, "authenticated", true, false},
}

func callAuthStatusTool(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	result, err := s.handleAuthStatus(context.Background(), &mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleAuthStatus: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("ndcli.auth.status reported failure: %+v", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is %T, want an object", resp.Data)
	}
	return data
}

func readAuthStatusResource(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	result, err := s.handleAuthResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "ndcli://auth/status"},
	})
	if err != nil {
		t.Fatalf("handleAuthResource: %v", err)
	}
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &content); err != nil {
		t.Fatalf("decode resource: %v", err)
	}
	return content
}

func TestAuthStatusTool(t *testing.T) {
	for _, tt := range authStatusCases {
		t.Run(tt.name, func(t *testing.T) {
			stub := &sessionAuthStub{status: tt.status, accessErr: tt.accessErr}
			data := callAuthStatusTool(t, newSessionServer(stub))

			if got, _ := data["authenticated"].(bool); got != tt.wantAuth {
				t.Errorf("authenticated = %v, want %v", data["authenticated"], tt.wantAuth)
			}
			if got, _ := data["status"].(string); got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}
			if got, _ := data["storage"].(string); got != "stub-storage" {
				t.Errorf("storage = %q, want stub-storage", got)
			}
			if attempted := stub.accessCalls > 0; attempted != tt.wantRenewal {
				t.Errorf("token requested = %v, want %v: only a session that can authenticate is proven with RequireAuth", attempted, tt.wantRenewal)
			}
			info, _ := data["token_info"].(map[string]interface{})
			if _, leaked := info["subject"]; leaked {
				t.Error("token_info carries the subject claim")
			}
		})
	}
}

func TestAuthStatusResource(t *testing.T) {
	for _, tt := range authStatusCases {
		t.Run(tt.name, func(t *testing.T) {
			stub := &sessionAuthStub{status: tt.status, accessErr: tt.accessErr}
			content := readAuthStatusResource(t, newSessionServer(stub))

			if got, _ := content["authenticated"].(bool); got != tt.wantAuth {
				t.Errorf("authenticated = %v, want %v", content["authenticated"], tt.wantAuth)
			}
			if got, _ := content["status"].(string); got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}
			_, hasUser := content["user"]
			if hasUser != tt.wantUser {
				t.Errorf("user block present = %v, want %v", hasUser, tt.wantUser)
			}
		})
	}
}

// One classifier feeds both: a caller reading the tool and the resource must not
// be told two different things about the same session.
func TestAuthStatusToolAndResourceAgree(t *testing.T) {
	for _, tt := range authStatusCases {
		t.Run(tt.name, func(t *testing.T) {
			s := newSessionServer(&sessionAuthStub{status: tt.status, accessErr: tt.accessErr})
			tool := callAuthStatusTool(t, s)
			resource := readAuthStatusResource(t, s)
			for _, key := range []string{"authenticated", "status", "storage"} {
				if tool[key] != resource[key] {
					t.Errorf("%s: tool %v, resource %v", key, tool[key], resource[key])
				}
			}
		})
	}
}

// The status strings are the contract an agent reads, so the tool description
// has to list every one the handler can emit.
func TestAuthStatusToolDescriptionListsEveryStatus(t *testing.T) {
	description := registeredToolDescription(t, "ndcli.auth.status")
	for _, tt := range authStatusCases {
		if !strings.Contains(description, tt.wantStatus) {
			t.Errorf("description does not mention status %q:\n%s", tt.wantStatus, description)
		}
	}
	if strings.Contains(strings.ToLower(description), "auth0") {
		t.Errorf("description names the identity provider:\n%s", description)
	}
}

func registeredToolDescription(t *testing.T, toolName string) string {
	t.Helper()

	cs := connectTestClient(t)
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if tool.Name == toolName {
			return tool.Description
		}
	}
	t.Fatalf("tool %q not registered", toolName)
	return ""
}
