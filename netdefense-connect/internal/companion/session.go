package companion

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

// Tunnel is the encrypted path to the device.
type Tunnel interface {
	// Dial opens the session's connection to the device. It may be called
	// again after it fails.
	Dial(ctx context.Context) (net.Conn, error)
	// Direct asks the device for an answer and reports whether it came peer
	// to peer rather than through the relay.
	Direct(ctx context.Context) (bool, error)
	// Drain waits until closed connections have sent what they had left.
	Drain(ctx context.Context) error
	Close() error
}

// Options is what the process hands Run.
type Options struct {
	// Version is the release this binary was built as.
	Version string
	// Stdin, Stdout and Events are the three pipes to the parent. Nothing
	// else may write to the last two.
	Stdin  io.Reader
	Stdout io.Writer
	Events io.Writer
	// Terminate, when it yields, ends the session as terminated.
	Terminate <-chan struct{}

	newTunnel  func(cfg *Config, logf func(string, ...any)) (Tunnel, error)
	probeRelay func(ctx context.Context, relay Relay) bool
	timing     *timing
}

// timing holds every wait of a session, so tests can shorten them.
type timing struct {
	// dialRetry is the pause between two attempts to reach the device.
	dialRetry time.Duration
	// The path is checked every pathPollFast for pathFastWindow after the
	// session connects, while a direct path is still being found, and every
	// pathPollSlow afterwards. One check waits at most pathPollTimeout.
	pathPollFast    time.Duration
	pathFastWindow  time.Duration
	pathPollSlow    time.Duration
	pathPollTimeout time.Duration
	// probe bounds the check of the relay after the device did not answer.
	probe time.Duration
	// lastWords is how long a session this end closes waits for the device
	// to close its side; drain bounds the wait for the last segments to
	// leave; shutdown bounds the whole teardown.
	lastWords time.Duration
	drain     time.Duration
	shutdown  time.Duration
}

func defaultTiming() *timing {
	return &timing{
		dialRetry:       500 * time.Millisecond,
		pathPollFast:    5 * time.Second,
		pathFastWindow:  time.Minute,
		pathPollSlow:    20 * time.Second,
		pathPollTimeout: 5 * time.Second,
		probe:           5 * time.Second,
		lastWords:       2 * time.Second,
		drain:           2 * time.Second,
		shutdown:        6 * time.Second,
	}
}

// copyBufferSize is the size of each direction's copy buffer.
const copyBufferSize = 32 * 1024

// Run is the whole program: it says hello, reads the configuration, reaches
// the device and carries the session until one side ends it. It returns the
// process's exit status: 0 after a closed event, or when the parent left
// before sending a configuration; 1 after an error event.
func Run(opts Options) int {
	if opts.newTunnel == nil {
		opts.newTunnel = newLibraryTunnel
	}
	if opts.probeRelay == nil {
		opts.probeRelay = relayAcceptsConnections
	}
	if opts.timing == nil {
		opts.timing = defaultTiming()
	}
	events := &emitter{w: opts.Events}
	events.emit(Event{Event: EventHello, Version: opts.Version, Protocol: Protocol})

	// The read waits for as long as the parent takes; a request to stop does
	// not.
	type read struct {
		cfg *Config
		err error
	}
	configured := make(chan read, 1)
	go func() {
		cfg, err := ReadConfig(opts.Stdin)
		configured <- read{cfg, err}
	}()
	var cfg *Config
	var err error
	select {
	case r := <-configured:
		cfg, err = r.cfg, r.err
	case <-opts.Terminate:
		events.emit(Event{Event: EventClosed, Reason: ReasonTerminated})
		return 0
	}
	if errors.Is(err, io.EOF) {
		return 0
	}
	if err != nil {
		ev := Event{Event: EventError, Reason: ReasonBadConfig}
		var refused *ConfigError
		if errors.As(err, &refused) {
			ev.Detail = refused.Detail
		}
		events.emit(ev)
		return 1
	}

	s := &session{opts: opts, events: events, timing: opts.timing, relay: cfg.Relay}
	s.connectTimeout = time.Duration(cfg.ConnectTimeoutSeconds) * time.Second

	logf := func(string, ...any) {}
	if cfg.Debug {
		logf = events.logf
		previous := log.Writer()
		log.SetOutput(logWriter{events})
		defer log.SetOutput(previous)
	}
	s.logf = logf

	tunnel, err := opts.newTunnel(cfg, logf)
	cfg.wipe()
	if err != nil {
		logf("tunnel setup: %v", err)
		events.emit(Event{Event: EventError, Reason: ReasonInternal})
		return 1
	}
	return s.run(tunnel)
}

type session struct {
	opts           Options
	events         *emitter
	timing         *timing
	relay          Relay
	connectTimeout time.Duration
	logf           func(string, ...any)
}

func (s *session) run(tunnel Tunnel) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := startPump(s.opts.Stdin)

	conn, end := s.dial(ctx, tunnel, in)
	if conn == nil {
		in.abandon()
		s.events.emit(end)
		s.shutdown(tunnel, nil, nil)
		if end.Event == EventError {
			return 1
		}
		return 0
	}
	s.events.emit(Event{Event: EventConnected})
	in.attach(conn)

	// deviceDone yields why the copy from the device stopped, and is then
	// closed.
	deviceDone := make(chan string, 1)
	go func() {
		deviceDone <- copyOut(s.opts.Stdout, conn)
		close(deviceDone)
	}()
	go s.watchPath(ctx, tunnel)

	// stillOpen is the device's side, for as long as it has not closed it.
	var stillOpen <-chan string = deviceDone
	var reason string
	select {
	case writeFailed := <-in.done:
		reason = ReasonParentClosed
		if writeFailed {
			reason = ReasonPeerClosed
		}
	case reason = <-deviceDone:
		stillOpen = nil
	case <-s.opts.Terminate:
		reason = ReasonTerminated
	}
	cancel()
	s.events.emit(Event{Event: EventClosed, Reason: reason})
	s.shutdown(tunnel, conn, stillOpen)
	return 0
}

// dial keeps trying to reach the device until it answers, the connect timeout
// passes, or the parent or the system ends the wait. It returns the
// connection, or the event that ends the session.
func (s *session) dial(ctx context.Context, tunnel Tunnel, in *pump) (net.Conn, Event) {
	dialCtx, stop := context.WithTimeout(ctx, s.connectTimeout)
	defer stop()

	result := make(chan net.Conn, 1)
	go func() {
		for {
			conn, err := tunnel.Dial(dialCtx)
			if err == nil {
				result <- conn
				return
			}
			s.logf("reaching the device: %v", err)
			select {
			case <-dialCtx.Done():
				result <- nil
				return
			case <-time.After(s.timing.dialRetry):
			}
		}
	}()

	// abandon stops the attempts and closes a connection that one of them
	// still returns.
	abandon := func() {
		stop()
		go func() {
			if conn := <-result; conn != nil {
				_ = conn.Close()
			}
		}()
	}

	select {
	case conn := <-result:
		if conn != nil {
			return conn, Event{}
		}
	case <-in.done:
		abandon()
		return nil, Event{Event: EventClosed, Reason: ReasonParentClosed}
	case <-s.opts.Terminate:
		abandon()
		return nil, Event{Event: EventClosed, Reason: ReasonTerminated}
	}

	// The device did not answer. Whether the relay can be reached tells the
	// parent which of the two failed; the parent leaving, or a request to
	// stop, still ends the wait.
	probeCtx, done := context.WithTimeout(ctx, s.timing.probe)
	defer done()
	reachable := make(chan bool, 1)
	go func() { reachable <- s.opts.probeRelay(probeCtx, s.relay) }()
	select {
	case up := <-reachable:
		if up {
			return nil, Event{Event: EventError, Reason: ReasonNoAnswer}
		}
		return nil, Event{Event: EventError, Reason: ReasonRelayUnreachable}
	case <-in.done:
		return nil, Event{Event: EventClosed, Reason: ReasonParentClosed}
	case <-s.opts.Terminate:
		return nil, Event{Event: EventClosed, Reason: ReasonTerminated}
	}
}

// watchPath reports the path each time it changes. A check that is not
// answered says nothing: whether the device is still there is for the two
// ends of the session to find out, which exchange frames over it.
func (s *session) watchPath(ctx context.Context, tunnel Tunnel) {
	var (
		reported  string
		connected = time.Now()
		wait      time.Duration
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		checkCtx, done := context.WithTimeout(ctx, s.timing.pathPollTimeout)
		direct, err := tunnel.Direct(checkCtx)
		done()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.logf("checking the path: %v", err)
		} else {
			path := PathRelayed
			if direct {
				path = PathDirect
			}
			if path != reported {
				reported = path
				s.events.emit(Event{Event: EventPath, Path: path})
			}
		}

		wait = s.timing.pathPollSlow
		if time.Since(connected) < s.timing.pathFastWindow {
			wait = s.timing.pathPollFast
		}
	}
}

// shutdown ends the connection and the tunnel, in at most the shutdown time.
// When this end is the one leaving (stillOpen is the device's side, not nil),
// it first closes its own side only, and gives the device a moment to send
// what it still has and close: the device then sees the session end at once,
// rather than at its next timeout. The last segments get a moment to leave
// either way.
func (s *session) shutdown(tunnel Tunnel, conn net.Conn, stillOpen <-chan string) {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if conn != nil {
			if half, ok := conn.(interface{ CloseWrite() error }); ok && stillOpen != nil {
				_ = half.CloseWrite()
				select {
				case <-stillOpen:
				case <-time.After(s.timing.lastWords):
				}
			}
			_ = conn.Close()
			ctx, done := context.WithTimeout(context.Background(), s.timing.drain)
			_ = tunnel.Drain(ctx)
			done()
		}
		_ = tunnel.Close()
	}()
	select {
	case <-finished:
	case <-time.After(s.timing.shutdown):
	}
}

// pump copies stdin to the device. It reads stdin from the start, before the
// device is reached, so that the parent closing it is seen at once; bytes
// that arrive early wait for the connection.
type pump struct {
	conn chan net.Conn
	// done yields once: false when stdin ended, true when a write to the
	// device failed.
	done chan bool
}

func startPump(stdin io.Reader) *pump {
	p := &pump{conn: make(chan net.Conn, 1), done: make(chan bool, 1)}
	go func() {
		buf := make([]byte, copyBufferSize)
		var conn net.Conn
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				if conn == nil {
					c, ok := <-p.conn
					if !ok {
						return
					}
					conn = c
				}
				if _, werr := conn.Write(buf[:n]); werr != nil {
					p.done <- true
					return
				}
			}
			if err != nil {
				p.done <- false
				return
			}
		}
	}()
	return p
}

// attach gives the pump the connection to write to.
func (p *pump) attach(conn net.Conn) { p.conn <- conn }

// abandon tells the pump no connection will come.
func (p *pump) abandon() { close(p.conn) }

// copyOut copies the device's bytes to stdout and returns why it stopped.
func copyOut(stdout io.Writer, conn net.Conn) string {
	buf := make([]byte, copyBufferSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := stdout.Write(buf[:n]); werr != nil {
				return ReasonParentClosed
			}
		}
		if err != nil {
			return ReasonPeerClosed
		}
	}
}
