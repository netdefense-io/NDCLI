package pathfinder

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// ErrReadOnlySession carries the clear, self-explanatory message shown when a
// read-only caller is denied terminal/console exec access. The authoritative
// read-only decision is made server-side: NDManager derives read_only from the
// caller's role and returns it in the connect-status response, and the MCP
// console_open handler fails early with this message when that flag is true.
// (The agent's server-side chokepoint remains the security guarantee; this is
// purely the clarity layer.)
var ErrReadOnlySession = &PathfinderError{
	Message: "Terminal/console access is disabled for this session: your role on " +
		"this organization is read-only (RO). Running commands on devices requires " +
		"read-write (RW) access. Read-only WebAdmin browsing is still available via " +
		"the device connect / webadmin flow.",
}

// execRequest is the JSON object sent to the agent on the exec stream.
type execRequest struct {
	ID             string `json:"id"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// execResponse is a single JSON object received from the agent on the exec stream.
// The agent uses omitempty, so missing fields are their zero values.
type execResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // "stdout" | "stderr" | "result"
	Data      string `json:"data"`      // base64; absent on result frames
	ExitCode  int    `json:"exit_code"` // present on result frames (0 = success, 124 = timeout, etc.)
	Truncated bool   `json:"truncated"` // absent when false
}

// frameMsg is a decoded frame delivered from the persistent reader goroutine.
type frameMsg struct {
	resp execResponse
	err  error
}

// execStreamIface is the minimal interface used by readerLoop and Exec.
// In production, *Stream satisfies this interface. In tests, a *fakeStream
// (same package) also satisfies it, enabling full ExecHandle testing without
// a live relay connection.
type execStreamIface interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
	Close() error
}

// ExecHandle is a live exec-stream handle to a paired agent session.
// It can run many commands sequentially. The caller is responsible for
// acquiring a per-session lock before each Exec call to serialise commands.
// Close tears down the exec stream and the relay connection.
//
// A single persistent reader goroutine (started in ConnectExec, stopped in
// Close) owns all reads from execStream. Exec() consumes from the shared
// frameCh; it never spawns its own reader goroutine, eliminating the frame-
// theft race that occurred when per-call goroutines outlived their Exec() call.
type ExecHandle struct {
	streamMgr  *StreamManager
	execStream *Stream
	relay      *RelayClient

	// execStreamOverride is non-nil only in tests; readerLoop uses it instead
	// of execStream when set. This lets tests inject a fakeStream without
	// touching the production *Stream type.
	execStreamOverride execStreamIface

	// frameCh carries decoded frames from the persistent reader goroutine.
	// Buffered to absorb a burst of frames between Exec() drain iterations.
	frameCh chan frameMsg

	// closedCh is closed by Close() so that any Exec() in progress can detect
	// the handle has been torn down.
	closedCh  chan struct{}
	closeOnce sync.Once
}

// ConnectExec establishes a Pathfinder relay connection, waits for pairing,
// opens the exec stream, and returns an ExecHandle without entering an
// interactive loop. The persistent reader goroutine is started here and
// stopped only when Close() is called. The caller must call Close() when done.
func (c *Client) ConnectExec() (*ExecHandle, error) {
	wsURL := c.buildWebSocketURL()

	c.progress("Connecting to relay server...")

	relay := NewRelayClient(wsURL, c.sessionID, c.sslVerify)
	if err := relay.Connect(); err != nil {
		return nil, &PathfinderError{Message: "failed to connect to relay: " + err.Error()}
	}

	if err := relay.WaitForRegistration(10 * time.Second); err != nil {
		relay.Close()
		return nil, &PathfinderError{Message: "registration failed: " + err.Error()}
	}

	c.progress("Waiting for device...")

	if err := relay.WaitForPairing(120 * time.Second); err != nil {
		relay.Close()
		return nil, &PathfinderError{Message: "pairing failed: " + err.Error()}
	}

	c.progress("Opening exec stream...")

	streamMgr := NewStreamManager(relay)
	relay.SetStreamManager(streamMgr)

	execStream, err := streamMgr.OpenStream("exec")
	if err != nil {
		relay.Close()
		return nil, &PathfinderError{Message: "failed to open exec stream: " + err.Error()}
	}

	h := &ExecHandle{
		streamMgr:  streamMgr,
		execStream: execStream,
		relay:      relay,
		frameCh:    make(chan frameMsg, 256),
		closedCh:   make(chan struct{}),
	}

	// Start the single persistent reader goroutine. It exits when the exec
	// stream returns an error (EOF on Close, relay drop, etc.) or when
	// closedCh is closed, whichever comes first.
	//
	// Read-only enforcement is NOT done here: NDManager returns the
	// authoritative read_only flag in the connect-status response, and the
	// console_open handler fails early on it before this point is reached. The
	// agent's server-side chokepoint remains the security backstop.
	go h.readerLoop()

	return h, nil
}

// activeStream returns the active exec stream: the test override when set,
// otherwise the production *Stream. It captures the value once so that
// callers hold a stable reference even if Close() nilifies the fields.
func (h *ExecHandle) activeStream() execStreamIface {
	if h.execStreamOverride != nil {
		return h.execStreamOverride
	}
	return h.execStream
}

// readerLoop is the single goroutine that owns all reads from execStream.
// It decodes each FrameTypeData payload as a JSON execResponse and sends it
// to frameCh for consumption by Exec(). It exits on any read error or when
// the handle is closed.
func (h *ExecHandle) readerLoop() {
	// Capture the stream once at startup. Close() may nilify the handle fields
	// later, but this goroutine holds its own reference and is unaffected.
	s := h.activeStream()
	buf := make([]byte, 64*1024)
	for {
		n, readErr := s.Read(buf)
		if readErr != nil {
			// Stream closed (EOF from Close() or relay drop). Deliver the
			// error to any waiting Exec() call, then exit.
			if readErr != io.EOF {
				select {
				case h.frameCh <- frameMsg{err: readErr}:
				case <-h.closedCh:
				}
			}
			return
		}
		if n == 0 {
			continue
		}

		// Decode the frame payload as a JSON execResponse.
		var resp execResponse
		if jsonErr := json.Unmarshal(buf[:n], &resp); jsonErr != nil {
			select {
			case h.frameCh <- frameMsg{err: fmt.Errorf("malformed exec response JSON: %w", jsonErr)}:
			case <-h.closedCh:
				return
			}
			continue
		}

		select {
		case h.frameCh <- frameMsg{resp: resp}:
		case <-h.closedCh:
			return
		}
	}
}

// Exec sends a single command to the agent and collects all response frames
// until the matching result frame arrives.
//
// The wire contract (agent side):
//   - Request: { "id": "<uuid>", "command": "...", "timeout_seconds": N }
//   - Response frames (in order): zero or more stdout/stderr frames, then exactly one result frame.
//   - Data fields are standard base64 (RFC 4648 StdEncoding).
//   - omitempty on agent side: treat missing fields as zero values.
//   - exit_code 124 = timeout; 128+signal = signal kill; -1 = pre-exec failure.
//   - Malformed request: agent replies with id:"" result (exit_code -1).
//
// Exec must be called serially (never concurrently for the same session). The
// MCP layer enforces this with consoleSession.mu.
func (h *ExecHandle) Exec(ctx context.Context, command string, timeout time.Duration) (stdout, stderr string, exitCode int, truncated bool, err error) {
	// Reject calls on a closed handle immediately.
	select {
	case <-h.closedCh:
		return "", "", -1, false, fmt.Errorf("exec handle is closed")
	default:
	}

	reqID, err := newUUID()
	if err != nil {
		return "", "", -1, false, fmt.Errorf("generate request id: %w", err)
	}

	timeoutSecs := int(timeout.Seconds())
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}

	reqBytes, err := json.Marshal(execRequest{
		ID:             reqID,
		Command:        command,
		TimeoutSeconds: timeoutSecs,
	})
	if err != nil {
		return "", "", -1, false, fmt.Errorf("marshal exec request: %w", err)
	}

	// Send the request as a single FrameTypeData payload on the exec stream.
	if _, writeErr := h.activeStream().Write(reqBytes); writeErr != nil {
		return "", "", -1, false, fmt.Errorf("write exec request: %w", writeErr)
	}

	// Drain frameCh until the result frame for this request arrives.
	// The persistent reader goroutine feeds all frames; Exec merely consumes.
	var stdoutBuf, stderrBuf []byte
	for {
		select {
		case <-ctx.Done():
			return "", "", -1, false, ctx.Err()

		case <-h.closedCh:
			return "", "", -1, false, fmt.Errorf("exec handle closed while waiting for result")

		case fm := <-h.frameCh:
			if fm.err != nil {
				return "", "", -1, false, fmt.Errorf("exec stream error: %w", fm.err)
			}
			resp := fm.resp

			// Malformed-request case: agent rejected our request, replies with id:"".
			if resp.Type == "result" && resp.ID == "" {
				decodedStderr, _ := base64.StdEncoding.DecodeString(resp.Data)
				stderrBuf = append(stderrBuf, decodedStderr...)
				return "", string(stderrBuf), resp.ExitCode, resp.Truncated,
					fmt.Errorf("agent rejected request (exit_code=%d)", resp.ExitCode)
			}

			// ID mismatch guard — protects against stream desync.
			if resp.ID != reqID {
				return "", "", -1, false, fmt.Errorf(
					"exec stream desync: received response id %q, expected %q", resp.ID, reqID)
			}

			switch resp.Type {
			case "stdout":
				decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
				if decErr != nil {
					return "", "", -1, false, fmt.Errorf("base64 decode stdout: %w", decErr)
				}
				stdoutBuf = append(stdoutBuf, decoded...)

			case "stderr":
				decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
				if decErr != nil {
					return "", "", -1, false, fmt.Errorf("base64 decode stderr: %w", decErr)
				}
				stderrBuf = append(stderrBuf, decoded...)

			case "result":
				// result frame: no data field, just exit_code and optional truncated.
				return string(stdoutBuf), string(stderrBuf), resp.ExitCode, resp.Truncated, nil

			default:
				debugLog("exec: skipping unexpected response type %q for id %s", resp.Type, resp.ID)
			}
		}
	}
}

// Close sends FrameTypeClose on the exec stream (which triggers the agent's
// OnAllStreamsClosed → session teardown), stops the persistent reader
// goroutine, and closes the relay connection. After Close, any in-progress
// or subsequent Exec() call returns an error immediately. Close is idempotent.
func (h *ExecHandle) Close() {
	h.closeOnce.Do(func() {
		// Signal the reader goroutine and any waiting Exec() to stop first.
		// readerLoop captured the stream at startup so it is unaffected by
		// the subsequent stream/relay closes. Guard against zero-value handles
		// (closedCh == nil) that may be constructed in tests.
		if h.closedCh != nil {
			close(h.closedCh)
		}

		// Close whichever stream is active (production or test override).
		// This unblocks readerLoop's pending Read() call (returns io.EOF).
		if h.execStreamOverride != nil {
			h.execStreamOverride.Close()
		} else if h.execStream != nil {
			h.execStream.Close()
		}

		if h.relay != nil {
			h.relay.Close()
		}
	})
}

// newUUID generates a random UUID v4 string without external dependencies.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// Set version (4) and variant bits (RFC 4122).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
