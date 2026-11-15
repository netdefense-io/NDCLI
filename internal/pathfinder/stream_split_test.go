package pathfinder

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// captureLink is a link that keeps the frames it is given.
type captureLink struct {
	mu     sync.Mutex
	frames []*Frame
	// failAfter, when above zero, refuses every frame after that many.
	failAfter int
}

func (c *captureLink) SendFrame(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failAfter > 0 && len(c.frames) >= c.failAfter {
		return errors.New("not connected")
	}
	frame, err := DecodeFrame(append([]byte(nil), data...))
	if err != nil {
		return err
	}
	c.frames = append(c.frames, frame)
	return nil
}

func (c *captureLink) OnFrame(func([]byte))            {}
func (c *captureLink) SetStreamManager(*StreamManager) {}
func (c *captureLink) IsConnected() bool               { return true }
func (c *captureLink) EndReason() string               { return "" }
func (c *captureLink) Done() <-chan struct{}           { return nil }
func (c *captureLink) Close() error                    { return nil }

// dataFrames are the data frames sent after the stream's open frame.
func (c *captureLink) dataFrames() []*Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	var data []*Frame
	for _, f := range c.frames {
		if f.Type == FrameTypeData {
			data = append(data, f)
		}
	}
	return data
}

// A direct-peer session ends on a frame longer than maxStreamFrameData, so a
// write of any size goes out in frames no longer than that, in order.
func TestAWriteLongerThanAFrameIsSplit(t *testing.T) {
	ln := &captureLink{}
	stream, err := NewStreamManager(ln).OpenStream("webadmin")
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 3*maxStreamFrameData+3392)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	n, err := stream.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = %d, %v; want %d", n, err, len(payload))
	}

	frames := ln.dataFrames()
	var sizes []int
	var joined []byte
	for _, f := range frames {
		sizes = append(sizes, len(f.Data))
		joined = append(joined, f.Data...)
		if f.StreamID != stream.ID() {
			t.Errorf("a frame went to stream %d, want %d", f.StreamID, stream.ID())
		}
	}
	if want := []int{maxStreamFrameData, maxStreamFrameData, maxStreamFrameData, 3392}; len(sizes) != len(want) || sizes[0] != want[0] || sizes[3] != want[3] {
		t.Errorf("frame sizes = %v, want %v", sizes, want)
	}
	if !bytes.Equal(joined, payload) {
		t.Error("the frames do not add up to what was written")
	}
}

// Writes that fit in one frame go out as one frame, as they always have.
func TestAWriteThatFitsIsOneFrame(t *testing.T) {
	for _, size := range []int{0, 1, 32 * 1024, maxStreamFrameData} {
		ln := &captureLink{}
		stream, err := NewStreamManager(ln).OpenStream("shell")
		if err != nil {
			t.Fatal(err)
		}
		if n, err := stream.Write(make([]byte, size)); err != nil || n != size {
			t.Fatalf("Write(%d bytes) = %d, %v", size, n, err)
		}
		if frames := ln.dataFrames(); len(frames) != 1 || len(frames[0].Data) != size {
			t.Errorf("a write of %d bytes went out as %d frames", size, len(frames))
		}
	}
}

func TestAWriteCutShortSaysHowMuchWasSent(t *testing.T) {
	// The open frame, then two data frames, then the link refuses.
	ln := &captureLink{failAfter: 3}
	stream, err := NewStreamManager(ln).OpenStream("webadmin")
	if err != nil {
		t.Fatal(err)
	}
	n, err := stream.Write(make([]byte, 4*maxStreamFrameData))
	if err == nil || n != 2*maxStreamFrameData {
		t.Errorf("Write = %d, %v; want %d and the link's error", n, err, 2*maxStreamFrameData)
	}
}

// The exec stream needs one request per frame, so a request must never be
// split: the largest one the client sends fits in a frame.
func TestAnExecRequestIsNeverSplit(t *testing.T) {
	if largest := MaxExecFrameBytes - frameHeaderLen; largest > maxStreamFrameData {
		t.Errorf("an exec request may be %d bytes and a frame carries %d", largest, maxStreamFrameData)
	}
}
