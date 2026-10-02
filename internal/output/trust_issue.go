package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// formatTrustIssueLine renders one TRUST_CA/TRUST_CERT build-time issue (a
// trust_issues entry from NDManager) in the same single-line shape as
// formatAuthIssueLine: the code, the message, and whichever names the
// issue carries.
func formatTrustIssueLine(ti models.TrustIssue) string {
	line := fmt.Sprintf("[%s] %s", ti.Code, ti.Message)

	var refs []string
	if len(ti.Snippet) > 0 {
		refs = append(refs, "snippet: "+strings.Join(ti.Snippet, ", "))
	}
	if len(ti.Template) > 0 {
		refs = append(refs, "template: "+strings.Join(ti.Template, ", "))
	}
	if ti.Name != "" {
		refs = append(refs, "name: "+ti.Name)
	}
	if len(refs) > 0 {
		line += " (" + strings.Join(refs, "; ") + ")"
	}
	return line
}
