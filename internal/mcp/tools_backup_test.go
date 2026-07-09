package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestRegisterBackupTools_NoConfigCreate verifies that org backup bootstrap
// is no longer reachable via MCP: ndcli.backup.config_create must not be
// registered, while config_update (the non-secret variant) still is. Fails
// against pre-fix code, which registers config_create with required
// s3_access_key/encryption_key fields.
func TestRegisterBackupTools_NoConfigCreate(t *testing.T) {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	s := &Server{mcpServer: mcpServer, svc: service.New(nil, nil, nil)}
	s.registerBackupTools()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, _ = mcpServer.Connect(ctx, serverTransport, nil)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	if names["ndcli.backup.config_create"] {
		t.Error("ndcli.backup.config_create is registered — org backup bootstrap must be CLI-only")
	}
	if !names["ndcli.backup.config_update"] {
		t.Error("ndcli.backup.config_update should still be registered")
	}
}

// TestBackupConfigUpdateInput_NoSecretFields verifies that
// backupConfigUpdateInput has no way to carry s3_access_key/encryption_key
// through the MCP tool surface: parsing a raw payload containing them, then
// re-marshaling the struct, must never reproduce those keys. Fails against
// pre-fix code, where both fields exist on the struct and flow straight
// through to the API payload.
func TestBackupConfigUpdateInput_NoSecretFields(t *testing.T) {
	raw := json.RawMessage(`{"s3_access_key":"AKIA_SHOULD_NOT_SURVIVE","encryption_key":"sekret","s3_endpoint":"https://s3.example.com"}`)
	input, err := parseInput[backupConfigUpdateInput](raw)
	if err != nil {
		t.Fatalf("parseInput error: %v", err)
	}
	if input.S3Endpoint != "https://s3.example.com" {
		t.Errorf("expected s3_endpoint to survive parsing, got %q", input.S3Endpoint)
	}

	reencoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(reencoded, &asMap); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	for _, secretField := range []string{"s3_access_key", "encryption_key"} {
		if _, present := asMap[secretField]; present {
			t.Errorf("backupConfigUpdateInput exposes %q — must not carry secret material", secretField)
		}
	}
}
