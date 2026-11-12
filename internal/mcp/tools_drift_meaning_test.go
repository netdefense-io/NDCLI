package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Every tool that filters by drift status says what drift means, as the CLI
// help does.
func TestDriftStatusParametersSayWhatDriftMeans(t *testing.T) {
	session := connectTestClient(t)
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range result.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(raw, &schema)
		if p, ok := schema.Properties["drift_status"]; ok {
			seen++
			if !strings.Contains(p.Description, models.DriftStatusMeaning) {
				t.Errorf("%s drift_status does not say what drift means: %q", tool.Name, p.Description)
			}
		}
	}
	if seen != 3 {
		t.Errorf("found %d tools with drift_status, want 3", seen)
	}
}
