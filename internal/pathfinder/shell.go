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

// ShellSession manages an interactive shell session over a stream
type ShellSession struct {
	streamManager *StreamManager
	shellStream   *Stream
	ctlStream     *Stream
	oldState      *term.State
	fd            int
}

// StartShellSession starts an interactive shell session
func StartShellSession(streamManager *StreamManager) error {
	session := &ShellSession{
		streamManager: streamManager,
		fd:            int(os.Stdin.Fd()),
	}
	return session.run()
}

func (s *ShellSession) run() error {
	// Open shell stream for PTY I/O
	shellStream, err := s.streamManager.OpenStream("shell")
	if err != nil {
		return err
	}
	s.shellStream = shellStream

	// Open control stream for resize messages
	ctlStream, err := s.streamManager.OpenStream("shell-ctl")
	if err != nil {
		shellStream.Close()
		return err
	}
	s.ctlStream = ctlStream

	// Put terminal in raw mode so Ctrl+C sends 0x03 instead of killing the client
	oldState, err := term.MakeRaw(s.fd)
	if err != nil {
		shellStream.Close()
		ctlStream.Close()
		return err
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

	return nil
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
