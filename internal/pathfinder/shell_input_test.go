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

// TestForwardInput_CtrlCSignalsInterrupt verifies that a Ctrl-C (0x03) byte in
// the input closes the interrupt channel, which run() uses to force-close a
// wedged shell stream when the remote never sent a CLOSE frame. This is the
// fix for the live "Logout immediately → hang, Ctrl-C does nothing" deadlock.
func TestForwardInput_CtrlCSignalsInterrupt(t *testing.T) {
	readFD, writeFD := newInputPipe(t)
	defer syscall.Close(readFD)
	defer syscall.Close(writeFD)

	// A paired relay + open shell stream so forwardInput can forward the byte
	// without panicking on a nil stream.
	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)
	shellStream, err := streamMgr.OpenStream("shell")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	s := &ShellSession{streamManager: streamMgr, shellStream: shellStream, fd: readFD}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputDone := make(chan struct{})
	interrupt := make(chan struct{})
	go s.forwardInput(ctx, readFD, inputDone, interrupt)

	time.Sleep(50 * time.Millisecond)
	// Simulate the user pressing Ctrl-C.
	if _, werr := syscall.Write(writeFD, []byte{0x03}); werr != nil {
		t.Fatalf("write ctrl-c: %v", werr)
	}

	select {
	case <-interrupt:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInput did not close interrupt on Ctrl-C (0x03) — a wedged session could not be ended by the user")
	}
}
