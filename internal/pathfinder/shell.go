//go:build !windows

package pathfinder

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

// retryWriter wraps a writer and retries on EAGAIN/EWOULDBLOCK.
// This is needed because setting stdin to non-blocking mode on a TTY
// also affects stdout (they share the same terminal device).
type retryWriter struct {
	w io.Writer
}

func (r *retryWriter) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		n, err := r.w.Write(p[written:])
		written += n
		if err != nil {
			// Check for EAGAIN/EWOULDBLOCK
			if perr, ok := err.(*os.PathError); ok {
				if perr.Err == syscall.EAGAIN || perr.Err == syscall.EWOULDBLOCK {
					// Resource temporarily unavailable - wait and retry
					time.Sleep(10 * time.Millisecond)
					continue
				}
			}
			return written, err
		}
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

	// Ensure terminal is restored on exit
	defer func() {
		term.Restore(s.fd, s.oldState)
	}()

	// Context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Local input → remote PTY (with cancellation check)
	go func() {
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Set stdin to non-blocking temporarily to allow checking ctx
			if err := syscall.SetNonblock(s.fd, true); err != nil {
				return
			}

			nr, err := os.Stdin.Read(buf)

			// Restore blocking mode
			syscall.SetNonblock(s.fd, false)

			if err != nil {
				// EAGAIN means no data available (non-blocking)
				if perr, ok := err.(*os.PathError); ok {
					if perr.Err == syscall.EAGAIN || perr.Err == syscall.EWOULDBLOCK {
						// No data, wait a bit and retry
						select {
						case <-ctx.Done():
							return
						case <-time.After(50 * time.Millisecond):
							continue
						}
					}
				}
				return
			}

			if nr > 0 {
				_, err = s.shellStream.Write(buf[:nr])
				if err != nil {
					debugLog("shellStream.Write error: %v", err)
					return
				}
			}
		}
	}()

	// Wait for remote to close
	<-done

	// Clean up streams
	s.shellStream.Close()
	s.ctlStream.Close()

	// We received terminal output, so this was a real (now-ended) session,
	// not a refusal.
	return ShellOutcome{Refused: false}, nil
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
