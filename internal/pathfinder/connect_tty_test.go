package pathfinder

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

// A shell needs a terminal on stdin (raw mode) and on stdout (the remote
// screen). With only one of them it used to be attempted and fail into the
// webadmin keep-alive (#318).
func TestShellNeedsATerminalOnStdinAndStdout(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		stdout, stdin, webAdminOnly bool
		want                        bool
	}{
		{"both terminals", true, true, false, true},
		{"stdin piped", true, false, false, false},
		{"stdout redirected", false, true, false, false},
		{"neither", false, false, false, false},
		{"--webadmin-only", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{isTTY: tc.stdout, stdinIsTTY: tc.stdin, webAdminOnly: tc.webAdminOnly}
			if got := c.shellWanted(); got != tc.want {
				t.Errorf("shellWanted() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Without a terminal on stdout the tunnel's URL goes to stderr, so a script or
// a nohup run can use a port that was assigned automatically (#318).
func TestWebAdminURLIsReportedWithoutATerminal(t *testing.T) {
	var stderr bytes.Buffer
	(&Client{isTTY: false}).announceWebAdmin(&stderr, "http://localhost:54321")
	if got, want := stderr.String(), "WebAdmin tunnel: http://localhost:54321\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// A tunnel that cannot listen is reported and skipped when a shell will run,
// and fails the session when nothing else would be served.
func TestTunnelThatCannotListen(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port

	var stderr bytes.Buffer
	withShell := &Client{webAdminPort: port, isTTY: true, stdinIsTTY: true}
	if tunnel, err := withShell.startTunnel(nil, &stderr); tunnel != nil || err != nil {
		t.Errorf("with a shell: got tunnel %v, err %v; want neither", tunnel, err)
	}
	if !strings.HasPrefix(stderr.String(), "WebAdmin tunnel did not start: ") {
		t.Errorf("with a shell: stderr = %q", stderr.String())
	}

	stderr.Reset()
	webAdminOnly := &Client{webAdminPort: port, isTTY: true, stdinIsTTY: true, webAdminOnly: true}
	if _, err := webAdminOnly.startTunnel(nil, &stderr); err == nil || !strings.Contains(err.Error(), "webadmin tunnel did not start") {
		t.Errorf("without a shell: err = %v, want the tunnel failure", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("without a shell: the failure is the error, not a stderr line: %q", stderr.String())
	}
}

// A read-only device refuses the shell; with no tunnel up the session has
// served nothing, so it fails instead of exiting 0.
func TestRefusedShellWithoutATunnelFails(t *testing.T) {
	if err := (&Client{webAdminEnabled: true}).refusedShellError(true); err != nil {
		t.Errorf("with a tunnel up: %v, want the session to go on", err)
	}
	for _, c := range []*Client{{webAdminEnabled: true}, {webAdminEnabled: false}} {
		if err := c.refusedShellError(false); err == nil || !strings.Contains(err.Error(), "refused the terminal") {
			t.Errorf("webadmin %v, no tunnel: err = %v, want the refusal", c.webAdminEnabled, err)
		}
	}
}
