package pathfinder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fake stream infrastructure
// ---------------------------------------------------------------------------

// fakeStream is a synchronous in-memory bidirectional channel pair that mimics
// the Stream API. Write on the "client" side enqueues bytes for the agent to
// receive via its own Read, and the agent's Write feeds the client's Read.
type fakeStream struct {
	// agentRx receives bytes sent by the client (exec requests).
	agentRx chan []byte
	// clientRx receives bytes sent by the agent (exec responses).
	clientRx chan []byte
	closed   chan struct{}
}

func newFakeStreamPair() (client *fakeStream, agent *fakeStream) {
	agentRx := make(chan []byte, 1024)
	clientRx := make(chan []byte, 1024)
	closed := make(chan struct{})
	client = &fakeStream{agentRx: agentRx, clientRx: clientRx, closed: closed}
	agent = &fakeStream{agentRx: clientRx, clientRx: agentRx, closed: closed}
	return
}

func (f *fakeStream) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case f.agentRx <- cp:
		return len(p), nil
	case <-f.closed:
		return 0, io.ErrClosedPipe
	}
}

func (f *fakeStream) Read(p []byte) (int, error) {
	select {
	case data := <-f.clientRx:
		n := copy(p, data)
		return n, nil
	case <-f.closed:
		return 0, io.EOF
	}
}

func (f *fakeStream) Close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ExecHandle test constructor
// ---------------------------------------------------------------------------

// newTestExecHandleViaOverride builds a real ExecHandle with the execStream
// override set to the given fakeStream, then starts the persistent reader
// goroutine. The returned handle behaves exactly like one returned by
// ConnectExec(), minus the real relay/relay-pairing scaffolding.
//
// execStreamIface is defined in exec.go (same package); *fakeStream satisfies
// it because it implements Write, Read, and Close.
func newTestExecHandleViaOverride(fs *fakeStream) *ExecHandle {
	h := &ExecHandle{
		frameCh:            make(chan frameMsg, 256),
		closedCh:           make(chan struct{}),
		execStreamOverride: fs,
	}
	go h.readerLoop()
	return h
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// agentSend writes a JSON response frame to the agent-side stream.
func agentSend(t *testing.T, agent *fakeStream, resp execResponse) {
	t.Helper()
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("agentSend marshal: %v", err)
	}
	agent.agentRx <- data
}

// agentReadRequest reads one exec request from the agent-side stream.
func agentReadRequest(t *testing.T, agent *fakeStream) execRequest {
	t.Helper()
	buf := make([]byte, 8192)
	n, err := agent.Read(buf)
	if err != nil {
		t.Fatalf("agentReadRequest: %v", err)
	}
	var req execRequest
	if err := json.Unmarshal(buf[:n], &req); err != nil {
		t.Fatalf("agentReadRequest unmarshal: %v", err)
	}
	return req
}

// b64 encodes a string to standard base64 (RFC 4648).
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// ---------------------------------------------------------------------------
// execOverStreamer — protocol logic test harness (independent of ExecHandle)
// ---------------------------------------------------------------------------

// execStreamer is the minimal interface required by execOverStreamer.
type execStreamer interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
}

// streamReadResult carries one read result from the goroutine.
type streamReadResult struct {
	data []byte
	err  error
}

// execOverStreamer mirrors the Exec framing logic but accepts a generic
// execStreamer instead of an ExecHandle. Used for protocol unit tests that
// exercise framing / base64 / exit-code semantics without needing the full
// ExecHandle machinery.
func execOverStreamer(ctx context.Context, s execStreamer, command string, timeout time.Duration) (stdout, stderr string, exitCode int, truncated bool, err error) {
	reqID, err := newUUID()
	if err != nil {
		return "", "", -1, false, err
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
		return "", "", -1, false, err
	}

	if _, writeErr := s.Write(reqBytes); writeErr != nil {
		return "", "", -1, false, writeErr
	}

	// Use a goroutine+channel so we can select on ctx.Done() simultaneously.
	readCh := make(chan streamReadResult, 1)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, readErr := s.Read(buf)
			if readErr != nil {
				readCh <- streamReadResult{err: readErr}
				return
			}
			if n == 0 {
				continue
			}
			cp := make([]byte, n)
			copy(cp, buf[:n])
			readCh <- streamReadResult{data: cp}
		}
	}()

	var stdoutBuf, stderrBuf []byte
	for {
		select {
		case <-ctx.Done():
			return "", "", -1, false, ctx.Err()

		case rr := <-readCh:
			if rr.err != nil {
				return "", "", -1, false, rr.err
			}

			var resp execResponse
			if jsonErr := json.Unmarshal(rr.data, &resp); jsonErr != nil {
				return "", "", -1, false, jsonErr
			}

			if resp.Type == "result" && resp.ID == "" {
				decodedStderr, _ := base64.StdEncoding.DecodeString(resp.Data)
				stderrBuf = append(stderrBuf, decodedStderr...)
				return "", string(stderrBuf), resp.ExitCode, resp.Truncated, nil
			}

			if resp.ID != reqID {
				return "", "", -1, false, &PathfinderError{
					Message: "exec stream desync: received id " + resp.ID + ", expected " + reqID,
				}
			}

			switch resp.Type {
			case "stdout":
				decoded, _ := base64.StdEncoding.DecodeString(resp.Data)
				stdoutBuf = append(stdoutBuf, decoded...)
			case "stderr":
				decoded, _ := base64.StdEncoding.DecodeString(resp.Data)
				stderrBuf = append(stderrBuf, decoded...)
			case "result":
				return string(stdoutBuf), string(stderrBuf), resp.ExitCode, resp.Truncated, nil
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Protocol framing tests (via execOverStreamer)
// ---------------------------------------------------------------------------

func TestExec_HappyPath(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("hello world\n")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result"})
	}()

	stdout, stderr, code, trunc, err := execOverStreamer(
		context.Background(), clientStream, "echo hello world", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", stdout, "hello world\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if code != 0 {
		t.Errorf("exit_code = %d, want 0", code)
	}
	if trunc {
		t.Errorf("truncated = true, want false")
	}
}

func TestExec_MultiFrameStdout(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("chunk1")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("chunk2")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("chunk3")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result"})
	}()

	stdout, _, code, _, err := execOverStreamer(
		context.Background(), clientStream, "cat bigfile", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "chunk1chunk2chunk3"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if code != 0 {
		t.Errorf("exit_code = %d, want 0", code)
	}
}

func TestExec_StdoutAndStderr(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("out1")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stderr", Data: b64("err1")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("out2")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result", ExitCode: 1})
	}()

	stdout, stderr, code, _, err := execOverStreamer(
		context.Background(), clientStream, "cmd", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "out1out2" {
		t.Errorf("stdout = %q, want %q", stdout, "out1out2")
	}
	if stderr != "err1" {
		t.Errorf("stderr = %q, want %q", stderr, "err1")
	}
	if code != 1 {
		t.Errorf("exit_code = %d, want 1", code)
	}
}

func TestExec_NonZeroExitCode(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result", ExitCode: 2})
	}()

	_, _, code, _, err := execOverStreamer(
		context.Background(), clientStream, "false", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Errorf("exit_code = %d, want 2", code)
	}
}

func TestExec_Timeout(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stderr", Data: b64("command timed out\n")})
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result", ExitCode: 124, Truncated: true})
	}()

	_, stderr, code, trunc, err := execOverStreamer(
		context.Background(), clientStream, "sleep 9999", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 124 {
		t.Errorf("exit_code = %d, want 124", code)
	}
	if stderr != "command timed out\n" {
		t.Errorf("stderr = %q, want %q", stderr, "command timed out\n")
	}
	if !trunc {
		t.Errorf("truncated = false, want true")
	}
}

func TestExec_MalformedRequest(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		agentStream.Read(make([]byte, 4096)) //nolint:errcheck
		// Agent replies with id:"" + exit_code:-1 for malformed requests.
		agentSend(t, agentStream, execResponse{ID: "", Type: "result", ExitCode: -1})
	}()

	_, _, code, _, err := execOverStreamer(
		context.Background(), clientStream, "", 10*time.Second)
	_ = err // error is expected but checked via exit_code
	if code != -1 {
		t.Errorf("exit_code = %d, want -1", code)
	}
}

func TestExec_IDMismatch(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		agentStream.Read(make([]byte, 4096)) //nolint:errcheck
		agentSend(t, agentStream, execResponse{ID: "wrong-id-1234", Type: "result"})
	}()

	_, _, _, _, err := execOverStreamer(
		context.Background(), clientStream, "cmd", 10*time.Second)
	if err == nil {
		t.Fatal("expected error on ID mismatch, got nil")
	}
}

func TestExec_ContextCancellation(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		agentStream.Read(make([]byte, 4096)) //nolint:errcheck
		cancel()
		clientStream.Close() // unblocks the reader goroutine inside execOverStreamer
	}()

	_, _, _, _, err := execOverStreamer(ctx, clientStream, "cmd", 10*time.Second)
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
}

func TestExec_OmitemptyZeroValues(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		// Minimal result frame: only id and type; exit_code and truncated absent.
		data, _ := json.Marshal(map[string]string{"id": req.ID, "type": "result"})
		agentStream.agentRx <- data
	}()

	_, _, code, trunc, err := execOverStreamer(
		context.Background(), clientStream, "true", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit_code = %d, want 0 (omitted = zero)", code)
	}
	if trunc {
		t.Errorf("truncated = true, want false (omitted = zero)")
	}
}

func TestExec_RequestFormat(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	var gotReq execRequest
	done := make(chan struct{})
	go func() {
		defer close(done)
		gotReq = agentReadRequest(t, agentStream)
		agentSend(t, agentStream, execResponse{ID: gotReq.ID, Type: "result"})
	}()

	execOverStreamer(context.Background(), clientStream, "uname -a", 30*time.Second)
	<-done

	if gotReq.ID == "" {
		t.Error("request ID is empty")
	}
	if gotReq.Command != "uname -a" {
		t.Errorf("command = %q, want %q", gotReq.Command, "uname -a")
	}
	if gotReq.TimeoutSeconds != 30 {
		t.Errorf("timeout_seconds = %d, want 30", gotReq.TimeoutSeconds)
	}
}

// ---------------------------------------------------------------------------
// ExecHandle regression test — three sequential commands on one handle
// ---------------------------------------------------------------------------

// TestExec_ThreeSequentialCommandsOneHandle is the regression for the
// goroutine-leak / frame-theft bug: each per-Exec goroutine from the old
// implementation would keep reading from execStream after its Exec() returned,
// stealing frames belonging to the next command.
//
// This test drives THREE Exec() calls against ONE ExecHandle backed by a single
// fakeStream pair. Each call must receive its own correct stdout and exit_code
// with no frame theft and no hang.
func TestExec_ThreeSequentialCommandsOneHandle(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	// Fake agent: serve three requests in order, each with a distinct stdout.
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		commands := []string{"cmd1", "cmd2", "cmd3"}
		for _, wantCmd := range commands {
			req := agentReadRequest(t, agentStream)
			if req.Command != wantCmd {
				t.Errorf("agent: got command %q, want %q", req.Command, wantCmd)
			}
			agentSend(t, agentStream, execResponse{
				ID:   req.ID,
				Type: "stdout",
				Data: b64("result-" + req.Command),
			})
			agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result"})
		}
	}()

	// Build a real ExecHandle using the override field so the reader goroutine
	// reads from our fakeStream.
	h := newTestExecHandleViaOverride(clientStream)
	defer h.Close()

	type result struct {
		stdout string
		code   int
		err    error
	}

	for i, cmd := range []string{"cmd1", "cmd2", "cmd3"} {
		stdout, _, code, _, err := h.Exec(context.Background(), cmd, 5*time.Second)
		r := result{stdout, code, err}
		if r.err != nil {
			t.Fatalf("cmd #%d (%s): unexpected error: %v", i+1, cmd, r.err)
		}
		want := "result-" + cmd
		if r.stdout != want {
			t.Errorf("cmd #%d (%s): stdout = %q, want %q", i+1, cmd, r.stdout, want)
		}
		if r.code != 0 {
			t.Errorf("cmd #%d (%s): exit_code = %d, want 0", i+1, cmd, r.code)
		}
	}

	// Close the handle and make sure the agent served all three requests.
	h.Close()
	clientStream.Close() // unblock agent if still waiting
	<-agentDone
}

// TestExec_HandleClosedWhileWaiting verifies that closing an ExecHandle while
// Exec() is blocked waiting for a result causes Exec() to return an error.
func TestExec_HandleClosedWhileWaiting(t *testing.T) {
	clientStream, agentStream := newFakeStreamPair()

	h := newTestExecHandleViaOverride(clientStream)

	// Agent reads the request but never responds.
	go func() {
		agentReadRequest(t, agentStream) // consume request
		// Close the handle from another goroutine while Exec() is waiting.
		h.Close()
	}()

	_, _, _, _, err := h.Exec(context.Background(), "cmd", 10*time.Second)
	if err == nil {
		t.Fatal("expected error when handle is closed mid-Exec, got nil")
	}
}

// TestExec_ExecOnClosedHandle verifies that calling Exec() after Close()
// returns an immediate error.
func TestExec_ExecOnClosedHandle(t *testing.T) {
	clientStream, _ := newFakeStreamPair()
	h := newTestExecHandleViaOverride(clientStream)
	h.Close()

	_, _, _, _, err := h.Exec(context.Background(), "cmd", 5*time.Second)
	if err == nil {
		t.Fatal("expected error on Exec after Close, got nil")
	}
}

// TestExec_SequentialCommands verifies two sequential calls on separate
// execOverStreamer invocations (protocol smoke test).
func TestExec_SequentialCommands(t *testing.T) {
	for i, cmd := range []string{"cmd1", "cmd2"} {
		clientStream, agentStream := newFakeStreamPair()

		go func() {
			req := agentReadRequest(t, agentStream)
			agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("out-" + req.Command)})
			agentSend(t, agentStream, execResponse{ID: req.ID, Type: "result"})
		}()

		stdout, _, _, _, err := execOverStreamer(
			context.Background(), clientStream, cmd, 5*time.Second)
		if err != nil {
			t.Fatalf("cmd[%d] %s error: %v", i, cmd, err)
		}
		if want := "out-" + cmd; stdout != want {
			t.Errorf("cmd[%d] stdout = %q, want %q", i, stdout, want)
		}
	}
}

// TestExec_OutputCapAborts is revert-sensitive: pre-fix, Exec() appends every
// stdout/stderr frame to stdoutBuf/stderrBuf with no cumulative size check,
// so a single frame exceeding the (temporarily lowered) cap is silently
// accumulated and the call blocks waiting for a result frame that never
// comes, until the context deadline fires. Post-fix, Exec() detects the
// overflow on the offending frame and returns an error immediately.
func TestExec_OutputCapAborts(t *testing.T) {
	orig := maxExecOutputBytes
	maxExecOutputBytes = 16
	defer func() { maxExecOutputBytes = orig }()

	clientStream, agentStream := newFakeStreamPair()

	go func() {
		req := agentReadRequest(t, agentStream)
		// Decoded payload (32 bytes) exceeds the 16-byte cap in one frame.
		agentSend(t, agentStream, execResponse{ID: req.ID, Type: "stdout", Data: b64("this payload is over the cap!!!")})
		// No result frame is ever sent: a real bug would hang here until ctx
		// timeout instead of returning the cap error below.
	}()

	h := newTestExecHandleViaOverride(clientStream)
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, _, _, err := h.Exec(ctx, "cmd", 5*time.Second)
	if err == nil {
		t.Fatal("expected an error once accumulated output exceeds the cap, got nil")
	}
	if err == context.DeadlineExceeded {
		t.Fatal("Exec() hung until context deadline instead of detecting the output cap overflow")
	}
}

// ---------------------------------------------------------------------------
// UUID helper test
// ---------------------------------------------------------------------------

func TestNewUUID(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID error: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("UUID length = %d, want 36", len(id))
	}
	for _, pos := range []int{8, 13, 18, 23} {
		if id[pos] != '-' {
			t.Errorf("expected '-' at position %d, got %q", pos, id[pos])
		}
	}
	if id[14] != '4' {
		t.Errorf("UUID version nibble = %q, want '4'", id[14])
	}
}
