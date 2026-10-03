package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

const weakKeyMessage = "'crt' has an RSA key of 1024 bits: use 2048 to 4096 bits"

// apiFailure returns the error a snippet create gets back from the service
// when NDManager answers with status and body.
func apiFailure(t *testing.T, status int, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	s := service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	_, err := s.SnippetCreate(context.Background(), "acme", service.SnippetCreateOpts{
		Name: "corp-ca", Type: "TRUST_CA", Content: `{"name":"corp-ca","crt":"${corp_ca}"}`,
	})
	if err == nil {
		t.Fatal("expected the request to fail")
	}
	return err
}

func weakKeyFailure(t *testing.T) error {
	t.Helper()
	return apiFailure(t, http.StatusUnprocessableEntity,
		`{"error":"`+weakKeyMessage+`","code":"TRUST_KEY_WEAK"}`)
}

// useFormatter sets the formatter and -f value a command would run with, and
// puts both back afterwards.
func useFormatter(t *testing.T, f output.Formatter, flag string) {
	t.Helper()
	origFormatter, origFmt := formatter, outputFmt
	formatter, outputFmt = f, flag
	t.Cleanup(func() { formatter, outputFmt = origFormatter, origFmt })
}

func printed(err error) string {
	var buf bytes.Buffer
	printCommandError(&buf, err)
	return buf.String()
}

// decodeCommandError parses printCommandError's JSON output, failing unless it
// is exactly one document.
func decodeCommandError(t *testing.T, out string) map[string]string {
	t.Helper()
	var doc map[string]map[string]string
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("error output is not one JSON document: %v\n%s", err, out)
	}
	return doc["error"]
}

func TestPrintCommandError_TextShowsTheServerCode(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	got := printed(weakKeyFailure(t))

	if want := "Error: [TRUST_KEY_WEAK] " + weakKeyMessage + "\n"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestPrintCommandError_UncodedErrorsAreUnchanged: an error with no code from
// NDManager prints byte for byte what cobra printed before it was silenced.
func TestPrintCommandError_UncodedErrorsAreUnchanged(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	if got := printed(errors.New("--content or --file is required")); got != "Error: --content or --file is required\n" {
		t.Errorf("local error: got %q", got)
	}
	if got := printed(apiFailure(t, http.StatusInternalServerError, `{"error":"Internal server error"}`)); got != "Error: Internal server error\n" {
		t.Errorf("server error without a code: got %q", got)
	}
}

func TestPrintCommandError_JSONCarriesTheCodeAsAField(t *testing.T) {
	useFormatter(t, output.NewJSONFormatter(), "json")

	got := decodeCommandError(t, printed(weakKeyFailure(t)))

	if got["code"] != "TRUST_KEY_WEAK" || got["message"] != weakKeyMessage {
		t.Errorf("got %v, want code TRUST_KEY_WEAK and the message unchanged", got)
	}

	uncoded := decodeCommandError(t, printed(errors.New("--content or --file is required")))
	if _, ok := uncoded["code"]; ok || uncoded["message"] != "--content or --file is required" {
		t.Errorf("an error without a code must have no code field, got %v", uncoded)
	}
}

// TestPrintCommandError_JSONBeforeTheFormatterIsSetUp: a command that fails
// in flag parsing or in PersistentPreRunE has no formatter yet, and the -f
// value decides.
func TestPrintCommandError_JSONBeforeTheFormatterIsSetUp(t *testing.T) {
	useFormatter(t, nil, "json")

	if got := decodeCommandError(t, printed(weakKeyFailure(t))); got["code"] != "TRUST_KEY_WEAK" {
		t.Errorf("got %v", got)
	}
}

// TestPrintCommandError_ConfiguredJSONBeforeTheFormatterIsSetUp: without -f,
// the configured default decides, as it would have for the formatter.
func TestPrintCommandError_ConfiguredJSONBeforeTheFormatterIsSetUp(t *testing.T) {
	useFormatter(t, nil, "")
	cfg := config.Get()
	orig := cfg.Output.Format
	cfg.Output.Format = "json"
	t.Cleanup(func() { cfg.Output.Format = orig })

	if got := decodeCommandError(t, printed(weakKeyFailure(t))); got["code"] != "TRUST_KEY_WEAK" {
		t.Errorf("got %v", got)
	}
}

// TestExecutePrintsTheServerCode drives a failing command through Execute,
// which prints the error now that cobra's own printing is silenced. The
// stand-in's PersistentPreRunE replaces the root's, which would load the real
// config and keyring.
func TestExecutePrintsTheServerCode(t *testing.T) {
	failure := weakKeyFailure(t)
	stand := &cobra.Command{
		Use:               "zz-fail",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE:              func(*cobra.Command, []string) error { return failure },
	}
	rootCmd.AddCommand(stand)
	t.Cleanup(func() { rootCmd.RemoveCommand(stand) })
	useFormatter(t, nil, "")

	out, err := runRootCommand(t, "zz-fail")
	if err == nil {
		t.Fatal("Execute must still return the error, so main exits non-zero")
	}
	if want := "Error: [TRUST_KEY_WEAK] " + weakKeyMessage + "\n"; out != want {
		t.Errorf("text: got %q\nwant %q", out, want)
	}

	out, _ = runRootCommand(t, "zz-fail", "-f", "json")
	if got := decodeCommandError(t, out); got["code"] != "TRUST_KEY_WEAK" || got["message"] != weakKeyMessage {
		t.Errorf("json: got %v", got)
	}
}

func TestErrorText(t *testing.T) {
	if got := errorText(weakKeyFailure(t)); got != "[TRUST_KEY_WEAK] "+weakKeyMessage {
		t.Errorf("errorText = %q", got)
	}
	if got := errorText(errors.New("plain")); got != "plain" {
		t.Errorf("an error without a code must be unchanged, got %q", got)
	}
	if strings.Contains(errorText(weakKeyFailure(t)), service.CodeAPIError) {
		t.Error("the service's own API_ERROR tag is not the code a user needs")
	}
}

const uuidInUseMessage = "'uuid' is already used by snippet 'corp-web' in this organization; omit it and NetDefense generates one"

// aliasWithHeldUUID is ALIAS content as `snippet describe` shows it: the
// managed uuid NDManager generated for the snippet it was copied from.
const aliasWithHeldUUID = `{"name":"corp-web","type":"host","content":"10.0.0.1","uuid":"221f3268-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`

// answerAll points the cli's service at a fake NDManager that answers every
// request with status and body, and puts the globals back afterwards.
func answerAll(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	origSvc, origOrg := svc, orgName
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName = "acme"
	t.Cleanup(func() { svc, orgName = origSvc, origOrg })
}

func runCreateWithContent(t *testing.T, content string) error {
	t.Helper()
	flags := snippetCreateCmd.Flags()
	for name, value := range map[string]string{"type": "ALIAS", "content": content} {
		if err := flags.Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		_ = flags.Set("type", "")
		_ = flags.Set("content", "")
	})
	return runSnippetCreate(snippetCreateCmd, []string{"corp-web-copy"})
}

func runUpdateContentWith(t *testing.T, content string) error {
	t.Helper()
	file := t.TempDir() + "/content.json"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write content: %v", err)
	}
	return runSnippetUpdateContent(snippetUpdateContentCmd, []string{"corp-web-copy", file})
}

// TestSnippetWritesShowSNIPPET_UUID_IN_USE: NDManager answers a create or a
// content update that carries a uuid another snippet holds with 409
// SNIPPET_UUID_IN_USE. Both commands must show that code and NDManager's own
// message, which names the holder and the remedy, rather than the generic
// conflict text the client falls back to for a 409 without a body.
func TestSnippetWritesShowSNIPPET_UUID_IN_USE(t *testing.T) {
	body := `{"error":"` + uuidInUseMessage + `","code":"SNIPPET_UUID_IN_USE","blocking_resources":["corp-web"]}`

	for name, run := range map[string]func(*testing.T, string) error{
		"snippet create":         runCreateWithContent,
		"snippet update-content": runUpdateContentWith,
	} {
		t.Run(name, func(t *testing.T) {
			answerAll(t, http.StatusConflict, body)

			err := run(t, aliasWithHeldUUID)
			if err == nil {
				t.Fatal("expected the 409 to fail the command")
			}

			useFormatter(t, output.NewTableFormatter(), "")
			text := printed(err)
			if want := "Error: [SNIPPET_UUID_IN_USE] " + uuidInUseMessage + "\n"; text != want {
				t.Errorf("text: got %q\nwant %q", text, want)
			}
			if strings.Contains(text, "may already exist") {
				t.Error("the generic conflict text replaced NDManager's message")
			}

			useFormatter(t, output.NewJSONFormatter(), "json")
			if got := decodeCommandError(t, printed(err)); got["code"] != "SNIPPET_UUID_IN_USE" || got["message"] != uuidInUseMessage {
				t.Errorf("json: got %v", got)
			}
		})
	}
}

// TestSnippetWritesShowTheMalformedUUIDMessage: NDManager refuses a malformed
// managed uuid with a 422 that has no code. On create it is FastAPI's
// validation list, on a content update a plain error. Both commands show
// NDManager's message, and nothing invents a code for it.
func TestSnippetWritesShowTheMalformedUUIDMessage(t *testing.T) {
	const refusal = "ALIAS snippet content invalid: 'uuid' must be a managed uuid, " +
		"221f3268-xxxx-4xxx-yxxx-xxxxxxxxxxxx in lowercase hex with y one of 8, 9, a or b, or be omitted (it is generated)"

	for name, tc := range map[string]struct {
		run  func(*testing.T, string) error
		body string
	}{
		"snippet create": {runCreateWithContent,
			`{"detail":[{"loc":["body"],"msg":"Value error, ` + refusal + `","type":"value_error"}]}`},
		"snippet update-content": {runUpdateContentWith, `{"error":"` + refusal + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			answerAll(t, http.StatusUnprocessableEntity, tc.body)

			err := tc.run(t, `{"name":"corp-web","type":"host","content":"10.0.0.1","uuid":"221f3268-garbage"}`)
			if err == nil {
				t.Fatal("expected the 422 to fail the command")
			}

			useFormatter(t, output.NewTableFormatter(), "")
			if text := printed(err); !strings.Contains(text, refusal) || strings.Contains(text, "[") {
				t.Errorf("text: got %q", text)
			}
			useFormatter(t, output.NewJSONFormatter(), "json")
			got := decodeCommandError(t, printed(err))
			if _, ok := got["code"]; ok || !strings.Contains(got["message"], refusal) {
				t.Errorf("json: got %v", got)
			}
		})
	}
}
