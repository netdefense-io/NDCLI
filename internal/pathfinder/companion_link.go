package pathfinder

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// linkQueueBytes is how many bytes of frames may wait for the companion to
// take them before a sender of data has to wait.
const linkQueueBytes = 4 * 1024 * 1024

// companionLink is a link through a companion. The companion's stdin and
// stdout are one byte stream to the device, so frames travel back to back,
// each found by the length in its header.
type companionLink struct {
	proc   *companion
	onPath func(direct bool)

	onFrameMu sync.RWMutex
	onFrame   func([]byte)

	streamManagerMu sync.RWMutex
	streamManager   *StreamManager

	// The frames waiting for the companion, oldest first. more wakes the
	// writer; room wakes senders waiting for the queue to shrink.
	mu     sync.Mutex
	more   *sync.Cond
	room   *sync.Cond
	queue  [][]byte
	queued int
	ended  bool

	endMu     sync.Mutex
	endReason string
	endOnce   sync.Once
	done      chan struct{}

	// writerDone is closed when the writer has handed over what it will;
	// stdoutDone when the companion's stdout has been read to its end.
	writerDone chan struct{}
	stdoutDone chan struct{}

	// lastSent and lastReceived are when a frame last went to the device and
	// last came from it, as time since started.
	started      time.Time
	lastSent     atomic.Int64
	lastReceived atomic.Int64
}

// newCompanionLink is the link of a companion that has reached the device.
func newCompanionLink(proc *companion, onPath func(direct bool)) *companionLink {
	l := &companionLink{
		proc:       proc,
		onPath:     onPath,
		done:       make(chan struct{}),
		writerDone: make(chan struct{}),
		stdoutDone: make(chan struct{}),
		started:    time.Now(),
	}
	l.more = sync.NewCond(&l.mu)
	l.room = sync.NewCond(&l.mu)
	go l.writeFrames()
	go l.readFrames()
	go l.supervise()
	go l.keepAlive()
	return l
}

// keepAlive sends a keepalive frame when nothing has been sent for the
// keepalive interval, and ends the session when no frame has come for the
// receive timeout.
func (l *companionLink) keepAlive() {
	waits := l.proc.waits
	tick := time.NewTicker(waits.check)
	defer tick.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-tick.C:
		}
		now := time.Since(l.started)
		if now-time.Duration(l.lastReceived.Load()) >= waits.receive {
			l.finish("the device stopped responding")
			return
		}
		if now-time.Duration(l.lastSent.Load()) >= waits.keepAlive {
			l.mu.Lock()
			if !l.ended && len(l.queue) == 0 {
				frame := EncodeFrame(&Frame{Type: FrameTypeKeepAlive})
				l.queue = append(l.queue, frame)
				l.queued += len(frame)
				l.lastSent.Store(int64(now))
				l.more.Signal()
			}
			l.mu.Unlock()
		}
	}
}

// SendFrame queues one frame for the device. A data frame waits while the
// queue is full, so a slow path to the device slows its sender down instead
// of growing the queue. Other frames never wait: the stream manager sends
// them from the goroutine that reads the device's frames, which must not
// stop reading.
func (l *companionLink) SendFrame(data []byte) error {
	frame := make([]byte, len(data))
	copy(frame, data)

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(frame) > 0 && frame[0] == FrameTypeData {
		for !l.ended && l.queued > 0 && l.queued+len(frame) > linkQueueBytes {
			l.room.Wait()
		}
	}
	if l.ended {
		return fmt.Errorf("not connected")
	}
	l.queue = append(l.queue, frame)
	l.queued += len(frame)
	l.more.Signal()
	return nil
}

// writeFrames hands the queued frames to the companion. Once the link has
// ended it still hands over what was queued before, then stops.
func (l *companionLink) writeFrames() {
	defer close(l.writerDone)
	for {
		l.mu.Lock()
		for len(l.queue) == 0 && !l.ended {
			l.more.Wait()
		}
		if len(l.queue) == 0 {
			l.mu.Unlock()
			return
		}
		frame := l.queue[0]
		l.queue[0] = nil
		l.queue = l.queue[1:]
		l.mu.Unlock()

		_, err := l.proc.stdin.Write(frame)

		l.mu.Lock()
		l.queued -= len(frame)
		l.room.Broadcast()
		l.mu.Unlock()
		if err != nil {
			// The companion has gone; its last event says why.
			return
		}
		l.lastSent.Store(int64(time.Since(l.started)))
	}
}

// readFrames delivers the device's frames, whole, in order.
func (l *companionLink) readFrames() {
	defer close(l.stdoutDone)
	defer func() { _ = l.proc.stdout.Close() }()
	r := bufio.NewReaderSize(l.proc.stdout, 64*1024)
	var header [frameHeaderLen]byte
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return
		}
		length := binary.BigEndian.Uint32(header[5:9])
		if length > maxStreamFrameData {
			// Nothing after a wrong length can be trusted to be a frame.
			debugLog("%s: a frame of %d bytes, more than the %d a frame may hold", companionName, length, maxStreamFrameData)
			l.finish("the session's data could not be read")
			_, _ = io.Copy(io.Discard, r)
			return
		}
		frame := make([]byte, frameHeaderLen+int(length))
		copy(frame, header[:])
		if _, err := io.ReadFull(r, frame[frameHeaderLen:]); err != nil {
			return
		}
		l.lastReceived.Store(int64(time.Since(l.started)))
		if frame[0] == FrameTypeKeepAlive {
			continue
		}
		// A frame that arrives before a handler is set is dropped, as the
		// websocket relay client drops it. The device speaks only on streams
		// this end opened, and none exists before the handler does.
		l.onFrameMu.RLock()
		handler := l.onFrame
		l.onFrameMu.RUnlock()
		if handler != nil && l.IsConnected() {
			handler(frame)
		}
	}
}

// supervise follows the companion's events: the path, and how the session
// ends. A companion that leaves without saying why has stopped unexpectedly.
func (l *companionLink) supervise() {
	defer func() {
		// Whatever the companion still writes is read, so it is never held
		// up on its way out.
		for range l.proc.events {
		}
	}()
	for {
		select {
		case ev, ok := <-l.proc.events:
			if !ok {
				l.awaitLastBytes()
				l.finish(companionEndReason(""))
				return
			}
			switch ev.Event {
			case companionPath:
				if l.onPath != nil {
					l.onPath(ev.Path == companionPathDirect)
				}
			case companionClosed, companionFailed:
				if ev.Reason == companionPeerClosed {
					// The device's last bytes may still be on their way
					// through the companion's stdout.
					l.awaitLastBytes()
				}
				l.finish(companionEndReason(ev.Reason))
				return
			}
		case <-l.done:
			return
		}
	}
}

// awaitLastBytes waits, for a bounded time, until the companion's stdout has
// been read to its end.
func (l *companionLink) awaitLastBytes() {
	select {
	case <-l.stdoutDone:
	case <-l.done:
	case <-time.After(l.proc.waits.lastBytes):
	}
}

// finish ends the link because the other side did, for reason. The reason is
// recorded before the streams close, so a reader woken by the close sees it.
// Unlike Close it does not wait for queued frames to be handed over: nobody
// is left to read them.
func (l *companionLink) finish(reason string) {
	l.end(reason)
	l.proc.stop()
}

func (l *companionLink) end(reason string) {
	l.endOnce.Do(func() {
		l.endMu.Lock()
		l.endReason = reason
		l.endMu.Unlock()

		l.mu.Lock()
		l.ended = true
		l.more.Broadcast()
		l.room.Broadcast()
		l.mu.Unlock()

		l.streamManagerMu.RLock()
		mgr := l.streamManager
		l.streamManagerMu.RUnlock()
		if mgr != nil {
			mgr.CloseAll()
		}
		close(l.done)
	})
}

// Close ends the session from this end. The frames already queued are handed
// over first, then the companion is told to leave, which closes the
// connection to the device.
func (l *companionLink) Close() error {
	l.end("")
	select {
	case <-l.writerDone:
	case <-time.After(l.proc.waits.exit):
	}
	l.proc.stop()
	select {
	case <-l.proc.exited:
	case <-time.After(l.proc.waits.exit):
	}
	return nil
}

func (l *companionLink) OnFrame(handler func([]byte)) {
	l.onFrameMu.Lock()
	l.onFrame = handler
	l.onFrameMu.Unlock()
}

func (l *companionLink) SetStreamManager(mgr *StreamManager) {
	l.streamManagerMu.Lock()
	l.streamManager = mgr
	l.streamManagerMu.Unlock()
}

func (l *companionLink) IsConnected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.ended
}

func (l *companionLink) EndReason() string {
	l.endMu.Lock()
	defer l.endMu.Unlock()
	return l.endReason
}

func (l *companionLink) Done() <-chan struct{} { return l.done }

var _ link = (*companionLink)(nil)
