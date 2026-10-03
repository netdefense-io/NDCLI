package mcp

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSnippetWriteToolsStateUUIDGuidance: a model asked to clone a snippet
// reads it with ndcli.snippet.describe and writes the content back, uuid and
// all. The write tools tell it to drop the uuid before NDManager refuses the
// copy with SNIPPET_UUID_IN_USE.
func TestSnippetWriteToolsStateUUIDGuidance(t *testing.T) {
	for _, tool := range []string{"ndcli.snippet.create", "ndcli.snippet.update_content"} {
		if description := registeredToolDescription(t, tool); !strings.Contains(description, models.SnippetUUIDGuidance) {
			t.Errorf("%s description does not carry the uuid guidance:\n%s", tool, description)
		}
	}
}
