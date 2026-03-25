package pathfinder

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
)

// Tunnel listens on a local port and proxies connections to a remote service
type Tunnel struct {
	localPort   int
	serviceName string
	manager     *StreamManager
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	actualPort  int

	// Track active connections for cleanup
	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
}

// NewTunnel creates a new tunnel for the given service
func NewTunnel(localPort int, serviceName string, manager *StreamManager) *Tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	return &Tunnel{
		localPort:   localPort,
		serviceName: serviceName,
		manager:     manager,
		ctx:         ctx,
		cancel:      cancel,
		conns:       make(map[net.Conn]struct{}),
	}
}

// Start begins listening for connections on the local port
// If localPort is 0, an available port is automatically assigned
func (t *Tunnel) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", t.localPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	t.listener = listener

	// Get the actual port (useful when localPort was 0)
	t.actualPort = listener.Addr().(*net.TCPAddr).Port

	debugLog("Tunnel started: listening on 127.0.0.1:%d for service %q", t.actualPort, t.serviceName)

	// Accept connections in background
	t.wg.Add(1)
	go t.acceptLoop()

	return nil
}

// Port returns the actual port the tunnel is listening on
func (t *Tunnel) Port() int {
	return t.actualPort
}

// Stop gracefully shuts down the tunnel
func (t *Tunnel) Stop() error {
	t.cancel()
	if t.listener != nil {
		t.listener.Close()
	}

	// Close all active connections to unblock io.Copy goroutines
	t.connsMu.Lock()
	for conn := range t.conns {
		conn.Close()
	}
	t.connsMu.Unlock()

	t.wg.Wait()
	debugLog("Tunnel stopped for service %q", t.serviceName)
	return nil
}

func (t *Tunnel) acceptLoop() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.ctx.Done():
				return
			default:
				debugLog("Tunnel accept error: %v", err)
				return
			}
		}

		t.wg.Add(1)
		go t.handleConn(conn)
	}
}

func (t *Tunnel) handleConn(conn net.Conn) {
	defer t.wg.Done()

	// Track connection for cleanup on Stop()
	t.connsMu.Lock()
	t.conns[conn] = struct{}{}
	t.connsMu.Unlock()

	defer func() {
		conn.Close()
		t.connsMu.Lock()
		delete(t.conns, conn)
		t.connsMu.Unlock()
	}()

	remoteAddr := conn.RemoteAddr().String()
	debugLog("Tunnel: new connection from %s for service %q", remoteAddr, t.serviceName)

	// Open a new stream to the remote service
	stream, err := t.manager.OpenStream(t.serviceName)
	if err != nil {
		debugLog("Tunnel: failed to open stream for %s: %v", remoteAddr, err)
		return
	}

	debugLog("Tunnel: stream %d opened for connection from %s", stream.ID(), remoteAddr)

	// Bidirectional copy between TCP connection and stream
	done := make(chan struct{})
	var closeOnce sync.Once
	signalDone := func() {
		closeOnce.Do(func() { close(done) })
	}

	// TCP → Stream
	go func() {
		n, err := io.Copy(stream, conn)
		debugLog("Tunnel: TCP→Stream finished for %s: bytes=%d, err=%v", remoteAddr, n, err)
		stream.Close() // Close stream to unblock the other direction
		signalDone()
	}()

	// Stream → TCP
	go func() {
		n, err := io.Copy(conn, stream)
		debugLog("Tunnel: Stream→TCP finished for %s: bytes=%d, err=%v", remoteAddr, n, err)
		// Close TCP write side to signal EOF
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
		signalDone()
	}()

	<-done
	debugLog("Tunnel: connection from %s closed", remoteAddr)
}
