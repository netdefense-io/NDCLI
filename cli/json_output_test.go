package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// runStreams runs fn against a fake NDManager served by handler, with stdin
// fed from input and the formatter for format, and returns what fn wrote to
// stdout and to stderr. Colour is off so the text compares as written.
func runStreams(t *testing.T, handler http.HandlerFunc, format, input string, fn func() error) (stdout, stderr string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	pipe := func() (*os.File, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		return r, w
	}
	inR, inW := pipe()
	outR, outW := pipe()
	errR, errW := pipe()
	_, _ = io.WriteString(inW, input)
	inW.Close()

	origSvc, origOrg, origFormatter := svc, orgName, formatter
	origIn, origOut, origErr, origColorOut, origNoColor := os.Stdin, os.Stdout, os.Stderr, color.Output, color.NoColor
	os.Stdin, os.Stdout, os.Stderr, color.Output, color.NoColor = inR, outW, errW, outW, true
	defer func() {
		os.Stdin, os.Stdout, os.Stderr, color.Output, color.NoColor = origIn, origOut, origErr, origColorOut, origNoColor
		svc, orgName, formatter = origSvc, origOrg, origFormatter
		output.GetFormatter("")
		inR.Close()
	}()

	drain := func(r *os.File) <-chan string {
		c := make(chan string, 1)
		go func() {
			b, _ := io.ReadAll(r)
			r.Close()
			c <- string(b)
		}()
		return c
	}
	outC, errC := drain(outR), drain(errR)

	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName = "acme"
	formatter = output.GetFormatter(format)
	if err := fn(); err != nil {
		t.Errorf("command failed: %v", err)
	}
	outW.Close()
	errW.Close()
	return <-outC, <-errC
}

// decodeOne fails unless out is exactly one JSON document, decoded into v.
func decodeOne(t *testing.T, out string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
}

func findCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	prepareRootCommand()
	cmd, _, err := rootCmd.Find(path)
	if err != nil {
		t.Fatalf("find %v: %v", path, err)
	}
	return cmd
}

// pullServer answers a snippet pull, and the PULL task's status with
// COMPLETED and the pulled content.
func pullServer(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pull"):
			_, _ = io.WriteString(w, `{"task":"T1","name":"corp","config_type":"ALIAS","status":"PENDING"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/T1":
			_, _ = io.WriteString(w, `{"task":"T1","type":"PULL","status":"COMPLETED","message":"{\"name\":\"corp\",\"type\":\"host\"}"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func setWait(t *testing.T, cmd *cobra.Command, wait bool) {
	t.Helper()
	value := "false"
	if wait {
		value = "true"
	}
	if err := cmd.Flags().Set("wait", value); err != nil {
		t.Fatalf("set --wait: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("wait", "false") })
}

// TestSnippetPullJSON: snippet pull printed a status line and a hint with no
// formatter, so -f json produced prose.
func TestSnippetPullJSON(t *testing.T) {
	cmd := findCommand(t, "snippet", "pull")
	setWait(t, cmd, false)

	stdout, _ := runStreams(t, pullServer(t), "json", "", func() error {
		return runSnippetPull(cmd, []string{"fw01", "corp"})
	})

	var got models.SnippetPull
	decodeOne(t, stdout, &got)
	if got.Task != "T1" || got.Status != "PENDING" || got.Snippet != "corp" || got.ConfigType != "ALIAS" || got.Content != "" {
		t.Errorf("decoded %+v", got)
	}
}

// TestSnippetPullWaitJSON: with --wait, the progress lines and the
// "Content:" block were printed around nothing parseable. Progress now goes
// to stderr and the completed pull is one document carrying the content.
func TestSnippetPullWaitJSON(t *testing.T) {
	cmd := findCommand(t, "snippet", "pull")
	setWait(t, cmd, true)

	stdout, stderr := runStreams(t, pullServer(t), "json", "", func() error {
		return runSnippetPull(cmd, []string{"fw01", "corp"})
	})

	var got models.SnippetPull
	decodeOne(t, stdout, &got)
	if got.Task != "T1" || got.Status != "COMPLETED" || got.Snippet != "corp" || got.Content != `{"name":"corp","type":"host"}` {
		t.Errorf("decoded %+v", got)
	}
	if !strings.Contains(stderr, "Waiting for task to complete...") {
		t.Errorf("progress should be on stderr, got %q", stderr)
	}
}

// TestSnippetPullHumanOutputIsUnchanged: the human formats print what they
// printed before, with the --wait progress lines moved to stderr.
func TestSnippetPullHumanOutputIsUnchanged(t *testing.T) {
	cmd := findCommand(t, "snippet", "pull")

	setWait(t, cmd, false)
	stdout, _ := runStreams(t, pullServer(t), "table", "", func() error {
		return runSnippetPull(cmd, []string{"fw01", "corp"})
	})
	if want := "✓ Pull task created: T1\nCheck task status with: ndcli task describe T1\n"; stdout != want {
		t.Errorf("without --wait:\ngot  %q\nwant %q", stdout, want)
	}

	setWait(t, cmd, true)
	stdout, stderr := runStreams(t, pullServer(t), "table", "", func() error {
		return runSnippetPull(cmd, []string{"fw01", "corp"})
	})
	if want := "✓ Snippet pulled successfully: corp\n\nContent:\n{\n  \"name\": \"corp\",\n  \"type\": \"host\"\n}\n"; stdout != want {
		t.Errorf("with --wait:\ngot  %q\nwant %q", stdout, want)
	}
	if want := "✓ Pull task created: T1\nWaiting for task to complete...\n"; stderr != want {
		t.Errorf("with --wait, stderr:\ngot  %q\nwant %q", stderr, want)
	}
}

// TestSyncStatusEmptyJSON: an empty status printed "No devices found" in
// place of the formatter, so -f json produced prose.
func TestSyncStatusEmptyJSON(t *testing.T) {
	cmd := findCommand(t, "sync", "status")
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"total":0}`)
	}, "json", "", func() error {
		return runSyncStatus(cmd, nil)
	})

	var got struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	decodeOne(t, stdout, &got)
	if len(got.Items) != 0 || got.Total != 0 {
		t.Errorf("decoded %+v", got)
	}
}

// syncServer answers sync status with one device and sync apply with one
// task.
func syncServer(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/status":
			_, _ = io.WriteString(w, `{"items":[{"device_name":"fw01","organization":"acme","in_sync":false}],"total":1}`)
		case "/api/v1/sync":
			_, _ = io.WriteString(w, `{"message":"Sync triggered for 1 device(s)","devices_affected":1,"skipped":0,"tasks":[{"task":"T1","device_name":"fw01"}]}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// TestSyncApplyReviewStaysOffStdout: without --yes, sync apply showed the
// devices to sync, the prompt and any cancellation on stdout, ahead of the
// formatter's document. They are the prompt, so they now go to stderr.
func TestSyncApplyReviewStaysOffStdout(t *testing.T) {
	cmd := findCommand(t, "sync", "apply")

	stdout, stderr := runStreams(t, syncServer(t), "json", "y\n", func() error {
		return runSyncApply(cmd, nil)
	})
	var got models.SyncApplyResponse
	decodeOne(t, stdout, &got)
	if got.DevicesAffected != 1 {
		t.Errorf("decoded %+v", got)
	}
	for _, want := range []string{"Devices to sync (1):", "fw01", "Trigger sync for 1 device(s)? [y/N]: "} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}

	stdout, stderr = runStreams(t, syncServer(t), "json", "n\n", func() error {
		return runSyncApply(cmd, nil)
	})
	if stdout != "" {
		t.Errorf("a cancelled apply must leave stdout empty, got %q", stdout)
	}
	if !strings.HasSuffix(stderr, "Cancelled\n") {
		t.Errorf("stderr should end with the cancellation, got %q", stderr)
	}
}

// TestAuthTokenRevokePromptStaysOffStdout: the revoke prompt and "Aborted."
// were printed to stdout, ahead of FormatTokenRevoked's document.
func TestAuthTokenRevokePromptStaysOffStdout(t *testing.T) {
	cmd := findCommand(t, "auth", "token", "revoke")
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/auth/tokens/ci" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}

	stdout, stderr := runStreams(t, handler, "json", "y\n", func() error {
		return runAuthTokenRevoke(cmd, []string{"ci"})
	})
	var got map[string]any
	decodeOne(t, stdout, &got)
	if got["revoked"] != true || got["name"] != "ci" {
		t.Errorf("decoded %v", got)
	}
	if !strings.Contains(stderr, `Revoke token "ci"?`) {
		t.Errorf("the prompt should be on stderr, got %q", stderr)
	}

	stdout, stderr = runStreams(t, handler, "json", "n\n", func() error {
		return runAuthTokenRevoke(cmd, []string{"ci"})
	})
	if stdout != "" || !strings.HasSuffix(stderr, "Aborted.\n") {
		t.Errorf("an aborted revoke: stdout %q, stderr %q", stdout, stderr)
	}
}

// TestConfirmAndCancellationGoToStderr: helpers.Confirm printed its prompt to
// stdout and the commands printed "Cancelled" there too, so a cancelled
// command left prose where -f json promises a document or nothing.
func TestConfirmAndCancellationGoToStderr(t *testing.T) {
	cmd := findCommand(t, "snippet", "delete")
	stdout, stderr := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a cancelled delete must not reach the server: %s %s", r.Method, r.URL.Path)
	}, "json", "n\n", func() error {
		return runSnippetDelete(cmd, []string{"corp"})
	})
	if stdout != "" {
		t.Errorf("stdout must stay empty, got %q", stdout)
	}
	if want := "Delete snippet 'corp'? [y/N]: Cancelled\n"; stderr != want {
		t.Errorf("stderr:\ngot  %q\nwant %q", stderr, want)
	}
}

// userStub is just enough of an auth manager for svc.AuthLocalUser.
type userStub struct{}

func (userStub) IsAuthenticated() bool { return true }
func (userStub) SessionStatus() *models.SessionStatus {
	return &models.SessionStatus{State: models.SessionValid}
}
func (userStub) GetAccessToken() (string, error) { return "tok", nil }
func (userStub) ForceRefresh() error             { return nil }
func (userStub) GetUserInfo() (*models.UserInfo, error) {
	return &models.UserInfo{Email: "alice@example.com"}, nil
}
func (userStub) GetTokenSummary() map[string]interface{} { return nil }
func (userStub) GetStorageName() string                  { return "stub" }
func (userStub) Logout() error                           { return nil }

// TestAuthDeleteAbortLeavesStdoutEmpty: the warning and the typed-DELETE
// prompt are the prompt, so an abort leaves stdout empty like every other
// cancellation.
func TestAuthDeleteAbortLeavesStdoutEmpty(t *testing.T) {
	cmd := findCommand(t, "auth", "delete")
	stdout, stderr := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an aborted delete must not reach the server: %s %s", r.Method, r.URL.Path)
	}, "json", "no\n", func() error {
		svc = service.NewFromProvider(api.NewClient("http://127.0.0.1:1", false, &noopAuthProvider{}), userStub{}, nil)
		return runAuthDelete(cmd, nil)
	})
	if stdout != "" {
		t.Errorf("stdout must stay empty, got %q", stdout)
	}
	for _, want := range []string{
		"WARNING: This will permanently delete your account!",
		"Email: alice@example.com",
		"Type 'DELETE' to confirm: ",
		"Aborted.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func exampleServer(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/snippet-types" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema":1,"types":[{"type":"TRUST_CERT","docs":"/d","examples":[{"id":"cert-variable","name":"Cert","subtype":null,"content":{"key":"${FW_KEY}"},"requires":{"variables":["FW_KEY"]}}]}]}`)
	}
}

func TestSnippetExampleContentStdoutNeedsStderr(t *testing.T) {
	cmd := findCommand(t, "snippet", "example")
	stdout, stderr := runStreams(t, exampleServer(t), "table", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT", "cert-variable"})
	})
	if strings.TrimSpace(stdout) != "{\n  \"key\": \"${FW_KEY}\"\n}" {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.TrimSpace(stderr) != "Needs: variables FW_KEY\nDocs: /d" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSnippetExampleJSONIsWholeObject(t *testing.T) {
	cmd := findCommand(t, "snippet", "example")
	stdout, _ := runStreams(t, exampleServer(t), "json", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT", "cert-variable"})
	})
	var got models.SnippetExample
	decodeOne(t, stdout, &got)
	if got.ID != "cert-variable" || got.Requires == nil || !strings.Contains(string(got.Content), "${FW_KEY}") {
		t.Errorf("decoded %+v", got)
	}
}

func TestSnippetExampleDocsShown(t *testing.T) {
	cmd := findCommand(t, "snippet", "example")
	stdout, _ := runStreams(t, exampleServer(t), "table", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT"})
	})
	if !strings.Contains(stdout, "Docs: /d") {
		t.Errorf("list stdout lacks the docs path: %q", stdout)
	}

	stdout, stderr := runStreams(t, exampleServer(t), "table", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT", "cert-variable"})
	})
	if strings.Contains(stdout, "Docs") || !strings.Contains(stderr, "Docs: /d") {
		t.Errorf("content stdout must stay clean, docs on stderr: %q / %q", stdout, stderr)
	}

	stdout, _ = runStreams(t, exampleServer(t), "json", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT", "cert-variable"})
	})
	var one struct{ Docs, Type string }
	decodeOne(t, stdout, &one)
	if one.Docs != "/d" || one.Type != "TRUST_CERT" {
		t.Errorf("json example lacks type/docs: %q", stdout)
	}
	stdout, _ = runStreams(t, exampleServer(t), "json", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT"})
	})
	var list struct{ Docs string }
	decodeOne(t, stdout, &list)
	if list.Docs != "/d" {
		t.Errorf("json list lacks docs: %q", stdout)
	}
}

func TestSnippetCreateEditNeedsExample(t *testing.T) {
	cmd := findCommand(t, "snippet", "create")
	for f, v := range map[string]string{"type": "RULE", "edit": "true"} {
		if err := cmd.Flags().Set(f, v); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = cmd.Flags().Set("type", "")
		_ = cmd.Flags().Set("edit", "false")
	})
	var err error
	runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}, "table", "", func() error {
		err = runSnippetCreate(cmd, []string{"x"})
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "--edit needs --example") {
		t.Errorf("err = %v", err)
	}
}

func TestSnippetCreateExamplePrintsNeedsOnStderr(t *testing.T) {
	cmd := findCommand(t, "snippet", "create")
	for f, v := range map[string]string{"type": "trust_cert", "example": "cert-variable"} {
		if err := cmd.Flags().Set(f, v); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, f := range []string{"type", "example"} {
			_ = cmd.Flags().Set(f, "")
			cmd.Flags().Lookup(f).Changed = false
		}
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["type"] != "TRUST_CERT" {
				t.Errorf("created with type %v, want the registry's TRUST_CERT", body["type"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"x","type":"TRUST_CERT","content":"{}"}`)
			return
		}
		exampleServer(t)(w, r)
	}
	stdout, stderr := runStreams(t, handler, "table", "", func() error {
		return runSnippetCreate(cmd, []string{"x"})
	})
	if !strings.Contains(stdout, "Snippet created") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "Needs: variables FW_KEY") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSnippetExampleListTable(t *testing.T) {
	cmd := findCommand(t, "snippet", "example")
	stdout, _ := runStreams(t, exampleServer(t), "table", "", func() error {
		return runSnippetExample(cmd, []string{"TRUST_CERT"})
	})
	if !strings.Contains(stdout, "cert-variable") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSnippetCreateExampleConflicts(t *testing.T) {
	cmd := findCommand(t, "snippet", "create")
	cmd.SetArgs([]string{"x", "--type", "RULE", "--example", "a", "--content", "{}"})
	if err := cmd.ParseFlags([]string{"--type", "RULE", "--example", "a", "--content", "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.ValidateFlagGroups(); err == nil {
		t.Error("--example with --content must conflict")
	}
	t.Cleanup(func() {
		for _, f := range []string{"type", "example", "content"} {
			_ = cmd.Flags().Set(f, "")
			cmd.Flags().Lookup(f).Changed = false
		}
	})
}
