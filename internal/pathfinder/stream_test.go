package pathfinder

import (
	"io"
	"testing"
	"time"
)

// newTestStreamManager builds a real *StreamManager wired to a RelayClient
// that is never actually connected. StreamManager.send() calls through to
// RelayClient.SendFrame(), which fails cheaply ("not connected") without a
// live socket; every call site in handleOpen/handleData/handleClose ignores
// that error, so this is sufficient to exercise the stream bookkeeping logic
// under test without any network I/O.
func newTestStreamManager() *StreamManager {
	client := NewRelayClient("ws://127.0.0.1:0/fake", "test-session", false)
	return NewStreamManager(client)
}

// openFrame builds an encoded FrameTypeOpen message for the given stream ID.
func openFrame(streamID uint32) []byte {
	return EncodeFrame(&Frame{Type: FrameTypeOpen, StreamID: streamID, Data: []byte("shell")})
}

// TestHandleOpen_NoConsumerRejectsEverything is revert-sensitive: pre-fix,
// handleOpen unconditionally allocates a Stream (100k+4k buffered channels
// and a goroutine) for every inbound FrameTypeOpen regardless of whether
// onNewStream is registered. Every current production caller leaves
// onNewStream nil, so pre-fix this test finds streams accumulating in the
// map; post-fix, with no consumer registered, every Open frame is rejected
// and the map stays empty.
func TestHandleOpen_NoConsumerRejectsEverything(t *testing.T) {
	mgr := newTestStreamManager()

	for i := uint32(1); i <= 10; i++ {
		mgr.handleMessage(openFrame(i))
	}

	mgr.streamsMu.RLock()
	got := len(mgr.streams)
	mgr.streamsMu.RUnlock()

	if got != 0 {
		t.Fatalf("streams = %d, want 0 (no consumer registered, all Open frames should be rejected)", got)
	}
}

// TestHandleOpen_CapsConcurrentStreams pins the maxConcurrentStreams boundary
// once a consumer is registered: the 65th Open frame must be rejected rather
// than accepted.
func TestHandleOpen_CapsConcurrentStreams(t *testing.T) {
	mgr := newTestStreamManager()
	mgr.OnNewStream(func(*Stream) {})

	for i := uint32(1); i <= maxConcurrentStreams+1; i++ {
		mgr.handleMessage(openFrame(i))
	}

	mgr.streamsMu.RLock()
	got := len(mgr.streams)
	mgr.streamsMu.RUnlock()

	if got != maxConcurrentStreams {
		t.Fatalf("streams = %d, want %d (cap should reject the (%d+1)th Open frame)", got, maxConcurrentStreams, maxConcurrentStreams)
	}
}

// TestHandleData_PendingBufferOverflowClosesStream is revert-sensitive:
// pre-fix, handleData's default branch on a full pendingData channel only
// logs at debug level and returns, leaving the stream open with no signal to
// the consumer — the <-s.closed wait below never fires and this test times
// out. Post-fix, the stream is closed with a distinguishable error and
// removed from the manager.
func TestHandleData_PendingBufferOverflowClosesStream(t *testing.T) {
	mgr := newTestStreamManager()

	const streamID = uint32(7)
	s := &Stream{
		id:          streamID,
		manager:     mgr,
		serviceName: "test",
		pendingData: make(chan []byte, 1),
		recvChan:    make(chan []byte, 4),
		closed:      make(chan struct{}),
	}
	// Pre-fill the single pendingData slot so the next handleData call has
	// nowhere to put the frame and must take the overflow branch.
	s.pendingData <- []byte("already queued")

	mgr.streamsMu.Lock()
	mgr.streams[streamID] = s
	mgr.streamsMu.Unlock()

	mgr.handleData(&Frame{Type: FrameTypeData, StreamID: streamID, Data: []byte("overflow me")})

	select {
	case <-s.closed:
	case <-time.After(time.Second):
		t.Fatal("stream was not closed after pendingData overflow (pre-fix behavior: frame silently dropped)")
	}

	n, err := s.Read(make([]byte, 16))
	if err == nil {
		t.Fatal("expected a non-nil error from Read() after overflow, got nil")
	}
	if err == io.EOF {
		t.Fatal("expected a distinguishable overflow error from Read(), got plain io.EOF")
	}
	if n != 0 {
		t.Errorf("Read() n = %d, want 0", n)
	}

	mgr.streamsMu.RLock()
	_, stillPresent := mgr.streams[streamID]
	mgr.streamsMu.RUnlock()
	if stillPresent {
		t.Error("stream is still present in the manager after overflow; expected it to be removed")
	}
}
