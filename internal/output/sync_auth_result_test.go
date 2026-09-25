package output

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSymbolForResult_AuthActionsAndWarningStatus guards the AUTH_SERVER/
// AUTH_ORDER additions to symbolForResult: "unchanged"/"written" get
// their own symbols, and a "warning" status (ORDER_NAMES_LOCAL_SERVER,
// PRIVILEGED_LOCAL_USERS_SHADOWABLE, ...) must render as ⚠, not the ✗ a
// real refusal gets — the generic "any non-success/ok status is a
// failure" rule would otherwise catch "warning" too.
func TestSymbolForResult_AuthActionsAndWarningStatus(t *testing.T) {
	cases := []struct {
		name  string
		entry syncResultEntry
		want  string
	}{
		{"server unchanged", syncResultEntry{Type: "auth_server", Action: "unchanged", Status: "success"}, "="},
		{"server created", syncResultEntry{Type: "auth_server", Action: "created", Status: "success"}, "+"},
		{"facility written", syncResultEntry{Type: "auth_facility", Action: "written", Status: "success"}, "~"},
		{"facility unchanged", syncResultEntry{Type: "auth_facility", Action: "unchanged", Status: "success"}, "="},
		{"server blocked", syncResultEntry{Type: "auth_server", Action: "blocked", Status: "blocked"}, "✗"},
		{"facility refused", syncResultEntry{Type: "auth_facility", Action: "refused", Status: "blocked"}, "✗"},
		{"deferred", syncResultEntry{Type: "user", Action: "deferred", Status: "blocked"}, "✗"},
		{"local-server warning", syncResultEntry{Type: "auth_local_server", Action: "warning", Status: "warning"}, "⚠"},
		{"shadowable warning", syncResultEntry{Type: "auth_warning", Action: "warning", Status: "warning"}, "⚠"},
	}
	for _, tc := range cases {
		got := symbolForResult(tc.entry)
		if got != tc.want {
			t.Errorf("%s: symbolForResult() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestWriteResultLines_RendersAuthDetailFields guards the additive
// Code/Before/After/Available/Risks fields against being silently
// dropped by writeResultLines — a plain Type/Action/Status/Error line
// alone would lose exactly the detail these fields exist to carry.
func TestWriteResultLines_RendersAuthDetailFields(t *testing.T) {
	var b strings.Builder
	writeResultLines(&b, []syncResultEntry{
		{
			// The helper sets after == before on every refusal
			// (AuthServerHelper.php) — a refused write never changes the
			// live order — so this fixture keeps the two identical rather
			// than showing what the rejected write would have produced.
			Type: "auth_facility", Name: "webadmin", Action: "refused", Status: "blocked",
			Code: "AUTH_ORDER_UNRESOLVED", Error: `auth order "webadmin": AUTH_ORDER_UNRESOLVED`,
			Before: []string{"Local Database"}, After: []string{"Local Database"},
			Available: []string{"Local Database", "HQ-LDAP"},
		},
		{
			Type: "auth_local_server", Name: "LAB-LOCAL", Action: "warning", Status: "warning",
			Code: "ORDER_NAMES_LOCAL_SERVER", Risks: []string{"cleartext", "unscoped_sync"},
		},
	})
	out := b.String()

	for _, want := range []string{
		"before: [Local Database]",
		"after: [Local Database]",
		"available: Local Database, HQ-LDAP",
		"code: AUTH_ORDER_UNRESOLVED",
		"risks: cleartext, unscoped_sync",
		"code: ORDER_NAMES_LOCAL_SERVER",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestWriteResultLines_SuppressesOKCodeAndUnchangedBeforeAfter guards
// against noise on the common case: the helper reports code "OK" on
// every successful create/update/unchanged/delete, so printing it would
// put a redundant "code: OK" under nearly every AUTH_SERVER/AUTH_ORDER
// row, and an unchanged facility's before/after are always identical by
// definition — the "=" symbol already says nothing changed.
func TestWriteResultLines_SuppressesOKCodeAndUnchangedBeforeAfter(t *testing.T) {
	var b strings.Builder
	writeResultLines(&b, []syncResultEntry{
		{Type: "auth_server", Name: "Corp-AD", Action: "created", Status: "success", Code: "OK"},
		{
			Type: "auth_facility", Name: "webadmin", Action: "unchanged", Status: "success", Code: "OK",
			Before: []string{"Local Database"}, After: []string{"Local Database"},
		},
		{
			Type: "auth_facility", Name: "vpn", Action: "written", Status: "success", Code: "OK",
			Before: []string{"Local Database"}, After: []string{"Local Database", "Corp-AD"},
		},
	})
	out := b.String()

	if strings.Contains(out, "code: OK") {
		t.Errorf("output should never render a bare success code:\n%s", out)
	}
	if strings.Contains(out, "before: [Local Database]  after: [Local Database]") {
		t.Errorf("an unchanged facility's identical before/after should be suppressed:\n%s", out)
	}
	// A written facility's before/after differ, so that line must survive.
	if !strings.Contains(out, "before: [Local Database]  after: [Local Database, Corp-AD]") {
		t.Errorf("a written facility's before/after should still render:\n%s", out)
	}
}

// TestSyncApplyFormatters_TableRendersAuthIssues is the table-formatter
// counterpart to TestSyncApplyFormatters_RenderAuthIssues in
// auth_issue_test.go, which only covers simple and detailed —
// TableFormatter.FormatSyncApply writes with fmt.Printf rather than
// through BaseFormatter.Writer, so it needs captureStdout rather than a
// bytes.Buffer to be exercised at all.
func TestSyncApplyFormatters_TableRendersAuthIssues(t *testing.T) {
	syncErr := models.SyncError{
		DeviceName: "e2e-a",
		Error:      "AUTH build failed",
		Code:       "AUTH_BUILD_INVALID",
		AuthIssues: []models.AuthIssue{
			{Code: "AUTH_GROUP_NOT_ATTACHED", Message: "group not attached", Group: "IT-Staff", Server: "Corp-AD"},
		},
	}
	f := &TableFormatter{}
	out := captureStdout(t, func() {
		if err := f.FormatSyncApply(makeSyncResponse(syncErr)); err != nil {
			t.Fatalf("format failed: %v", err)
		}
	})
	for _, want := range []string{"AUTH_GROUP_NOT_ATTACHED", "group not attached"} {
		if !strings.Contains(out, want) {
			t.Errorf("table formatter missing %q in auth_issues rendering, got:\n%s", want, out)
		}
	}
}

// TestFormatTaskMessage_AuthFacilityRefused is the end-to-end path: the
// same JSON envelope NDBroker stores in Tasks.message, decoded and
// rendered through FormatTaskMessage.
func TestFormatTaskMessage_AuthFacilityRefused(t *testing.T) {
	// The helper sets after == before on every refusal, so this fixture
	// keeps the two identical rather than showing a written-looking after.
	raw := `{"message": "AUTH +0; 1 errors", "results": [` +
		`{"type": "auth_facility", "uuid": "", "name": "webadmin", "action": "refused", "status": "blocked",` +
		` "code": "AUTH_ORDER_UNRESOLVED", "error": "auth order \"webadmin\": AUTH_ORDER_UNRESOLVED",` +
		` "before": ["Local Database"], "after": ["Local Database"], "available": ["Local Database"]}` +
		`], "validation_errors": []}`
	got := FormatTaskMessage(raw)

	for _, want := range []string{
		"✗ auth_facility",
		"webadmin",
		"before: [Local Database]",
		"after: [Local Database]",
		"available: Local Database",
		"code: AUTH_ORDER_UNRESOLVED",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n--- got ---\n%s", want, got)
		}
	}
}
