//go:build !windows

package pathfinder

import (
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestShellRun_TeardownReturnsOnImmediateClose drives the FULL shell session
// teardown — not just forwardInput — through the real StreamManager, stream
// dataWorker, output io.Copy and deferred terminal restore, against a paired
// relay with no socket. It reproduces the operator repro: a shell that emits a
// burst of output and is then closed immediately (Logout / option 0), which
// maximizes the startup/teardown overlap. The invariant: run() must RETURN
// (so Connect can reach waitForQuit) — if any teardown goroutine deadlocks,
// run() never returns and this test times out, exactly as the live hang does.
func TestShellRun_TeardownReturnsOnImmediateClose(t *testing.T) {
	// A real PTY so term.MakeRaw / term.Restore operate on a genuine terminal
	// (the production path), unlike a plain pipe which is not a tty.
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("cannot allocate pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)

	// Capture frames the session sends (open/data/close) so we can drive the
	// peer side. We don't need a real socket; SendFrame buffers into binaryChan.
	s := &ShellSession{
		streamManager: streamMgr,
		fd:            int(tty.Fd()),
	}

	resCh := make(chan struct {
		outcome ShellOutcome
		err     error
	}, 1)
	go func() {
		outcome, rerr := s.run()
		resCh <- struct {
			outcome ShellOutcome
			err     error
		}{outcome, rerr}
	}()

	// run() opens "shell" (id 1) and "shell-ctl" (id 2), then a probe goroutine
	// reads the shell stream. Feed a burst of output so the session passes the
	// refusal probe and enters the interactive copy, then close the shell
	// stream immediately — the "logout right away" race.
	time.Sleep(100 * time.Millisecond)

	burst := []byte("\r\n*** OPNsense console ***\r\nEnter an option: ")
	streamMgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: burst}))

	// Give the probe + output goroutine a beat to consume the first chunk and
	// flip into raw mode, then close the shell stream (remote shell exited).
	time.Sleep(50 * time.Millisecond)
	streamMgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeClose, StreamID: 1}))

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("run() returned error: %v", res.err)
		}
		if res.outcome.Refused {
			t.Fatalf("expected a real session (Refused=false), got Refused=true")
		}
	case <-time.After(5 * time.Second):
		dumpGoroutines("test: run() teardown deadlock")
		t.Fatal("run() did not return within 5s after the shell stream closed — teardown deadlock (the live hang)")
	}

	// The terminal fd must be back in blocking mode for the keep-alive wait.
	if isNonblock(t, int(tty.Fd())) {
		t.Fatal("tty left non-blocking after run() returned")
	}
	_ = os.Stdout
}
