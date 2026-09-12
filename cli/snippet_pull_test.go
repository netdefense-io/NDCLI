package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestSnippetPullNameFlagIsWired guards the silent failure mode: runSnippetPull
// reads --name with GetString and ignores the error, so a rename on either side
// yields an empty string, SnippetName is never sent, and the server quietly
// derives a name instead of using the one the user asked for.
func TestSnippetPullNameFlagIsWired(t *testing.T) {
	prepareRootCommand()

	cmd, _, err := rootCmd.Find([]string{"snippet", "pull"})
	if err != nil {
		t.Fatalf("find snippet pull: %v", err)
	}

	flag := cmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("--name is not declared; the snippet name can never be sent")
	}
	if flag.Value.Type() != "string" {
		t.Errorf("--name type = %q, want string", flag.Value.Type())
	}
	if flag.DefValue != "" {
		t.Errorf("--name default = %q; it must be empty so the server derives a name when unset", flag.DefValue)
	}
}

// TestSnippetPullHelpDescribesTheSplit: the whole point of Community #10 is
// that the positional is a match key rather than the snippet's name, and a
// user hitting the old rejection needs the help to say so.
func TestSnippetPullHelpDescribesTheSplit(t *testing.T) {
	prepareRootCommand()

	cmd, _, err := rootCmd.Find([]string{"snippet", "pull"})
	if err != nil {
		t.Fatalf("find snippet pull: %v", err)
	}

	help := cmd.Long + " " + cmd.Use
	for _, want := range []string{"match-key", "MATCH KEY", "spaces", "--name"} {
		if !strings.Contains(help, want) {
			t.Errorf("help should mention %q:\n%s\nUse: %s", want, cmd.Long, cmd.Use)
		}
	}
}

// TestRunSnippetPullValidatesBeforeCallingTheService asserts the guarantee a
// user actually has: an invalid --name fails without a request being made. The
// service is wired to a server that fails the test if anything reaches it, so
// "rejected early" and "nothing was sent" are the same assertion.
//
// What it does NOT isolate: validation is deliberately in two places, here and
// in SnippetPull itself, so this still passes if either gate is removed. That
// is the intended defence in depth rather than an oversight — the CLI check
// exists so the error arrives without a round trip, the service check so every
// front-end gets it. TestSnippetPullRejectsABadSnippetNameBeforeSending covers
// the service gate on its own.
func TestRunSnippetPullValidatesBeforeCallingTheService(t *testing.T) {
	prepareRootCommand()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the service was called despite an invalid --name: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	// Swap the globals runSnippetPull reads, and put them back afterwards.
	origSvc, origOrg := svc, orgName
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName = "acme" // requireOrganization calls os.Exit when this is empty
	t.Cleanup(func() { svc, orgName = origSvc, origOrg })

	cmd, _, err := rootCmd.Find([]string{"snippet", "pull"})
	if err != nil {
		t.Fatalf("find snippet pull: %v", err)
	}
	if err := cmd.Flags().Set("name", "allow https"); err != nil {
		t.Fatalf("set --name: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("name", "") })

	err = runSnippetPull(cmd, []string{"fw01", "Allow HTTPS from LAN to DMZ"})
	if err == nil {
		t.Fatal("expected the invalid --name to be rejected")
	}

	var svcErr *service.Error
	if !errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput {
		t.Fatalf("expected %s, got %#v", service.CodeInvalidInput, err)
	}
	if !strings.Contains(err.Error(), "may only contain") {
		t.Errorf("expected the charset message:\n%s", err.Error())
	}
}

// noopAuthProvider satisfies api.AuthProvider for a client that must never be
// used.
type noopAuthProvider struct{}

func (noopAuthProvider) GetAccessToken() (string, error) { return "unused", nil }
func (noopAuthProvider) ForceRefresh() error             { return nil }

// TestNamedSnippetNameFlag: the service leaves a marker because an MCP caller
// has no flags; the CLI has to turn it into the flag a user can type. A test
// fails if the placeholder ever reaches user-facing text.
func TestNamedSnippetNameFlag(t *testing.T) {
	underivable := &service.Error{
		Code: service.CodeInvalidInput,
		Message: "Could not derive a snippet name from '@@@###': nothing survives outside " +
			"a-z, 0-9, '.', '_', and '-'. Pass " + service.SnippetNameHint + " to set the snippet name explicitly.",
	}

	got := namedSnippetNameFlag(underivable).Error()

	if strings.Contains(got, service.SnippetNameHint) {
		t.Errorf("the placeholder reached the user:\n%s", got)
	}
	// The CLI rendering has to match the broker's wording exactly, so the two
	// surfaces do not describe the same refusal differently.
	want := "Could not derive a snippet name from '@@@###': nothing survives outside " +
		"a-z, 0-9, '.', '_', and '-'. Pass --name to set the snippet name explicitly."
	if got != want {
		t.Errorf("message =\n%s\nwant\n%s", got, want)
	}

	// The code has to survive, since callers switch on it.
	var svcErr *service.Error
	if !errors.As(namedSnippetNameFlag(underivable), &svcErr) || svcErr.Code != service.CodeInvalidInput {
		t.Error("the service code should be preserved")
	}

	// Unrelated errors pass through untouched.
	other := &service.Error{Code: service.CodeInvalidInput, Message: "something else"}
	if got := namedSnippetNameFlag(other); got != error(other) {
		t.Errorf("an unrelated error should pass through, got %v", got)
	}
}
