package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// pullQuery runs SnippetPull against a stub and returns the query the client
// actually sent.
func pullQuery(t *testing.T, opts SnippetPullOpts) url.Values {
	t.Helper()

	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task": "T-1", "name": "office-lan", "config_type": "RULE", "status": "PENDING",
		})
	}))
	defer srv.Close()

	if _, err := newTestService(t, srv).SnippetPull(context.Background(), "acme", "fw01", opts); err != nil {
		t.Fatalf("SnippetPull: %v", err)
	}
	return got
}

// TestSnippetPullSendsMatchKeyAndSnippetName covers the split Community #10
// asked for: the positional argument is the device-side match key and may
// contain anything, while snippet_name is the constrained identifier of the
// snippet being created.
func TestSnippetPullSendsMatchKeyAndSnippetName(t *testing.T) {
	t.Run("a match key with spaces is sent verbatim", func(t *testing.T) {
		// A real rule description. This is the case that was refused before.
		q := pullQuery(t, SnippetPullOpts{
			Name:       "Allow HTTPS from LAN to DMZ",
			ConfigType: "RULE",
		})

		if got := q.Get("name"); got != "Allow HTTPS from LAN to DMZ" {
			t.Errorf("name = %q, want the match key unaltered", got)
		}
		if q.Has("snippet_name") {
			t.Errorf("snippet_name must be omitted entirely when not given, so the server derives one; got %q", q.Get("snippet_name"))
		}
		if got := q.Get("config_type"); got != "RULE" {
			t.Errorf("config_type = %q", got)
		}
	})

	t.Run("--name is sent as snippet_name alongside the match key", func(t *testing.T) {
		q := pullQuery(t, SnippetPullOpts{
			Name:        "Allow HTTPS from LAN to DMZ",
			SnippetName: "allow-https-lan-dmz",
			ConfigType:  "RULE",
		})

		if got := q.Get("name"); got != "Allow HTTPS from LAN to DMZ" {
			t.Errorf("name = %q, want the match key unaltered", got)
		}
		if got := q.Get("snippet_name"); got != "allow-https-lan-dmz" {
			t.Errorf("snippet_name = %q", got)
		}
	})

	t.Run("the collision flags are unchanged", func(t *testing.T) {
		q := pullQuery(t, SnippetPullOpts{
			Name: "web-servers", SnippetName: "web-servers", AutoCreate: true, Overwrite: true,
		})

		if q.Get("auto_create") != "true" || q.Get("overwrite") != "true" {
			t.Errorf("collision semantics should be untouched: %v", q)
		}
	})
}

func TestSnippetPullRejectsABadSnippetNameBeforeSending(t *testing.T) {
	// No server: reaching one would itself be the failure.
	svc := New(nil, nil, nil)

	_, err := svc.SnippetPull(context.Background(), "acme", "fw01", SnippetPullOpts{
		Name:        "Allow HTTPS from LAN to DMZ",
		SnippetName: "allow https lan dmz",
	})
	if err == nil {
		t.Fatal("a snippet name with spaces must be rejected client-side")
	}

	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
		t.Fatalf("expected %s, got %#v", CodeInvalidInput, err)
	}
	if !strings.Contains(err.Error(), "match key may contain spaces") {
		t.Errorf("the message should draw the distinction the flag exists for:\n%s", err.Error())
	}
}

func TestValidateSnippetName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "letters digits and the three punctuation marks", in: "allow-https_v2.1"},
		{name: "a single character", in: "a"},
		{name: "mixed case is allowed", in: "Allow-HTTPS"},
		{name: "at the length limit", in: strings.Repeat("a", 255)},

		{name: "empty", in: "", wantErr: "cannot be empty"},
		{name: "a space", in: "allow https", wantErr: "may only contain"},
		{name: "a slash", in: "allow/https", wantErr: "may only contain"},
		{name: "a colon", in: "allow:https", wantErr: "may only contain"},
		{name: "one over the length limit", in: strings.Repeat("a", 256), wantErr: "maximum is 255"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnippetName(tt.in)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateSnippetNameCountsRunes: the length is reported in characters, so
// it has to be counted in characters. len() would report bytes, which a user
// cannot reconcile with what they typed.
//
// A multi-byte name is rejected by the charset check regardless — this is
// about the number in the message being true when it is the one that fires.
func TestValidateSnippetNameCountsRunes(t *testing.T) {
	// 200 three-byte runes: 600 bytes, comfortably over the 255 byte-count
	// threshold but under it by rune count.
	multibyte := strings.Repeat("日", 200)

	err := ValidateSnippetName(multibyte)
	if err == nil {
		t.Fatal("non-ASCII must be rejected by the charset rule")
	}
	if strings.Contains(err.Error(), "maximum is") {
		t.Errorf("rejected for length when it is 200 characters, not 600; len() counted bytes:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "may only contain") {
		t.Errorf("expected the charset rejection:\n%s", err.Error())
	}
}

// TestDeriveSnippetName pins the derivation to the broker's rule. The client
// never sends the result — the server derives the name it stores — so the only
// thing this has to get right is whether the result is empty, which is what
// decides between a useful error and two wasted device tasks.
func TestDeriveSnippetName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "spaces become hyphens and case is folded", in: "SSH by WAN", want: "ssh-by-wan"},
		{name: "a run of punctuation and spaces collapses to one hyphen", in: "Allow HTTP/HTTPS from LAN", want: "allow-http-https-from-lan"},
		{name: "leading and trailing hyphens are trimmed", in: "*** WAN ***", want: "wan"},
		{name: "nothing survives", in: "@@@###", want: ""},
		{name: "only punctuation", in: "***", want: ""},
		{name: "only whitespace", in: "   ", want: ""},
		{name: "empty", in: "", want: ""},
		{name: "the charset passes through untouched", in: "web_srv-01.lan", want: "web_srv-01.lan"},
		{name: "digits are kept", in: "VLAN 100", want: "vlan-100"},
		{name: "a long run is still one hyphen", in: "a !!!??? b", want: "a-b"},
		{name: "truncated to the bound", in: strings.Repeat("a", 300), want: strings.Repeat("a", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveSnippetName(tt.in); got != tt.want {
				t.Errorf("DeriveSnippetName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSnippetPullRefusesAnUnderivableNameBeforeSending is the point of the
// derivation living here at all.
//
// Without it the server derives, but only after dispatching a task to the
// device: an underivable match key cost a round trip and a device task, and
// then failed with a message about the object rather than the name. When the
// object did not exist the user saw only "not found" and never learned that
// naming the snippet was the way through.
func TestSnippetPullRefusesAnUnderivableNameBeforeSending(t *testing.T) {
	for _, flags := range []struct {
		name       string
		autoCreate bool
		overwrite  bool
	}{
		{name: "auto-create", autoCreate: true},
		{name: "overwrite", overwrite: true},
		{name: "both", autoCreate: true, overwrite: true},
	} {
		t.Run(flags.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("a request was sent for an underivable name: %s %s", r.Method, r.URL)
			}))
			defer srv.Close()

			_, err := newTestService(t, srv).SnippetPull(context.Background(), "acme", "fw01", SnippetPullOpts{
				Name:       "@@@###",
				AutoCreate: flags.autoCreate,
				Overwrite:  flags.overwrite,
			})
			if err == nil {
				t.Fatal("expected a refusal")
			}

			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
				t.Fatalf("expected %s, got %#v", CodeInvalidInput, err)
			}
			for _, want := range []string{
				"Could not derive a snippet name from '@@@###'",
				"nothing survives outside a-z, 0-9, '.', '_', and '-'",
				SnippetNameHint,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message missing %q:\n%s", want, err.Error())
				}
			}
		})
	}
}

// TestSnippetPullDoesNotDeriveForADisplayOnlyPull: without auto-create or
// overwrite nothing is created, so there is no name to derive and no reason to
// refuse. Refusing here would break a pull that works today.
func TestSnippetPullDoesNotDeriveForADisplayOnlyPull(t *testing.T) {
	q := pullQuery(t, SnippetPullOpts{Name: "@@@###"})

	if got := q.Get("name"); got != "@@@###" {
		t.Errorf("name = %q, want the match key sent as-is", got)
	}
	if q.Has("snippet_name") {
		t.Errorf("nothing should be derived onto the wire; got snippet_name=%q", q.Get("snippet_name"))
	}
}

// TestSnippetPullNeverSendsADerivedName: the server derives the name it
// stores, so there is one source of truth for what gets created. Sending a
// client-derived name would create a second, and the two could drift.
func TestSnippetPullNeverSendsADerivedName(t *testing.T) {
	q := pullQuery(t, SnippetPullOpts{Name: "SSH by WAN", AutoCreate: true})

	if q.Has("snippet_name") {
		t.Errorf("snippet_name must be absent when the user gave no --name; got %q", q.Get("snippet_name"))
	}
	if got := q.Get("name"); got != "SSH by WAN" {
		t.Errorf("name = %q, want the match key unaltered", got)
	}
}
