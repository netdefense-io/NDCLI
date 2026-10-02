package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestFormatTrustIssueLine_NamesEveryField(t *testing.T) {
	got := formatTrustIssueLine(models.TrustIssue{
		Code:     "TRUST_NAME_CONFLICT",
		Message:  "two snippets define the same certificate name",
		Snippet:  []string{"fw-wildcard", "fw-wildcard-2"},
		Template: []string{"edge", "branch"},
		Name:     "fw-wildcard",
	})
	want := "[TRUST_NAME_CONFLICT] two snippets define the same certificate name " +
		"(snippet: fw-wildcard, fw-wildcard-2; template: edge, branch; name: fw-wildcard)"
	if got != want {
		t.Errorf("formatTrustIssueLine:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestFormatTrustIssueLine_OmitsAbsentFields(t *testing.T) {
	got := formatTrustIssueLine(models.TrustIssue{Code: "TRUST_AGENT_TOO_OLD", Message: "m"})
	if got != "[TRUST_AGENT_TOO_OLD] m" {
		t.Errorf("got %q, want no trailing parenthetical when no names are set", got)
	}
}

// bothIssueListsError is a device error carrying auth_issues and
// trust_issues at once: every renderer must show both lists rather than
// picking one by the top-level code.
func bothIssueListsError() models.SyncError {
	return models.SyncError{
		DeviceName: "e2e-a",
		Error:      "build failed",
		Code:       "AUTH_BUILD_INVALID",
		AuthIssues: []models.AuthIssue{
			{Code: "AUTH_GROUP_NOT_ATTACHED", Message: "group not attached", Group: "IT-Staff", Server: "Corp-AD"},
		},
		TrustIssues: []models.TrustIssue{
			{Code: "TRUST_KEY_MISMATCH", Message: "key does not match certificate", Snippet: []string{"fw-wildcard"}, Name: "fw-wildcard"},
		},
	}
}

var bothIssueListsWant = []string{
	"[AUTH_GROUP_NOT_ATTACHED] group not attached",
	"[TRUST_KEY_MISMATCH] key does not match certificate (snippet: fw-wildcard; name: fw-wildcard)",
}

func TestSyncApplyFormatters_RenderTrustAlongsideAuthIssues(t *testing.T) {
	resp := makeSyncResponse(bothIssueListsError())

	outputs := map[string]string{}
	for name, newFormatter := range map[string]func(*bytes.Buffer) Formatter{
		"simple":   func(b *bytes.Buffer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: b}} },
		"detailed": func(b *bytes.Buffer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: b}} },
	} {
		var buf bytes.Buffer
		if err := newFormatter(&buf).FormatSyncApply(resp); err != nil {
			t.Fatalf("%s: format failed: %v", name, err)
		}
		outputs[name] = buf.String()
	}
	// TableFormatter writes with fmt.Printf, not through BaseFormatter.Writer.
	outputs["table"] = captureStdout(t, func() {
		if err := (&TableFormatter{}).FormatSyncApply(resp); err != nil {
			t.Fatalf("table: format failed: %v", err)
		}
	})

	for name, out := range outputs {
		for _, want := range bothIssueListsWant {
			if !strings.Contains(out, want) {
				t.Errorf("%s formatter missing %q, got:\n%s", name, want, out)
			}
		}
	}
}

// TestSyncApplyFormatters_RenderTrustIssuesAlone covers the common case,
// a device whose only problems are trust issues (TRUST_BUILD_INVALID).
func TestSyncApplyFormatters_RenderTrustIssuesAlone(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := models.SyncError{
		DeviceName: "e2e-b",
		Error:      "build failed",
		Code:       "TRUST_BUILD_INVALID",
		TrustIssues: []models.TrustIssue{
			{Code: "SECRET_VARIABLE_CLEARANCE", Message: "FW_KEY was not written by a Superuser", Snippet: []string{"fw-wildcard"}, Name: "fw-wildcard"},
			{Code: "TRUST_AGENT_TOO_OLD", Message: "agent too old for trust snippets"},
		},
	}
	if ferr := f.FormatSyncApply(makeSyncResponse(err)); ferr != nil {
		t.Fatalf("format failed: %v", ferr)
	}
	out := buf.String()
	for _, want := range []string{
		"[SECRET_VARIABLE_CLEARANCE] FW_KEY was not written by a Superuser (snippet: fw-wildcard; name: fw-wildcard)",
		"[TRUST_AGENT_TOO_OLD] agent too old for trust snippets",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q, got:\n%s", want, out)
		}
	}
}

// TestJSONFormatter_SyncApply_RendersTrustIssues pins that the JSON
// formatter carries trust_issues next to auth_issues, rather than trusting
// the field tag alone.
func TestJSONFormatter_SyncApply_RendersTrustIssues(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatSyncApply(makeSyncResponse(bothIssueListsError())); err != nil {
		t.Fatalf("format failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"auth_issues"`, `"trust_issues"`, `"TRUST_KEY_MISMATCH"`, `"name":"fw-wildcard"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q, got:\n%s", want, out)
		}
	}
}
