//go:build !windows

package pathfinder

import (
	"bytes"
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// isNonblock reports whether the given fd currently has O_NONBLOCK set.
// Uses a raw F_GETFL fcntl, which is portable across the darwin/linux build.
func isNonblock(t *testing.T, fd int) bool {
	t.Helper()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		t.Fatalf("F_GETFL on fd %d: %v", fd, errno)
	}
	return flags&syscall.O_NONBLOCK != 0
}

// newInputPipe returns a raw pipe. The read fd stands in for stdin and is not
// registered with Go's runtime poller, so forwardInput's poll(2)/read(2)
// operate on it exactly as they do on a real terminal fd; writing to the write
// end simulates a keystroke. Both ends are closed at cleanup.
func newInputPipe(t *testing.T) (readFD int, input *os.File) {
	t.Helper()
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	input = os.NewFile(uintptr(fds[1]), "input")
	t.Cleanup(func() {
		input.Close()
		syscall.Close(fds[0])
	})
	return fds[0], input
}

// startForwardInput runs forwardInput on fd. At cleanup it cancels the reader
// and waits for it to return, before newInputPipe's cleanup closes fd: the
// next pipe the process opens reuses the fd number, and a reader still running
// would consume the next test's keystrokes.
func startForwardInput(t *testing.T, s *ShellSession, fd int) (cancel context.CancelFunc, inputDone, forceQuit chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	inputDone = make(chan struct{})
	forceQuit = make(chan struct{})
	go s.forwardInput(ctx, fd, inputDone, forceQuit)
	t.Cleanup(func() {
		cancel()
		select {
		case <-inputDone:
		case <-time.After(2 * time.Second):
			t.Error("forwardInput did not return within 2s of cancel")
		}
	})
	return cancel, inputDone, forceQuit
}

// TestForwardInput_CancelUnblocksBlockingRead is the regression test for the
// post-shell hang. The reader is parked waiting for input that never arrives
// (no keystroke). Cancelling the context MUST wake it promptly so inputDone
// closes — before the fix the goroutine sat in an uninterruptible read until a
// keystroke, inputDone never closed, the deferred terminal restore's join
// blocked forever, and run() never returned (so "WebAdmin tunnel still active"
// never printed).
func TestForwardInput_CancelUnblocksBlockingRead(t *testing.T) {
	readFD, _ := newInputPipe(t)
	s := &ShellSession{fd: readFD}
	cancel, inputDone, _ := startForwardInput(t, s, readFD)

	// Let the goroutine reach poll(2) with no input pending.
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-inputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not return within 2s of cancel while parked with no input — this is the unkillable post-shell hang (run() never returns, waitForQuit never runs)")
	}
}

// TestForwardInput_RestoresBlockingOnReturn verifies the fd is left in blocking
// mode after the reader returns, so the restored terminal behaves normally for
// the keep-alive wait.
func TestForwardInput_RestoresBlockingOnReturn(t *testing.T) {
	readFD, _ := newInputPipe(t)
	s := &ShellSession{fd: readFD}
	cancel, inputDone, _ := startForwardInput(t, s, readFD)

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-inputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close inputDone within 2s of cancel")
	}

	if isNonblock(t, readFD) {
		t.Fatal("fd left in non-blocking mode after forwardInput returned")
	}
}

// TestForwardInput_EOFReturns verifies that EOF on the terminal fd (write end
// closed) also makes the reader return and close inputDone, even without ctx
// cancellation — the normal shell-ended path.
func TestForwardInput_EOFReturns(t *testing.T) {
	readFD, input := newInputPipe(t)
	s := &ShellSession{fd: readFD}
	_, inputDone, _ := startForwardInput(t, s, readFD)

	time.Sleep(50 * time.Millisecond)
	// Closing the write end delivers EOF (POLLHUP) to the read fd.
	input.Close()

	select {
	case <-inputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not return on EOF of the terminal fd")
	}
}

// newStreamForTest returns a ShellSession wired to a real paired relay and an
// open shell stream, plus a channel carrying the payload of each data frame
// written to that stream — what forwardInput sent to the remote shell. The
// stream's OPEN frame is left out, so a receive means input was forwarded.
func newStreamForTest(t *testing.T, readFD int) (*ShellSession, <-chan []byte) {
	t.Helper()
	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)
	shellStream, err := streamMgr.OpenStream("shell")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	forwarded := make(chan []byte, 64)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case raw := <-relay.binaryChan:
				if f, err := DecodeFrame(raw); err == nil && f.Type == FrameTypeData {
					forwarded <- f.Data
				}
			case <-stop:
				return
			}
		}
	}()
	// A stream holds a 100,000-slot buffer and a worker goroutine until it is
	// closed.
	t.Cleanup(func() {
		close(stop)
		shellStream.Close()
	})
	s := &ShellSession{streamManager: streamMgr, shellStream: shellStream, fd: readFD}
	return s, forwarded
}

// expectForwarded fails unless the next data written to the shell stream is
// exactly want.
func expectForwarded(t *testing.T, forwarded <-chan []byte, want []byte) {
	t.Helper()
	select {
	case got := <-forwarded:
		if !bytes.Equal(got, want) {
			t.Fatalf("forwarded %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("nothing forwarded within 2s, want %q", want)
	}
}

func writeInput(t *testing.T, input *os.File, b []byte) {
	t.Helper()
	if _, err := input.Write(b); err != nil {
		t.Fatalf("write %q: %v", b, err)
	}
}

// TestForwardInput_CtrlCIsForwarded verifies that pressing Ctrl-C (0x03) in
// raw mode is forwarded to the remote shell stream and does NOT trigger the
// force-quit channel. This is the correct behavior: Ctrl-C interrupts the
// remote foreground command; it must not kill the session.
func TestForwardInput_CtrlCIsForwarded(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, forwarded := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	writeInput(t, input, []byte{0x03})

	// forceQuit must NOT fire.
	select {
	case <-forceQuit:
		t.Fatal("forwardInput closed forceQuit on Ctrl-C (0x03) — this kills the session instead of interrupting the remote command")
	case <-time.After(300 * time.Millisecond):
		// Good — no force-quit.
	}

	expectForwarded(t, forwarded, []byte{0x03})
}

// TestForwardInput_TildeDotAtLineStartForcequits verifies that \r~. triggers
// force-quit without forwarding the escape bytes.
func TestForwardInput_TildeDotAtLineStartForcequits(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, forwarded := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	// \r moves to line start, then ~. triggers force-quit.
	writeInput(t, input, []byte{'\r', '~', '.'})

	select {
	case <-forceQuit:
		// Good.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close forceQuit on \\r~. — wedged session cannot be escaped")
	}

	expectForwarded(t, forwarded, []byte{'\r'})
}

// TestForwardInput_TildeDotAtSessionStartForcequits verifies that ~. at the
// very start of the session (before any \r/\n has been seen) also triggers
// force-quit, because the state machine initialises atLineStart=true.
func TestForwardInput_TildeDotAtSessionStartForcequits(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, _ := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	writeInput(t, input, []byte{'~', '.'})

	select {
	case <-forceQuit:
		// Good.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close forceQuit on ~. at session start")
	}
}

// TestForwardInput_TildeMidLineIsForwarded verifies that a tilde that is NOT
// at the start of a line is forwarded normally without triggering force-quit.
func TestForwardInput_TildeMidLineIsForwarded(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, forwarded := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	// 'a' moves us off line-start, then '~' should be forwarded normally.
	writeInput(t, input, []byte{'a', '~', '.'})

	// forceQuit must NOT fire.
	select {
	case <-forceQuit:
		t.Fatal("forwardInput triggered force-quit on mid-line ~. — only line-start tildes should be escape candidates")
	case <-time.After(300 * time.Millisecond):
		// Good.
	}

	expectForwarded(t, forwarded, []byte{'a', '~', '.'})
}

// TestForwardInput_TildeTildeForwardsOneLiteral verifies that \r~~ at line
// start forwards exactly one '~' byte and does not trigger force-quit.
func TestForwardInput_TildeTildeForwardsOneLiteral(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, forwarded := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	writeInput(t, input, []byte{'\r', '~', '~'})

	select {
	case <-forceQuit:
		t.Fatal("forwardInput triggered force-quit on ~~ escape")
	case <-time.After(300 * time.Millisecond):
		// Good.
	}

	expectForwarded(t, forwarded, []byte{'\r', '~'})
}

// TestForwardInput_EscapeSplitAcrossReads verifies that when the '~' ends one
// read and the '.' starts the next read, the force-quit still fires. The
// escape state machine must survive the read boundary.
func TestForwardInput_EscapeSplitAcrossReads(t *testing.T) {
	readFD, input := newInputPipe(t)
	s, forwarded := newStreamForTest(t, readFD)
	_, _, forceQuit := startForwardInput(t, s, readFD)

	// The \r is forwarded once the read holding \r~ has been processed, so
	// the '.' written after it can only arrive in a later read.
	writeInput(t, input, []byte{'\r', '~'})
	expectForwarded(t, forwarded, []byte{'\r'})
	writeInput(t, input, []byte{'.'})

	select {
	case <-forceQuit:
		// Good — escape survived the read boundary.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close forceQuit when ~. was split across two reads")
	}
}
