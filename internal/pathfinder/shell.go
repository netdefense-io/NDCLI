//go:build !windows

package pathfinder

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// retryWriter wraps a writer and retries transient EAGAIN/EWOULDBLOCK.
//
// Historically stdin was flipped non-blocking during the session, and because
// stdin/stdout share one tty open-file-description, stdout writes could return
// EAGAIN. forwardInput no longer touches the non-blocking flag, so this should
// effectively never trigger now — but the retry is kept as defense and is
// BOUNDED: an unbounded EAGAIN loop here would wedge the output goroutine, so
// close(done) would never fire and run() would block forever on <-done with no
// "WebAdmin" line printed (the exact hang). After the bound is exhausted it
// returns the error, which ends io.Copy and lets teardown proceed.
type retryWriter struct {
	w io.Writer
}

func (r *retryWriter) Write(p []byte) (int, error) {
	written := 0
	const maxRetries = 500 // ~5s at 10ms; far beyond any real transient stall
	retries := 0
	for written < len(p) {
		n, err := r.w.Write(p[written:])
		written += n
		if err != nil {
			// Check for EAGAIN/EWOULDBLOCK
			if perr, ok := err.(*os.PathError); ok {
				if perr.Err == syscall.EAGAIN || perr.Err == syscall.EWOULDBLOCK {
					retries++
					if retries > maxRetries {
						debugLog("retryWriter: giving up after %d EAGAIN retries", retries)
						return written, err
					}
					// Resource temporarily unavailable - wait and retry
					time.Sleep(10 * time.Millisecond)
					continue
				}
			}
			return written, err
		}
		retries = 0
	}
	return written, nil
}

const (
	// Control message types (must match device agent)
	CtlMsgResize byte = 0x01
	CtlMsgClose  byte = 0xFF
)

// shellProbeTimeout bounds how long StartShellSession waits for the first
// byte of terminal output before deciding the shell was refused. The agent
// either streams a prompt almost immediately or (on a read-only session)
// closes the shell stream with zero bytes; this window only needs to absorb
// relay latency.
const shellProbeTimeout = 3 * time.Second

// ShellSession manages an interactive shell session over a stream
type ShellSession struct {
	streamManager *StreamManager
	shellStream   *Stream
	ctlStream     *Stream
	oldState      *term.State
	fd            int
}

// StartShellSession starts an interactive shell session. The returned
// ShellOutcome reports whether the remote ever delivered any terminal
// output. When Refused is true the shell stream was closed by the agent
// before producing any data (the read-only enforcement path), and the
// caller should fall back to webadmin-only keep-alive instead of treating
// the empty session as a normal terminal exit.
func StartShellSession(streamManager *StreamManager) (ShellOutcome, error) {
	session := &ShellSession{
		streamManager: streamManager,
		fd:            int(os.Stdin.Fd()),
	}
	return session.run()
}

func (s *ShellSession) run() (ShellOutcome, error) {
	// Open shell stream for PTY I/O
	shellStream, err := s.streamManager.OpenStream("shell")
	if err != nil {
		return ShellOutcome{}, err
	}
	s.shellStream = shellStream

	// Open control stream for resize messages
	ctlStream, err := s.streamManager.OpenStream("shell-ctl")
	if err != nil {
		shellStream.Close()
		return ShellOutcome{}, err
	}
	s.ctlStream = ctlStream

	// Probe for the first byte BEFORE touching the terminal. On a read-only
	// session the agent refuses the shell service and closes the stream with
	// zero bytes; detecting that here lets us bail out as Refused without ever
	// flipping the terminal into raw mode (no visible flicker) so the caller
	// can fall back to webadmin-only keep-alive.
	firstChunk := make([]byte, 4096)
	type readResult struct {
		n   int
		err error
	}
	probeCh := make(chan readResult, 1)
	go func() {
		n, rerr := s.shellStream.Read(firstChunk)
		probeCh <- readResult{n: n, err: rerr}
	}()

	var first []byte
	select {
	case res := <-probeCh:
		if res.n == 0 || res.err != nil {
			// Stream closed (or errored) before any output: refused.
			s.shellStream.Close()
			s.ctlStream.Close()
			return ShellOutcome{Refused: true}, nil
		}
		first = append(first, firstChunk[:res.n]...)
	case <-time.After(shellProbeTimeout):
		// No data within the probe window. Treat as refused rather than
		// hang the UI; the caller falls back to webadmin-only keep-alive.
		s.shellStream.Close()
		s.ctlStream.Close()
		return ShellOutcome{Refused: true}, nil
	}

	// Put terminal in raw mode so Ctrl+C sends 0x03 instead of killing the client
	oldState, err := term.MakeRaw(s.fd)
	if err != nil {
		shellStream.Close()
		ctlStream.Close()
		return ShellOutcome{}, err
	}
	s.oldState = oldState

	// Context for cancellation. The deferred restore below owns the single
	// cancel() call so that cancellation is always sequenced before the
	// reader-goroutine join (no separate defer cancel(), which would run after
	// restore and serve no purpose).
	ctx, cancel := context.WithCancel(context.Background())

	// inputDone is closed once the stdin reader goroutine has fully stopped
	// touching the terminal fd. The terminal restore below MUST wait on it:
	// the reader toggles the fd between non-blocking and blocking mode on every
	// iteration, so restoring (and clearing non-blocking mode) while it is still
	// running races — leaving stdin non-blocking and/or in raw mode after the
	// session ends. When that happens Ctrl-C is delivered as a literal 0x03
	// byte instead of generating SIGINT, and the subsequent webadmin keep-alive
	// wait hangs unkillably. Joining the reader before restore makes the
	// teardown deterministic.
	inputDone := make(chan struct{})

	// Ensure terminal is restored on exit, but only after the stdin reader has
	// stopped mutating the fd. Cancel first to wake the reader, wait for it,
	// then force the fd back to blocking mode and restore the saved termios.
	// A watchdog dumps goroutine stacks (under NDCLI_DEBUG) if any step of this
	// teardown stalls — that is where the live hang lives.
	defer func() {
		stopWatch := watchdog(3*time.Second, "shell run() teardown")
		defer stopWatch()
		stage("run(): cancel")
		cancel()
		stage("run(): join input reader (waiting on inputDone)")
		<-inputDone
		stage("run(): input reader joined")
		// Defensive: guarantee blocking mode regardless of where the reader
		// stopped, so the restored terminal behaves normally for waitForQuit.
		syscall.SetNonblock(s.fd, false)
		term.Restore(s.fd, s.oldState)
		stage("run(): terminal restored")
	}()

	// Send initial terminal size
	s.sendResize()

	// Watch for SIGWINCH (terminal resize)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-sigCh:
				s.sendResize()
			case <-ctx.Done():
				return
			}
		}
	}()
	defer signal.Stop(sigCh)

	// Channel to signal completion
	done := make(chan struct{})

	// Remote output → local terminal
	// Use retryWriter to handle EAGAIN when stdout is affected by stdin's non-blocking mode
	go func() {
		stdout := &retryWriter{w: os.Stdout}
		// Flush the byte(s) consumed by the refusal probe before resuming the copy.
		if len(first) > 0 {
			stdout.Write(first)
		}
		n, err := io.Copy(stdout, s.shellStream)
		// Remote closed, cancel everything
		debugLog("io.Copy exited: bytes=%d, err=%v", n, err)
		cancel()
		close(done)
	}()

	// interrupt is closed by forwardInput when the user presses Ctrl-C (0x03).
	// In raw mode Ctrl-C is delivered as a literal byte, not SIGINT, so it is
	// the only reliable in-band "I want out" signal — and it is what the
	// operator was pressing to no effect during the hang.
	interrupt := make(chan struct{})

	// Local input → remote PTY. forwardInput parks in poll(2) on the terminal
	// fd plus a self-pipe, so cancellation always wakes it promptly (it never
	// sits in an uninterruptible blocking read) and closes inputDone — the join
	// signal the deferred terminal restore waits on.
	go s.forwardInput(ctx, s.fd, inputDone, interrupt)

	// End the session robustly. The clean ending is `done` (io.Copy returned
	// because the remote closed the shell stream). But io.Copy only returns on
	// shell-stream EOF, which depends entirely on the AGENT sending a CLOSE
	// frame — and in the "Logout immediately" race that remote close can fail to
	// arrive, leaving io.Copy parked in Stream.Read forever. Proven by a live
	// goroutine dump: main blocked on <-done, the output goroutine blocked in
	// Stream.Read on <-s.closed, terminal still raw, "WebAdmin" never printed,
	// and Ctrl-C did nothing.
	//
	// So a closer goroutine force-closes the shell stream on any independent
	// end-of-session signal, which closes s.closed, unblocks Stream.Read, and
	// makes io.Copy return. run() then always returns via the single `done`
	// anchor. The triggers, in order of which fires in the real failure mode:
	//
	//   - ctlStream.Closed(): the agent closes BOTH the shell and shell-ctl
	//     streams on shell exit. When the shell-stream CLOSE is the one that
	//     races/loses, the shell-ctl CLOSE still arrives — so this ends the
	//     session automatically on Logout WITHOUT needing the user to do
	//     anything. This is the primary no-remote-shell-CLOSE trigger.
	//   - interrupt: the user pressed Ctrl-C (0x03). Always-available manual
	//     escape, even if every remote CLOSE is lost.
	//   - inputDone: local stdin reached EOF.
	//   - ctx.Done(): parent/relay cancellation.
	go func() {
		select {
		case <-done: // remote shell-stream close already ended it; nothing to force
		case <-s.ctlStream.Closed():
			stage("run(): shell-ctl stream closed by peer — forcing shell stream close")
			s.shellStream.Close()
		case <-interrupt:
			stage("run(): Ctrl-C — forcing shell stream close")
			s.shellStream.Close()
		case <-inputDone:
			stage("run(): input ended — forcing shell stream close")
			s.shellStream.Close()
		case <-ctx.Done():
			stage("run(): context cancelled — forcing shell stream close")
			s.shellStream.Close()
		}
	}()

	// Wait for the output goroutine to finish. Guaranteed to fire: either the
	// remote closed the shell stream, or the closer goroutine above did.
	stage("run(): waiting for output goroutine (done)")
	<-done
	stage("run(): output goroutine finished")

	// Clean up streams (idempotent if already closed by the closer goroutine).
	stage("run(): closing shell stream")
	s.shellStream.Close()
	stage("run(): closing ctl stream")
	s.ctlStream.Close()
	stage("run(): streams closed, returning")

	// We received terminal output, so this was a real (now-ended) session,
	// not a refusal.
	return ShellOutcome{Refused: false}, nil
}

// forwardInput copies bytes from the terminal fd to the remote shell stream
// until ctx is cancelled or the fd reaches EOF/error.
//
// It parks in poll(2) on two descriptors: the terminal fd and the read end of
// a self-pipe. A watcher writes one byte to the self-pipe when ctx is
// cancelled, which always wakes the poll — so the goroutine never sits in an
// uninterruptible blocking read waiting for a keystroke that may never come.
// That blocking-read-with-no-input case was the post-shell hang: the deferred
// terminal restore joins on inputDone, so a reader stuck in read(2) wedged the
// whole teardown and run() never returned.
//
// inputDone is closed on every return path, and the fd is left in blocking mode
// (its normal state), so the restore that joins on inputDone sees a clean fd.
func (s *ShellSession) forwardInput(ctx context.Context, fd int, inputDone chan struct{}, interrupt chan struct{}) {
	defer close(inputDone)

	// Close interrupt at most once, when we see Ctrl-C (0x03) in the raw input.
	var interruptOnce sync.Once
	signalInterrupt := func() {
		if interrupt != nil {
			interruptOnce.Do(func() { close(interrupt) })
		}
	}

	// Self-pipe used purely to interrupt poll(2) on cancel.
	var cancelPipe [2]int
	if err := unix.Pipe(cancelPipe[:]); err != nil {
		debugLog("forwardInput: pipe: %v", err)
		// Without a wake mechanism we must not risk a blocking read; bail out.
		return
	}
	cancelR, cancelW := cancelPipe[0], cancelPipe[1]
	defer unix.Close(cancelR)
	defer unix.Close(cancelW)

	// Wake the poll when ctx is cancelled. The watcher must also unblock when
	// forwardInput returns for a non-cancel reason (fd EOF/error): otherwise it
	// would sit on ctx.Done() forever and the join below would deadlock — which
	// is exactly the teardown hang this whole function exists to avoid.
	returning := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			unix.Write(cancelW, []byte{0})
		case <-returning:
		}
	}()
	// Signal the watcher to exit, then join it, before the self-pipe is closed.
	defer func() {
		close(returning)
		<-watcherDone
	}()

	// Deliberately DO NOT change the fd's non-blocking flag. We only read after
	// poll(2) reports the fd readable and we are the sole reader, so unix.Read
	// returns immediately without blocking — no non-blocking flag needed. This
	// matters because stdin and stdout share one open-file-description on a tty:
	// flipping stdin non-blocking also makes stdout writes return EAGAIN, which
	// forced the output copy through an unbounded retry loop. Leaving the fd in
	// its normal blocking state keeps stdout writes normal.

	buf := make([]byte, 1024)
	for {
		fds := []unix.PollFd{
			{Fd: int32(fd), Events: unix.POLLIN},
			{Fd: int32(cancelR), Events: unix.POLLIN},
		}
		if _, err := unix.Poll(fds, -1); err != nil {
			if err == unix.EINTR {
				continue
			}
			debugLog("forwardInput: poll: %v", err)
			return
		}

		// Cancelled: the watcher wrote to the self-pipe.
		if fds[1].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			return
		}

		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
			continue
		}

		nr, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				continue
			}
			return
		}
		if nr == 0 {
			// EOF on the terminal fd.
			return
		}

		// Surface a Ctrl-C (ETX, 0x03) as an interrupt so run() can end a
		// wedged session even if the remote never sent a CLOSE frame. We still
		// forward the byte to the shell (a live remote may want to handle it),
		// but the interrupt guarantees the user always has a way out — the
		// behavior the operator expected when "Ctrl-C did nothing".
		if containsByte(buf[:nr], 0x03) {
			signalInterrupt()
		}

		// Don't push input to a stream we're tearing down.
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, werr := s.shellStream.Write(buf[:nr]); werr != nil {
			debugLog("shellStream.Write error: %v", werr)
			return
		}
	}
}

// containsByte reports whether b appears in p.
func containsByte(p []byte, b byte) bool {
	for _, c := range p {
		if c == b {
			return true
		}
	}
	return false
}

func (s *ShellSession) sendResize() {
	cols, rows, err := term.GetSize(s.fd)
	if err != nil {
		return
	}
	msg := []byte{
		CtlMsgResize,
		byte(rows >> 8), byte(rows & 0xFF),
		byte(cols >> 8), byte(cols & 0xFF),
	}
	s.ctlStream.Write(msg)
}
