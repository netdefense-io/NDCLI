package pathfinder

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// maxConcurrentStreams bounds the goroutines/buffers a peer-initiated
// FrameTypeOpen flood can force onto this process.
const maxConcurrentStreams = 64

// StreamManager manages multiplexed streams over a relay client connection
type StreamManager struct {
	client *RelayClient

	streams      map[uint32]*Stream
	streamsMu    sync.RWMutex
	nextStreamID uint32

	onNewStream func(*Stream)
}

// NewStreamManager creates a new stream manager for the given relay client
func NewStreamManager(client *RelayClient) *StreamManager {
	mgr := &StreamManager{
		client:  client,
		streams: make(map[uint32]*Stream),
	}

	// Set up frame handler
	client.OnFrame(func(data []byte) {
		mgr.handleMessage(data)
	})

	return mgr
}

// OnNewStream sets the callback for new incoming streams
func (m *StreamManager) OnNewStream(handler func(*Stream)) {
	m.onNewStream = handler
}

// OpenStream opens a new stream to the remote peer
func (m *StreamManager) OpenStream(serviceName string) (*Stream, error) {
	streamID := atomic.AddUint32(&m.nextStreamID, 1)

	stream := newStream(streamID, m, "")

	m.streamsMu.Lock()
	m.streams[streamID] = stream
	m.streamsMu.Unlock()

	// Send open frame with service name
	frame := &Frame{
		Type:     FrameTypeOpen,
		StreamID: streamID,
		Data:     []byte(serviceName),
	}

	if err := m.send(frame); err != nil {
		m.streamsMu.Lock()
		delete(m.streams, streamID)
		m.streamsMu.Unlock()
		stream.closeInternal()
		return nil, err
	}

	return stream, nil
}

func (m *StreamManager) handleMessage(data []byte) {
	frame, err := DecodeFrame(data)
	if err != nil {
		return
	}

	switch frame.Type {
	case FrameTypeOpen:
		m.handleOpen(frame)
	case FrameTypeData:
		m.handleData(frame)
	case FrameTypeClose:
		m.handleClose(frame)
	case FrameTypeAck:
		// ACK handling can be used for flow control
	}
}

func (m *StreamManager) handleOpen(frame *Frame) {
	m.streamsMu.Lock()
	if m.onNewStream == nil || len(m.streams) >= maxConcurrentStreams {
		m.streamsMu.Unlock()
		debugLog("rejecting FrameTypeOpen for stream %d: no consumer registered or max concurrent streams (%d) reached", frame.StreamID, maxConcurrentStreams)
		m.send(&Frame{Type: FrameTypeClose, StreamID: frame.StreamID})
		return
	}

	serviceName := string(frame.Data)
	stream := newStream(frame.StreamID, m, serviceName)
	m.streams[frame.StreamID] = stream
	m.streamsMu.Unlock()

	// Send ACK
	ack := &Frame{
		Type:     FrameTypeAck,
		StreamID: frame.StreamID,
	}
	m.send(ack)

	m.onNewStream(stream)
}

func (m *StreamManager) handleData(frame *Frame) {
	m.streamsMu.RLock()
	stream := m.streams[frame.StreamID]
	m.streamsMu.RUnlock()

	if stream == nil {
		return
	}

	// Non-blocking send to pending queue - this keeps the read pump responsive
	// for ping/pong handling.
	select {
	case stream.pendingData <- frame.Data:
	case <-stream.closed:
	default:
		// Pending buffer full (should be rare with a 100k buffer). Rather than
		// silently dropping the frame and letting the consumer desync, close
		// the stream with an error so the loss is surfaced immediately.
		debugLog("pendingData full for stream %d, closing stream (frame dropped, %d bytes)", frame.StreamID, len(frame.Data))
		stream.closeWithError(fmt.Errorf("stream %d: pendingData buffer overflow, frame dropped", frame.StreamID))
		m.removeStream(frame.StreamID)
	}
}

func (m *StreamManager) handleClose(frame *Frame) {
	m.streamsMu.Lock()
	stream := m.streams[frame.StreamID]
	if stream != nil {
		delete(m.streams, frame.StreamID)
	}
	m.streamsMu.Unlock()

	if stream != nil {
		debugLog("Received CLOSE frame for stream %d (service: %s)", frame.StreamID, stream.serviceName)
		stream.closeInternal()
	}
}

func (m *StreamManager) send(frame *Frame) error {
	data := EncodeFrame(frame)
	return m.client.SendFrame(data)
}

func (m *StreamManager) removeStream(streamID uint32) {
	m.streamsMu.Lock()
	delete(m.streams, streamID)
	m.streamsMu.Unlock()
}

// CloseAll closes all active streams. Called when the relay connection dies.
func (m *StreamManager) CloseAll() {
	m.streamsMu.Lock()
	defer m.streamsMu.Unlock()

	for _, stream := range m.streams {
		stream.closeInternal()
	}
	m.streams = make(map[uint32]*Stream)
}

// Stream represents a multiplexed stream over the relay connection
type Stream struct {
	id          uint32
	manager     *StreamManager
	serviceName string

	// pendingData holds incoming frames in arrival order. It is large so that
	// handleData never blocks, keeping the read pump responsive for ping/pong
	// handling; Read consumes it directly.
	pendingData chan []byte

	// unread is the rest of a frame that a short Read did not consume.
	unread []byte

	closed     chan struct{}
	closeOnce  sync.Once
	closedOnce sync.Once

	// closeErr is set (once, before closed is closed) when the stream is torn
	// down abnormally (e.g. pendingData overflow). Read() surfaces it instead
	// of a plain io.EOF so callers can tell "peer closed cleanly" from "we
	// lost data and gave up".
	closeErr error
}

// newStream creates a new stream
func newStream(id uint32, manager *StreamManager, serviceName string) *Stream {
	return &Stream{
		id:          id,
		manager:     manager,
		serviceName: serviceName,
		pendingData: make(chan []byte, 100000), // Large buffer for burst traffic
		closed:      make(chan struct{}),
	}
}

// ID returns the stream ID
func (s *Stream) ID() uint32 {
	return s.id
}

// ServiceName returns the service name (for incoming streams)
func (s *Stream) ServiceName() string {
	return s.serviceName
}

// Closed returns a channel that is closed when the stream is closed (locally
// via Close/closeInternal or remotely via a received CLOSE frame). Callers can
// select on it to react to a peer-initiated close without reading the stream.
func (s *Stream) Closed() <-chan struct{} {
	return s.closed
}

// Read reads data from the stream in arrival order. A frame larger than p is
// returned over several calls. After the stream closes, Read still returns
// everything received before the close, and only then io.EOF (or the error
// the stream was closed with).
func (s *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.unread) == 0 {
		data, err := s.nextFrame()
		if err != nil {
			return 0, err
		}
		s.unread = data
	}
	n := copy(p, s.unread)
	s.unread = s.unread[n:]
	return n, nil
}

func (s *Stream) nextFrame() ([]byte, error) {
	select {
	case data := <-s.pendingData:
		return data, nil
	case <-s.closed:
	}
	// Closed, but frames that arrived before the close may still be queued.
	select {
	case data := <-s.pendingData:
		return data, nil
	default:
	}
	if s.closeErr != nil {
		return nil, s.closeErr
	}
	return nil, io.EOF
}

// Write writes data to the stream
func (s *Stream) Write(p []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, io.ErrClosedPipe
	default:
	}

	frame := &Frame{
		Type:     FrameTypeData,
		StreamID: s.id,
		Data:     p,
	}

	if err := s.manager.send(frame); err != nil {
		return 0, err
	}

	return len(p), nil
}

// Close closes the stream
func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		// Send close frame
		frame := &Frame{
			Type:     FrameTypeClose,
			StreamID: s.id,
		}
		s.manager.send(frame)

		s.closeInternal()
		s.manager.removeStream(s.id)
	})
	return nil
}

func (s *Stream) closeInternal() {
	s.closedOnce.Do(func() {
		close(s.closed)
	})
}

// closeWithError tears the stream down abnormally, recording err so Read()
// can surface it instead of a plain io.EOF. Used when we detect data loss
// (e.g. pendingData overflow) rather than a clean peer-initiated or local
// close; callers are responsible for removing the stream from its manager.
func (s *Stream) closeWithError(err error) {
	s.closedOnce.Do(func() {
		s.closeErr = err
		close(s.closed)
	})
}
