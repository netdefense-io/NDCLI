package pathfinder

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/netdefense-io/NDCLI/internal/sanitize"
)

// RelayClient is a WebSocket client that connects to the Pathfinder relay server
type RelayClient struct {
	serverURL  string
	sessionKey string
	role       string
	sslVerify  bool

	conn       *websocket.Conn
	sendChan   chan []byte
	binaryChan chan []byte

	// Connection state
	connected   bool
	connectedMu sync.RWMutex

	// Registration state
	registered   bool
	registeredMu sync.RWMutex
	peerOnline   bool

	// Paired state - when true, switch to binary relay mode
	paired   bool
	pairedMu sync.RWMutex

	// Binary frame handler
	onFrame   func([]byte)
	onFrameMu sync.RWMutex

	// Message handlers for JSON messages
	handlers   map[MessageType][]func(*Message)
	handlersMu sync.RWMutex

	// Stream manager reference for closing all streams on disconnect
	streamManager   *StreamManager
	streamManagerMu sync.RWMutex

	// endReason records why the session ended (first writer wins); done is
	// closed at the same time.
	endMu     sync.Mutex
	endReason string
	done      chan struct{}
	doneOnce  sync.Once

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// RateLimitedError is returned when the relay refuses the WebSocket upgrade
// with HTTP 429.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("relay is rate limiting new sessions (HTTP 429); retry in %ds", RetryAfterSeconds(e.RetryAfter))
	}
	return "relay is rate limiting new sessions (HTTP 429); retry in a few seconds"
}

// RelayBusyError is returned when the relay refuses the WebSocket upgrade
// with HTTP 503. With a Retry-After, its cap on connections that have not
// registered yet is reached; without one, the relay is restarting or
// something in front of it failed.
type RelayBusyError struct {
	RetryAfter time.Duration
}

func (e *RelayBusyError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("relay is refusing new connections for now (HTTP 503); retry in %ds", RetryAfterSeconds(e.RetryAfter))
	}
	return "relay is unavailable (HTTP 503)"
}

// maxRelayRetryAfter bounds a relay Retry-After.
const maxRelayRetryAfter = 24 * time.Hour

// parseRetryAfter reads a Retry-After header given either as seconds or as an
// HTTP date. It returns 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	// Both forms are capped at maxRelayRetryAfter, as the API client caps
	// them: the seconds before multiplying, so an absurd value cannot wrap to
	// a short wait, and a far-off date so it cannot reach the saturated
	// duration that RetryAfterSeconds would overflow on.
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
		if secs < 0 || strings.HasPrefix(v, "-") {
			return 0
		}
		if err != nil || secs > int64(maxRelayRetryAfter/time.Second) {
			return maxRelayRetryAfter
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return min(d, maxRelayRetryAfter)
		}
	}
	return 0
}

const maxReasonRunes = 200

// cleanReason makes relay-supplied text safe to print on a terminal: control
// characters (escape sequences included) are removed and it is cut to one
// line of bounded length.
func cleanReason(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, sanitize.String(s))
	if r := []rune(s); len(r) > maxReasonRunes {
		s = string(r[:maxReasonRunes])
	}
	return strings.TrimSpace(s)
}

// closeReasonText turns a relay close frame into a user-facing reason.
func closeReasonText(code int, text string) string {
	if text = cleanReason(text); text != "" {
		return text
	}
	switch code {
	case websocket.CloseNormalClosure:
		return "session closed by the relay"
	case websocket.CloseGoingAway:
		return "relay is restarting"
	}
	return fmt.Sprintf("relay closed the connection (code %d)", code)
}

// Done is closed when the relay session has ended for any reason.
func (c *RelayClient) Done() <-chan struct{} { return c.done }

// EndReason returns why the session ended, or "" while it is still running.
func (c *RelayClient) EndReason() string {
	c.endMu.Lock()
	defer c.endMu.Unlock()
	return c.endReason
}

func (c *RelayClient) recordEnd(reason string) {
	c.endMu.Lock()
	if c.endReason == "" {
		c.endReason = reason
	}
	c.endMu.Unlock()
}

// markEnded records the reason (first one wins) and signals Done.
func (c *RelayClient) markEnded(reason string) {
	c.recordEnd(reason)
	c.doneOnce.Do(func() { close(c.done) })
}

// NewRelayClient creates a new relay client
func NewRelayClient(serverURL, sessionKey string, sslVerify bool) *RelayClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &RelayClient{
		serverURL:  serverURL,
		sessionKey: sessionKey,
		role:       "client",
		sslVerify:  sslVerify,
		sendChan:   make(chan []byte, 1024),
		binaryChan: make(chan []byte, 4096),
		handlers:   make(map[MessageType][]func(*Message)),
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Connect establishes connection to the relay server
func (c *RelayClient) Connect() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	if !c.sslVerify {
		dialer.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	headers := http.Header{}
	conn, resp, err := dialer.DialContext(c.ctx, c.serverURL, headers)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return &RateLimitedError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
		}
		if resp != nil && resp.StatusCode == http.StatusServiceUnavailable {
			return &RelayBusyError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn
	c.setConnected(true)

	// Start read/write pumps
	c.wg.Add(2)
	go c.writePump()
	go c.readPump()

	// Register with the server
	if err := c.register(); err != nil {
		c.Close()
		return fmt.Errorf("registration failed: %w", err)
	}

	return nil
}

func (c *RelayClient) register() error {
	payload := RegisterPayload{
		SessionKey: c.sessionKey,
		Role:       c.role,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msg := &Message{
		Type:    MsgTypeRegister,
		Payload: payloadBytes,
	}

	return c.sendJSON(msg)
}

func (c *RelayClient) sendJSON(msg *Message) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case c.sendChan <- data:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendFrame sends a binary frame to the paired peer via the relay server
func (c *RelayClient) SendFrame(data []byte) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	if !c.IsPaired() {
		return fmt.Errorf("not paired")
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	select {
	case c.binaryChan <- dataCopy:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("send buffer full")
	}
}

// OnFrame sets the callback for receiving binary frames
func (c *RelayClient) OnFrame(handler func([]byte)) {
	c.onFrameMu.Lock()
	c.onFrame = handler
	c.onFrameMu.Unlock()
}

// OnMessage registers a handler for a specific message type
func (c *RelayClient) OnMessage(msgType MessageType, handler func(*Message)) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[msgType] = append(c.handlers[msgType], handler)
}

// IsConnected returns whether the client is connected
func (c *RelayClient) IsConnected() bool {
	c.connectedMu.RLock()
	defer c.connectedMu.RUnlock()
	return c.connected
}

func (c *RelayClient) setConnected(connected bool) {
	c.connectedMu.Lock()
	c.connected = connected
	c.connectedMu.Unlock()
}

// IsRegistered returns whether the client is registered
func (c *RelayClient) IsRegistered() bool {
	c.registeredMu.RLock()
	defer c.registeredMu.RUnlock()
	return c.registered
}

func (c *RelayClient) setRegistered(registered bool) {
	c.registeredMu.Lock()
	c.registered = registered
	c.registeredMu.Unlock()
}

// IsPeerOnline returns whether the other peer is online
func (c *RelayClient) IsPeerOnline() bool {
	c.registeredMu.RLock()
	defer c.registeredMu.RUnlock()
	return c.peerOnline
}

func (c *RelayClient) setPeerOnline(online bool) {
	c.registeredMu.Lock()
	c.peerOnline = online
	c.registeredMu.Unlock()
}

// IsPaired returns whether the client is paired and in binary relay mode
func (c *RelayClient) IsPaired() bool {
	c.pairedMu.RLock()
	defer c.pairedMu.RUnlock()
	return c.paired
}

func (c *RelayClient) setPaired(paired bool) {
	c.pairedMu.Lock()
	c.paired = paired
	c.pairedMu.Unlock()
}

// Close closes the connection
func (c *RelayClient) Close() error {
	c.cancel() // Signal writePump to send close frame and exit
	c.setConnected(false)
	c.setRegistered(false)
	c.setPaired(false)

	// Don't wait the full 60 s read deadline for a peer that never answers
	// our close frame.
	if c.conn != nil {
		c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	}

	c.wg.Wait() // Wait for pumps to finish (writePump sends close frame)

	if c.conn != nil {
		c.conn.Close() // Now safe to close underlying connection
	}

	return nil
}

// SessionKey returns the client's session key
func (c *RelayClient) SessionKey() string {
	return c.sessionKey
}

// Context returns the client's context for cancellation propagation
func (c *RelayClient) Context() context.Context {
	return c.ctx
}

// SetStreamManager sets the stream manager reference so the relay can close
// all streams when the connection dies
func (c *RelayClient) SetStreamManager(mgr *StreamManager) {
	c.streamManagerMu.Lock()
	c.streamManager = mgr
	c.streamManagerMu.Unlock()
}

// closeAllStreams closes all streams when the relay connection dies
func (c *RelayClient) closeAllStreams() {
	c.streamManagerMu.RLock()
	mgr := c.streamManager
	c.streamManagerMu.RUnlock()

	if mgr != nil {
		debugLog("Closing all streams due to relay disconnect")
		mgr.CloseAll()
	}
}

func (c *RelayClient) writePump() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Send close frame before exiting
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			debugLog("writePump: sent close frame, exiting")
			return

		case message := <-c.sendChan:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				debugLog("writePump: text write error: %v", err)
				return
			}

		case data := <-c.binaryChan:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				debugLog("writePump: binary write error: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				debugLog("writePump: ping write error: %v", err)
				return
			}
		}
	}
}

func (c *RelayClient) readPump() {
	defer c.wg.Done()
	defer func() {
		// The end reason must be recorded before the streams close, so a
		// reader woken by the close already sees it.
		c.recordEnd("connection to the relay was lost")
		c.setConnected(false)
		c.closeAllStreams()
		c.markEnded("connection to the relay was lost")
	}()

	c.conn.SetReadLimit(maxFrameDataLen) // matches DecodeFrame's cap on a single frame payload
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Handle pongs from server (response to our pings)
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Handle pings from server
	c.conn.SetPingHandler(func(appData string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		// Send pong response
		c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return c.conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	// Handle close frames from server
	c.conn.SetCloseHandler(func(code int, text string) error {
		debugLog("Received close frame: code=%d, text=%s", code, text)
		c.recordEnd(closeReasonText(code, text))
		return nil
	})

	for {
		select {
		case <-c.ctx.Done():
			debugLog("readPump: context cancelled")
			return
		default:
		}

		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				debugLog("readPump: unexpected close error: %v", err)
			} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				debugLog("readPump: normal close: %v", err)
			} else {
				debugLog("readPump: read error: %v", err)
			}
			return
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Handle binary messages (relay mode)
		if msgType == websocket.BinaryMessage {
			c.onFrameMu.RLock()
			handler := c.onFrame
			c.onFrameMu.RUnlock()

			if handler != nil {
				handler(message)
			}
			continue
		}

		// Handle JSON messages
		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		c.dispatchMessage(&msg)
	}
}

func (c *RelayClient) dispatchMessage(msg *Message) {
	// Handle registration response
	if msg.Type == MsgTypeRegistered {
		var payload RegisteredPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			c.setRegistered(true)
			c.setPeerOnline(payload.PeerOnline)
		}
	}

	// Handle paired notification - switch to binary mode
	if msg.Type == MsgTypePaired {
		c.setPaired(true)
		c.setPeerOnline(true)
	}

	// Handle peer ready notification
	if msg.Type == MsgTypePeerReady {
		c.setPeerOnline(true)
	}

	// Handle peer offline notification
	if msg.Type == MsgTypePeerOffline {
		wasPaired := c.IsPaired()
		c.setPeerOnline(false)
		c.setPaired(false)
		if wasPaired {
			c.recordEnd("device went offline")
			c.setConnected(false)
			c.closeAllStreams()
			c.markEnded("device went offline")
		}
	}

	c.handlersMu.RLock()
	handlers := c.handlers[msg.Type]
	c.handlersMu.RUnlock()

	for _, handler := range handlers {
		handler(msg)
	}
}

// WaitForRegistration waits for registration confirmation
func (c *RelayClient) WaitForRegistration(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if c.IsRegistered() {
			return nil
		}

		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	return fmt.Errorf("registration timeout")
}

// WaitForPairing waits for the connection to be paired
func (c *RelayClient) WaitForPairing(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if c.IsPaired() {
			return nil
		}

		if !c.IsConnected() {
			return fmt.Errorf("connection closed")
		}

		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	return fmt.Errorf("pairing timeout: device did not connect")
}
