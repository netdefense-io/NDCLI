package pathfinder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
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

// NewTunnelOn creates a tunnel that serves on a listener already bound.
func NewTunnelOn(listener net.Listener, serviceName string, manager *StreamManager) *Tunnel {
	t := NewTunnel(0, serviceName, manager)
	t.listener = listener
	return t
}

// Start begins listening for connections on the local port, or on the
// listener NewTunnelOn was given. If localPort is 0, an available port is
// automatically assigned
func (t *Tunnel) Start() error {
	if t.listener == nil {
		addr := fmt.Sprintf("127.0.0.1:%d", t.localPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		t.listener = listener
	}
	listener := t.listener

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

	s := &tunnelConn{
		tunnel:     t,
		conn:       conn,
		br:         bufio.NewReaderSize(conn, 32<<10),
		remoteAddr: remoteAddr,
		done:       make(chan struct{}),
	}
	go s.forwardRequests()
	<-s.done
	debugLog("Tunnel: connection from %s closed", remoteAddr)
}

// tunnelConn is one browser connection. Each request is checked
// (requestGate) before any of it reaches the device; the stream is opened by
// the first request let through.
type tunnelConn struct {
	tunnel     *Tunnel
	conn       net.Conn
	br         *bufio.Reader
	remoteAddr string

	stream     *Stream
	copierDone chan struct{}
	refusing   atomic.Bool

	done      chan struct{}
	closeOnce sync.Once
}

func (c *tunnelConn) finish() { c.closeOnce.Do(func() { close(c.done) }) }

func (c *tunnelConn) forwardRequests() {
	defer c.finish()
	defer func() {
		if c.stream != nil {
			_ = c.stream.Close()
		}
	}()

	gate := newRequestGate(c.tunnel.actualPort)
	for {
		head, err := readRequestHead(c.br)
		if err == errHeadTooLarge {
			c.refuse(&gateRefusal{status: http.StatusRequestHeaderFieldsTooLarge, body: "The request headers are too large."})
			return
		}
		if err != nil {
			debugLog("Tunnel: TCP→Stream finished for %s: %v", c.remoteAddr, err)
			return
		}
		req, err := parseRequestHead(head)
		if err != nil {
			debugLog("Tunnel: unreadable request from %s: %v", c.remoteAddr, err)
			c.refuse(&gateRefusal{status: http.StatusBadRequest, body: "The request could not be read."})
			return
		}
		if refusal := gate.check(req); refusal != nil {
			refusal.head = req.Method == http.MethodHead
			debugLog("Tunnel: refused %d %s", refusal.status, describeRequest(req))
			c.refuse(refusal)
			return
		}
		debugLog("Tunnel: forwarding %s", describeRequest(req))

		if c.stream == nil && !c.openStream() {
			return
		}
		if _, err := c.stream.Write(head); err != nil {
			return
		}
		if err := copyRequestBody(c.stream, c.br, req); err != nil {
			debugLog("Tunnel: request body from %s: %v", c.remoteAddr, err)
			return
		}
	}
}

func (c *tunnelConn) openStream() bool {
	stream, err := c.tunnel.manager.OpenStream(c.tunnel.serviceName)
	if err != nil {
		debugLog("Tunnel: failed to open stream for %s: %v", c.remoteAddr, err)
		c.refuse(&gateRefusal{status: http.StatusBadGateway, body: "The device session is not available."})
		return false
	}
	debugLog("Tunnel: stream %d opened for connection from %s", stream.ID(), c.remoteAddr)
	c.stream = stream
	c.copierDone = make(chan struct{})
	go c.copyReplies()
	return true
}

// copyReplies hands the device's replies to the browser. A stream the tunnel
// closed to refuse a request leaves the connection to refuse.
func (c *tunnelConn) copyReplies() {
	defer close(c.copierDone)
	n, err := io.Copy(c.conn, c.stream)
	debugLog("Tunnel: Stream→TCP finished for %s: bytes=%d, err=%v", c.remoteAddr, n, err)
	if c.refusing.Load() {
		return
	}
	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	c.finish()
}

// refuse answers a request the tunnel does not forward and ends the
// connection. The stream is closed and the reply copier waited for first, so
// the refusal is not written into a reply the copier is writing (a reply the
// device had not finished is cut short); what the client still sends is read
// for a moment, so that it reads the refusal rather than a reset.
func (c *tunnelConn) refuse(r *gateRefusal) {
	if c.stream != nil {
		c.refusing.Store(true)
		_ = c.conn.SetWriteDeadline(time.Now().Add(refusalDrain))
		_ = c.stream.Close()
		<-c.copierDone
		_ = c.conn.SetWriteDeadline(time.Now().Add(refusalDrain))
	}
	if writeRefusal(c.conn, r) != nil {
		return
	}
	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(refusalDrain))
	_, _ = io.CopyN(io.Discard, c.br, maxRefusalDrain)
}

const (
	refusalDrain    = 2 * time.Second
	maxRefusalDrain = 1 << 20
)
