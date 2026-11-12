package cli

import (
	"errors"
	"net/http"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// A connect that would open neither a shell nor a tunnel is refused before
// a CONNECT task is created; it used to pair with the device and exit 0
// having served nothing (#318).
func TestDeviceConnectRefusesWhenNothingCanBeServed(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		noWebadmin, webadminOnly, stdout, stdin bool
		refused                                 bool
	}{
		{"shell and tunnel", false, false, true, true, false},
		{"tunnel only, no terminal", false, false, false, false, false},
		{"shell only, both terminals", true, false, true, true, false},
		{"shell only, stdin piped", true, false, true, false, true},
		{"shell only, stdout redirected", true, false, false, true, true},
		{"--webadmin-only --no-webadmin", true, true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := connectRefusal(tc.noWebadmin, tc.webadminOnly, tc.stdout, tc.stdin)
			var svcErr *service.Error
			if tc.refused != (err != nil) || (err != nil && (!errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput)) {
				t.Errorf("connectRefusal() = %v, want refused=%v with INVALID_INPUT", err, tc.refused)
			}
		})
	}

	cmd := findCommand(t, "device", "connect")
	orig := connectTerminals
	connectTerminals = func() (bool, bool) { return true, false }
	t.Cleanup(func() { connectTerminals = orig })
	if err := cmd.Flags().Set("no-webadmin", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f := cmd.Flags().Lookup("no-webadmin")
		_ = f.Value.Set("false")
		f.Changed = false
	})

	var runErr error
	runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused connect sent %s %s", r.Method, r.URL.Path)
	}, "table", "", func() error {
		runErr = runDeviceConnect(cmd, []string{"fw01"})
		return nil
	})
	if runErr == nil {
		t.Error("device connect --no-webadmin with stdin piped was not refused")
	}
}

// The shell decision reaches the relay client from the same terminal reads
// the refusal judged.
func TestConnectClientConfigCarriesTheTerminals(t *testing.T) {
	for _, tc := range []struct{ stdout, stdin bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		cfg := connectClientConfig("s", 0, false, false, nil, tc.stdout, tc.stdin)
		if cfg.IsTTY != tc.stdout || cfg.StdinIsTTY != tc.stdin {
			t.Errorf("stdout %v stdin %v: IsTTY = %v, StdinIsTTY = %v", tc.stdout, tc.stdin, cfg.IsTTY, cfg.StdinIsTTY)
		}
	}
}
