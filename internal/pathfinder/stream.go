package pathfinder

import (
	"io"
	"sync"
	"sync/atomic"
)

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
	serviceName := string(frame.Data)

	stream := newStream(frame.StreamID, m, serviceName)

	m.streamsMu.Lock()
	m.streams[frame.StreamID] = stream
	m.streamsMu.Unlock()

	// Send ACK
	ack := &Frame{
		Type:     FrameTypeAck,
		StreamID: frame.StreamID,
	}
	m.send(ack)

	if m.onNewStream != nil {
		m.onNewStream(stream)
	}
}

func (m *StreamManager) handleData(frame *Frame) {
	m.streamsMu.RLock()
	stream := m.streams[frame.StreamID]
	m.streamsMu.RUnlock()

	if stream == nil {
		return
	}

	// Non-blocking send to pending queue - this keeps the read pump responsive
	// for ping/pong handling. The worker goroutine handles backpressure.
	select {
	case stream.pendingData <- frame.Data:
	case <-stream.closed:
	default:
		// Pending buffer full - this should be rare with 100k buffer
		// Drop the frame to keep read pump alive for ping/pong
		debugLog("WARNING: pendingData full for stream %d, frame dropped (%d bytes)", frame.StreamID, len(frame.Data))
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

	// pendingData is a large buffer for incoming data from the read pump.
	// This allows handleData to be non-blocking, keeping the read pump
	// responsive for ping/pong handling.
	pendingData chan []byte

	// recvChan is the consumer-facing channel that Read() pulls from.
	// The worker goroutine moves data from pendingData to recvChan.
	recvChan chan []byte

	closed     chan struct{}
	closeOnce  sync.Once
	closedOnce sync.Once
}

// newStream creates a new stream and starts its worker goroutine
func newStream(id uint32, manager *StreamManager, serviceName string) *Stream {
	s := &Stream{
		id:          id,
		manager:     manager,
		serviceName: serviceName,
		pendingData: make(chan []byte, 100000), // Large buffer for burst traffic
		recvChan:    make(chan []byte, 4096),   // Consumer buffer
		closed:      make(chan struct{}),
	}

	// Start worker goroutine that moves data from pendingData to recvChan.
	// This applies backpressure (blocks) when recvChan is full, without
	// blocking the read pump.
	go s.dataWorker()

	return s
}

// dataWorker moves data from pendingData to recvChan.
// It blocks when recvChan is full, applying backpressure.
func (s *Stream) dataWorker() {
	for {
		select {
		case data, ok := <-s.pendingData:
			if !ok {
				return
			}
			// Blocking send to recvChan - this is where backpressure happens
			select {
			case s.recvChan <- data:
			case <-s.closed:
				return
			}
		case <-s.closed:
			// Drain remaining pending data before exiting
			for {
				select {
				case data := <-s.pendingData:
					select {
					case s.recvChan <- data:
					default:
						return
					}
				default:
					return
				}
			}
		}
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

// Read reads data from the stream
func (s *Stream) Read(p []byte) (int, error) {
	// First check if there's buffered data, even if stream is closed
	select {
	case data, ok := <-s.recvChan:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		return n, nil
	default:
	}

	// No buffered data, wait for data or close
	select {
	case data, ok := <-s.recvChan:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		return n, nil
	case <-s.closed:
		// Check one more time for buffered data before returning EOF
		select {
		case data, ok := <-s.recvChan:
			if ok {
				n := copy(p, data)
				return n, nil
			}
		default:
		}
		return 0, io.EOF
	}
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
