package cli

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSnippetWriteHelpStatesUUIDGuidance: content copied from another snippet
// carries that snippet's managed uuid, and NDManager refuses the copy with
// SNIPPET_UUID_IN_USE. Both commands that write content say so up front.
func TestSnippetWriteHelpStatesUUIDGuidance(t *testing.T) {
	for _, want := range []string{"ALIAS", "RULE", "UNBOUND_*", "leave uuid out", "drop its uuid", "SNIPPET_UUID_IN_USE"} {
		if !strings.Contains(models.SnippetUUIDGuidance, want) {
			t.Errorf("guidance does not mention %q: %s", want, models.SnippetUUIDGuidance)
		}
	}
	for name, help := range map[string]string{
		"snippet create":         snippetCreateCmd.Long,
		"snippet update-content": snippetUpdateContentCmd.Long,
	} {
		if !strings.Contains(help, models.SnippetUUIDGuidance) {
			t.Errorf("%s help does not carry the uuid guidance:\n%s", name, help)
		}
	}
}
