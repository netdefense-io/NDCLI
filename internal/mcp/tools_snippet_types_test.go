package mcp

import (
	"context"
	"reflect"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// registeredToolSchema connects an in-memory client/server pair (via the
// shared connectTestClient helper in testutil_test.go — also used by
// tool_registry_test.go's registeredToolNames) and returns one tool's input
// schema the way a real client decodes it: per the SDK's
// mcp.Tool.InputSchema doc comment, the client side holds "the default JSON
// marshaling of the server's input schema (a map[string]any)".
func registeredToolSchema(t *testing.T, toolName string) map[string]any {
	t.Helper()

	cs := connectTestClient(t)
	ctx := context.Background()

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if tool.Name == toolName {
			schema, ok := tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("%s: InputSchema is %T, want map[string]any", toolName, tool.InputSchema)
			}
			return schema
		}
	}
	t.Fatalf("tool %q not registered", toolName)
	return nil
}

// schemaPropertyEnum drills into schema.properties[property].enum and
// returns it as a []string.
func schemaPropertyEnum(t *testing.T, schema map[string]any, property string) []string {
	t.Helper()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties object: %#v", schema)
	}
	prop, ok := props[property].(map[string]any)
	if !ok {
		t.Fatalf("schema has no %q property: %#v", property, props)
	}
	rawEnum, ok := prop["enum"].([]any)
	if !ok {
		t.Fatalf("%q property has no enum: %#v", property, prop)
	}
	out := make([]string, len(rawEnum))
	for i, v := range rawEnum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%q enum value %v is not a string", property, v)
		}
		out[i] = s
	}
	return out
}

// TestSnippetListAndCreateEnumsUseCreatableList guards the ndcli.snippet.list
// and ndcli.snippet.create MCP tool "type" enums against drifting from
// models.SnippetCreatableTypes.
func TestSnippetListAndCreateEnumsUseCreatableList(t *testing.T) {
	for _, tool := range []string{"ndcli.snippet.list", "ndcli.snippet.create"} {
		schema := registeredToolSchema(t, tool)
		got := schemaPropertyEnum(t, schema, "type")
		if !reflect.DeepEqual(got, models.SnippetCreatableTypes) {
			t.Errorf("%s.type enum = %v, want models.SnippetCreatableTypes %v", tool, got, models.SnippetCreatableTypes)
		}
	}
}

// TestSnippetPullEnumUsesPullableList guards the ndcli.snippet.pull
// "config_type" enum against drifting from models.SnippetPullableTypes.
func TestSnippetPullEnumUsesPullableList(t *testing.T) {
	schema := registeredToolSchema(t, "ndcli.snippet.pull")
	got := schemaPropertyEnum(t, schema, "config_type")
	if !reflect.DeepEqual(got, models.SnippetPullableTypes) {
		t.Errorf("ndcli.snippet.pull.config_type enum = %v, want models.SnippetPullableTypes %v", got, models.SnippetPullableTypes)
	}
}
