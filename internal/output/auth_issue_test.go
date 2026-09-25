package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestFormatAuthIssueLine_NamesEveryField(t *testing.T) {
	ai := models.AuthIssue{
		Code:     "AUTH_SERVER_NAME_CONFLICT",
		Message:  "two snippets define the same server name",
		Snippet:  []string{"server-a", "server-b"},
		Template: []string{"corp-template"},
		Server:   "Corp-AD/corp-ad",
		Group:    "IT-Staff",
		Facility: "webadmin",
	}
	got := formatAuthIssueLine(ai)
	for _, want := range []string{
		"[AUTH_SERVER_NAME_CONFLICT]",
		"two snippets define the same server name",
		"snippet: server-a, server-b",
		"template: corp-template",
		"server: Corp-AD/corp-ad",
		"group: IT-Staff",
		"facility: webadmin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatAuthIssueLine missing %q, got: %s", want, got)
		}
	}
}

func TestFormatAuthIssueLine_OmitsAbsentFields(t *testing.T) {
	got := formatAuthIssueLine(models.AuthIssue{Code: "SECRET_VARIABLE_REQUIRED", Message: "m"})
	if got != "[SECRET_VARIABLE_REQUIRED] m" {
		t.Errorf("got %q, want no trailing parenthetical when no names are set", got)
	}
}

// TestJSONFormatter_SyncApply_RendersAuthIssues covers the fourth format:
// the JSON formatter marshals models.SyncApplyResponse directly, so
// AuthIssues is a `json:"auth_issues,omitempty"` field away from showing
// up — this pins that it actually does, rather than trusting the field
// tag alone.
func TestJSONFormatter_SyncApply_RendersAuthIssues(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := models.SyncError{
		DeviceName: "e2e-a",
		Error:      "AUTH build failed",
		Code:       "AUTH_BUILD_INVALID",
		AuthIssues: []models.AuthIssue{
			{Code: "SECRET_VARIABLE_REQUIRED", Message: "m", Server: "Corp-AD"},
		},
	}
	if ferr := f.FormatSyncApply(makeSyncResponse(err)); ferr != nil {
		t.Fatalf("format failed: %v", ferr)
	}
	out := buf.String()
	for _, want := range []string{`"auth_issues"`, `"SECRET_VARIABLE_REQUIRED"`, `"server":"Corp-AD"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q, got:\n%s", want, out)
		}
	}
}

// TestSyncApplyFormatters_RenderAuthIssues guards all three human
// formatters against dropping auth_issues — each must render every
// issue's code and message somewhere in a device's error block.
func TestSyncApplyFormatters_RenderAuthIssues(t *testing.T) {
	err := models.SyncError{
		DeviceName: "e2e-a",
		Error:      "AUTH build failed",
		Code:       "AUTH_BUILD_INVALID",
		AuthIssues: []models.AuthIssue{
			{Code: "AUTH_GROUP_NOT_ATTACHED", Message: "group not attached", Group: "IT-Staff", Server: "Corp-AD"},
		},
	}
	resp := makeSyncResponse(err)

	for name, f := range map[string]Formatter{
		"simple":   &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &bytes.Buffer{}}},
		"detailed": &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &bytes.Buffer{}}},
	} {
		if ferr := f.FormatSyncApply(resp); ferr != nil {
			t.Fatalf("%s: format failed: %v", name, ferr)
		}
		var out string
		switch v := f.(type) {
		case *SimpleFormatter:
			out = v.Writer.(*bytes.Buffer).String()
		case *DetailedFormatter:
			out = v.Writer.(*bytes.Buffer).String()
		}
		for _, want := range []string{"AUTH_GROUP_NOT_ATTACHED", "group not attached"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s formatter missing %q in auth_issues rendering, got:\n%s", name, want, out)
			}
		}
	}
}
