//go:build !windows

package pathfinder

import (
	"context"
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

// newInputPipe returns a raw pipe (read fd, write fd) not registered with Go's
// runtime poller, so forwardInput's poll(2)/read(2) operate on it exactly as
// they do on a real terminal fd. The read fd stands in for stdin; writing to
// the write fd simulates a keystroke. The caller owns closing both fds.
func newInputPipe(t *testing.T) (readFD, writeFD int) {
	t.Helper()
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	return fds[0], fds[1]
}

// TestForwardInput_CancelUnblocksBlockingRead is the regression test for the
// post-shell hang. The reader is parked waiting for input that never arrives
// (no keystroke). Cancelling the context MUST wake it promptly so inputDone
// closes — before the fix the goroutine sat in an uninterruptible read until a
// keystroke, inputDone never closed, the deferred terminal restore's join
// blocked forever, and run() never returned (so "WebAdmin tunnel still active"
// never printed).
func TestForwardInput_CancelUnblocksBlockingRead(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s := &ShellSession{fd: readFD}

	ctx, cancel := context.WithCancel(context.Background())
	inputDone := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, nil)

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
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s := &ShellSession{fd: readFD}

	ctx, cancel := context.WithCancel(context.Background())
	inputDone := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, nil)

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
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)

	s := &ShellSession{fd: readFD}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, nil)

	time.Sleep(50 * time.Millisecond)
	// Closing the write end delivers EOF (POLLHUP) to the read fd.
	syscall.Close(writeFD)

	select {
	case <-inputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not return on EOF of the terminal fd")
	}
}

// newStreamForTest returns a ShellSession wired to a real paired relay and an
// open shell stream, plus a channel that drains bytes written to that stream.
// This lets escape-sequence tests observe what was (or wasn't) forwarded.
func newStreamForTest(t *testing.T, readFD int) (*ShellSession, <-chan []byte) {
	t.Helper()
	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)
	shellStream, err := streamMgr.OpenStream("shell")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	forwarded := make(chan []byte, 64)
	// Drain the relay's binaryChan so shellStream.Write never blocks and we can
	// inspect the raw frames that were sent.
	go func() {
		for frame := range relay.binaryChan {
			forwarded <- frame
		}
	}()
	s := &ShellSession{streamManager: streamMgr, shellStream: shellStream, fd: readFD}
	return s, forwarded
}

// TestForwardInput_CtrlCIsForwarded verifies that pressing Ctrl-C (0x03) in
// raw mode is forwarded to the remote shell stream and does NOT trigger the
// force-quit channel. This is the correct behavior: Ctrl-C interrupts the
// remote foreground command; it must not kill the session.
func TestForwardInput_CtrlCIsForwarded(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, forwarded := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)
	if _, werr := syscall.Write(writeFD, []byte{0x03}); werr != nil {
		t.Fatalf("write ctrl-c: %v", werr)
	}

	// forceQuit must NOT fire.
	select {
	case <-forceQuit:
		t.Fatal("forwardInput closed forceQuit on Ctrl-C (0x03) — this kills the session instead of interrupting the remote command")
	case <-time.After(300 * time.Millisecond):
		// Good — no force-quit.
	}

	// The byte must have been forwarded.
	select {
	case <-forwarded:
		// Good — something was sent to the stream (the 0x03 byte).
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Ctrl-C (0x03) was not forwarded to the shell stream")
	}
}

// TestForwardInput_TildeDotAtLineStartForcequits verifies that \r~. triggers
// force-quit without forwarding the escape bytes.
func TestForwardInput_TildeDotAtLineStartForcequits(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, _ := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)
	// \r moves to line start, then ~. triggers force-quit.
	if _, werr := syscall.Write(writeFD, []byte{'\r', '~', '.'}); werr != nil {
		t.Fatalf("write ~.: %v", werr)
	}

	select {
	case <-forceQuit:
		// Good.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close forceQuit on \\r~. — wedged session cannot be escaped")
	}
}

// TestForwardInput_TildeDotAtSessionStartForcequits verifies that ~. at the
// very start of the session (before any \r/\n has been seen) also triggers
// force-quit, because the state machine initialises atLineStart=true.
func TestForwardInput_TildeDotAtSessionStartForcequits(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, _ := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)
	if _, werr := syscall.Write(writeFD, []byte{'~', '.'}); werr != nil {
		t.Fatalf("write ~.: %v", werr)
	}

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
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, forwarded := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)
	// 'a' moves us off line-start, then '~' should be forwarded normally.
	if _, werr := syscall.Write(writeFD, []byte{'a', '~', '.'}); werr != nil {
		t.Fatalf("write: %v", werr)
	}

	// forceQuit must NOT fire.
	select {
	case <-forceQuit:
		t.Fatal("forwardInput triggered force-quit on mid-line ~. — only line-start tildes should be escape candidates")
	case <-time.After(300 * time.Millisecond):
		// Good.
	}

	// Something should have been forwarded (at least 'a' and '~').
	select {
	case <-forwarded:
		// Good.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("nothing was forwarded for mid-line input")
	}
}

// TestForwardInput_TildeTildeForwardsOneLiteral verifies that \r~~ at line
// start forwards exactly one '~' byte and does not trigger force-quit.
func TestForwardInput_TildeTildeForwardsOneLiteral(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, forwarded := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)
	if _, werr := syscall.Write(writeFD, []byte{'\r', '~', '~'}); werr != nil {
		t.Fatalf("write ~~: %v", werr)
	}

	select {
	case <-forceQuit:
		t.Fatal("forwardInput triggered force-quit on ~~ escape")
	case <-time.After(300 * time.Millisecond):
		// Good.
	}

	select {
	case <-forwarded:
		// Good — the literal ~ (and possibly the \r) was forwarded.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("~~ did not forward a literal ~")
	}
}

// TestForwardInput_EscapeSplitAcrossReads verifies that when the '~' ends one
// read and the '.' starts the next read, the force-quit still fires. The
// escape state machine must survive the read boundary.
func TestForwardInput_EscapeSplitAcrossReads(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	s, _ := newStreamForTest(t, readFD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	forceQuit := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, forceQuit)

	time.Sleep(50 * time.Millisecond)

	// Write \r~ in one syscall (tilde ends the read buffer).
	if _, werr := syscall.Write(writeFD, []byte{'\r', '~'}); werr != nil {
		t.Fatalf("write \\r~: %v", werr)
	}
	// Small pause so poll(2) returns on the first write before the second arrives.
	time.Sleep(20 * time.Millisecond)
	// Write . in a separate syscall (next read).
	if _, werr := syscall.Write(writeFD, []byte{'.'}); werr != nil {
		t.Fatalf("write .: %v", werr)
	}

	select {
	case <-forceQuit:
		// Good — escape survived the read boundary.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close forceQuit when ~. was split across two reads")
	}
}
