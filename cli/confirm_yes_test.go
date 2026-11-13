package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// auth migrate's two [Y/n] questions go through askUnlessYes: -y answers
// yes, an empty line is the default yes, and a prompt nobody answered on a
// stdin that is not a terminal fails naming -y instead of going ahead.
func TestAskUnlessYesForADefaultYesQuestion(t *testing.T) {
	if f := findCommand(t, "auth", "migrate").Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
		t.Fatal("auth migrate has no -y/--yes")
	}
	noServer := func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("asking must send nothing: %s %s", r.Method, r.URL.Path)
	}
	for _, tc := range []struct {
		name, input string
		yesFlag     bool
		want        bool
		wantErr     bool
	}{
		{name: "-y", yesFlag: true, want: true},
		{name: "empty line", input: "\n", want: true},
		{name: "y", input: "y\n", want: true},
		{name: "n", input: "n\n", want: false},
		// runStreams gives the command a pipe, never a terminal.
		{name: "unanswered", input: "", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "migrate"}
			addYesFlag(cmd)
			if tc.yesFlag {
				if err := cmd.Flags().Set("yes", "true"); err != nil {
					t.Fatal(err)
				}
			}
			var got bool
			var err error
			_, stderr := runStreams(t, noServer, "table", tc.input, func() error {
				got, err = askUnlessYes(cmd, "Update config to use keyring storage?", true)
				return nil
			})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "pass -y to confirm") {
					t.Errorf("err = %v, want the refusal naming -y", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %v, err %v; want %v", got, err, tc.want)
			}
			if tc.yesFlag && stderr != "" {
				t.Errorf("with -y: stderr = %q, want no prompt", stderr)
			}
			if !tc.yesFlag && !strings.Contains(stderr, "Update config to use keyring storage? [Y/n]: ") {
				t.Errorf("stderr = %q, want the [Y/n] prompt", stderr)
			}
			if strings.Contains(stderr, "Cancelled") {
				t.Errorf("stderr = %q: a no is the caller's to explain", stderr)
			}
		})
	}
}

// A rerun of auth migrate offers to delete the auth file only when the keyring
// holds exactly its bytes: auth.storage can say keyring without a migration
// having copied them, and the keyring can hold an older session, and then the
// file is the only working copy.
func TestLeftoverAuthFileNeedsItsTokensInTheKeyring(t *testing.T) {
	session := []byte(`{"user_info":{"email":"ann@acme.test"},"refresh_token":"r2"}`)
	older := []byte(`{"user_info":{"email":"ann@acme.test"},"refresh_token":"r1"}`)
	load := func(data []byte, err error) func() ([]byte, error) {
		return func() ([]byte, error) { return data, err }
	}
	askedTheKeyring := false
	for _, tc := range []struct {
		name          string
		file, keyring func() ([]byte, error)
		want          bool
	}{
		{"the keyring holds the file's session", load(session, nil), load(session, nil), true},
		{"the keyring holds an older session", load(session, nil), load(older, nil), false},
		{"the keyring holds nothing", load(session, nil), load(nil, nil), false},
		{"the keyring cannot be read", load(session, nil), load(nil, errors.New("locked")), false},
		{"no file", load(nil, nil), func() ([]byte, error) { askedTheKeyring = true; return session, nil }, false},
		{"the file cannot be read", load(nil, errors.New("denied")), load(session, nil), false},
	} {
		if got := leftoverAuthFile(tc.file, tc.keyring); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if askedTheKeyring {
		t.Error("the keyring was read for a file that is not there")
	}
}
