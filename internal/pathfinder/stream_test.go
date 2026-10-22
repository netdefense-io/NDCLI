package pathfinder

import (
	"bytes"
	"fmt"
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
// removed from the manager. The frame queued before the lost one is still
// delivered, and the error comes where the data stops.
func TestHandleData_PendingBufferOverflowClosesStream(t *testing.T) {
	mgr := newTestStreamManager()

	const streamID = uint32(7)
	s := &Stream{
		id:          streamID,
		manager:     mgr,
		serviceName: "test",
		pendingData: make(chan []byte, 1),
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

	got, err := io.ReadAll(s)
	if string(got) != "already queued" {
		t.Errorf("read %q before the error, want the frame queued before the overflow", got)
	}
	if err == nil {
		t.Fatal("Read() ended with a plain io.EOF after overflow, want the distinguishable overflow error")
	}

	mgr.streamsMu.RLock()
	_, stillPresent := mgr.streams[streamID]
	mgr.streamsMu.RUnlock()
	if stillPresent {
		t.Error("stream is still present in the manager after overflow; expected it to be removed")
	}
}

// openTestStream opens stream id on mgr the way a peer does and returns it.
func openTestStream(t *testing.T, mgr *StreamManager, id uint32) *Stream {
	t.Helper()
	var opened *Stream
	mgr.OnNewStream(func(s *Stream) { opened = s })
	mgr.handleMessage(openFrame(id))
	if opened == nil {
		t.Fatalf("stream %d was not opened", id)
	}
	return opened
}

// sendFrames feeds count DATA frames and then a CLOSE for stream id through
// the read-pump entry point, back to back, as a peer that replies and closes
// at once does. It returns the bytes the frames carried, in order.
func sendFrames(mgr *StreamManager, id uint32, count int) []byte {
	var sent bytes.Buffer
	for i := 0; i < count; i++ {
		chunk := []byte(fmt.Sprintf("frame %03d of stream %d\n", i, id))
		sent.Write(chunk)
		mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: id, Data: chunk}))
	}
	mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeClose, StreamID: id}))
	return sent.Bytes()
}

func readAllWithin(t *testing.T, s *Stream, d time.Duration) []byte {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(s)
		done <- result{data, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read error: %v", r.err)
		}
		return r.data
	case <-time.After(d):
		t.Fatal("Read did not reach end of stream")
		return nil
	}
}

// Every DATA frame that arrived before a CLOSE must be read before Read
// reports end of stream, whether the reader starts after the CLOSE was
// handled or is already waiting while the frames arrive. Repeated, because
// the loss was a scheduling race.
func TestReadDeliversEveryFrameQueuedBeforeClose(t *testing.T) {
	const rounds, frames = 200, 64

	t.Run("reader starts after the close", func(t *testing.T) {
		for round := 0; round < rounds; round++ {
			mgr := newTestStreamManager()
			id := uint32(round + 1)
			s := openTestStream(t, mgr, id)
			sent := sendFrames(mgr, id, frames)
			if got := readAllWithin(t, s, 5*time.Second); !bytes.Equal(got, sent) {
				t.Fatalf("round %d: read %d of %d bytes", round, len(got), len(sent))
			}
		}
	})

	t.Run("reader waiting while the frames arrive", func(t *testing.T) {
		for round := 0; round < rounds; round++ {
			mgr := newTestStreamManager()
			id := uint32(round + 1)
			s := openTestStream(t, mgr, id)
			got := make(chan []byte, 1)
			go func() {
				data, _ := io.ReadAll(s)
				got <- data
			}()
			sent := sendFrames(mgr, id, frames)
			select {
			case data := <-got:
				if !bytes.Equal(data, sent) {
					t.Fatalf("round %d: read %d of %d bytes", round, len(data), len(sent))
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("round %d: Read did not reach end of stream", round)
			}
		}
	})
}

// Closing our end after the peer's CLOSE (the shell session does this when
// the shell-ctl stream closes) must not discard what the peer sent first.
func TestLocalCloseKeepsFramesAlreadyReceived(t *testing.T) {
	mgr := newTestStreamManager()
	s := openTestStream(t, mgr, 1)
	sent := sendFrames(mgr, 1, 16)
	s.Close()
	if got := readAllWithin(t, s, 5*time.Second); !bytes.Equal(got, sent) {
		t.Fatalf("read %q, want %q", got, sent)
	}
}

// A frame larger than the caller's buffer is returned over several reads,
// not cut at the buffer size.
func TestReadReturnsALargeFrameAcrossShortReads(t *testing.T) {
	mgr := newTestStreamManager()
	s := openTestStream(t, mgr, 1)
	mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: []byte("0123456789")}))
	mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: []byte("abc")}))
	mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeClose, StreamID: 1}))

	var got []string
	buf := make([]byte, 4)
	for {
		n, err := s.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got = append(got, string(buf[:n]))
	}
	if want := []string{"0123", "4567", "89", "abc"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("reads = %q, want %q", got, want)
	}
}
