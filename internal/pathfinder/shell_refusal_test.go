//go:build !windows

package pathfinder

import (
	"testing"
	"time"
)

// newPairedRelay returns a RelayClient flipped into the connected+paired state
// without a live socket. SendFrame only requires those two flags and a buffered
// binaryChan, so OpenStream succeeds and frames are dropped into the buffer.
func newPairedRelay() *RelayClient {
	relay := NewRelayClient("wss://test/ws", "session", false)
	relay.setConnected(true)
	relay.setPaired(true)
	return relay
}

// TestStartShellSession_RefusedReturnsRefused verifies that when the agent
// closes the shell stream with zero bytes of output (the read-only enforcement
// path), StartShellSession reports Refused=true and never touches the terminal.
// This is the signal the connect lifecycle uses to fall back to webadmin-only
// keep-alive instead of treating the empty session as a normal terminal exit.
func TestStartShellSession_RefusedReturnsRefused(t *testing.T) {
	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)

	type result struct {
		outcome ShellOutcome
		err     error
	}
	resCh := make(chan result, 1)
	go func() {
		outcome, err := StartShellSession(streamMgr)
		resCh <- result{outcome: outcome, err: err}
	}()

	// StartShellSession opens "shell" (id 1) then "shell-ctl" (id 2). Simulate
	// the agent refusing by closing the shell stream with no data. The probe
	// reads from the shell stream and must observe EOF.
	closeShellStream := func(id uint32) {
		frame := &Frame{Type: FrameTypeClose, StreamID: id}
		streamMgr.handleMessage(EncodeFrame(frame))
	}

	// Give the goroutine a moment to register both streams, then refuse.
	time.Sleep(50 * time.Millisecond)
	closeShellStream(1)

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("expected no error on refusal, got %v", res.err)
		}
		if !res.outcome.Refused {
			t.Fatalf("expected Refused=true when shell closed with zero bytes, got Refused=false")
		}
	case <-time.After(shellProbeTimeout + 2*time.Second):
		t.Fatal("StartShellSession did not return after shell stream was refused")
	}
}

// TestStartShellSession_ProbeTimeoutIsRefused verifies that if the agent never
// sends shell output and never closes the stream, StartShellSession gives up
// after the probe window and reports Refused rather than hanging the UI.
func TestStartShellSession_ProbeTimeoutIsRefused(t *testing.T) {
	relay := newPairedRelay()
	streamMgr := NewStreamManager(relay)

	type result struct {
		outcome ShellOutcome
		err     error
	}
	resCh := make(chan result, 1)
	go func() {
		outcome, err := StartShellSession(streamMgr)
		resCh <- result{outcome: outcome, err: err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("expected no error on probe timeout, got %v", res.err)
		}
		if !res.outcome.Refused {
			t.Fatalf("expected Refused=true on probe timeout, got Refused=false")
		}
	case <-time.After(shellProbeTimeout + 2*time.Second):
		t.Fatal("StartShellSession did not return after the probe window elapsed")
	}
}
