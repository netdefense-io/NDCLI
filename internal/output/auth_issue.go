package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// formatAuthIssueLine renders one AUTH_SERVER/AUTH_ORDER build-time issue
// (an auth_issues entry from NDManager) as a single line: the
// machine-readable code, the message, and whichever names — snippet,
// template, server, group, facility — the issue names. Shared by all
// three human formatters (table.go, simple.go, detailed.go) since they
// live in the same package; the JSON formatter needs no equivalent, it
// marshals models.SyncError (and therefore AuthIssues) directly.
func formatAuthIssueLine(ai models.AuthIssue) string {
	line := fmt.Sprintf("[%s] %s", ai.Code, ai.Message)

	var refs []string
	if len(ai.Snippet) > 0 {
		refs = append(refs, "snippet: "+strings.Join(ai.Snippet, ", "))
	}
	if len(ai.Template) > 0 {
		refs = append(refs, "template: "+strings.Join(ai.Template, ", "))
	}
	if ai.Server != "" {
		refs = append(refs, "server: "+ai.Server)
	}
	if ai.Group != "" {
		refs = append(refs, "group: "+ai.Group)
	}
	if ai.Facility != "" {
		refs = append(refs, "facility: "+ai.Facility)
	}
	if len(refs) > 0 {
		line += " (" + strings.Join(refs, "; ") + ")"
	}
	return line
}
