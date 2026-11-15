package companion

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTunnel stands in for the tunnel library.
type fakeTunnel struct {
	dial   func(ctx context.Context) (net.Conn, error)
	direct func(ctx context.Context) (bool, error)

	dials   atomic.Int32
	drained atomic.Bool
	closed  atomic.Bool
}

func (f *fakeTunnel) Dial(ctx context.Context) (net.Conn, error) {
	f.dials.Add(1)
	return f.dial(ctx)
}

func (f *fakeTunnel) Direct(ctx context.Context) (bool, error) {
	if f.direct == nil {
		return false, nil
	}
	return f.direct(ctx)
}

func (f *fakeTunnel) Drain(context.Context) error { f.drained.Store(true); return nil }
func (f *fakeTunnel) Close() error                { f.closed.Store(true); return nil }

// fastTiming shortens every wait of a session.
func fastTiming() *timing {
	return &timing{
		dialRetry:       time.Millisecond,
		pathPollFast:    5 * time.Millisecond,
		pathFastWindow:  time.Hour,
		pathPollSlow:    5 * time.Millisecond,
		pathPollTimeout: 100 * time.Millisecond,
		probe:           time.Second,
		lastWords:       100 * time.Millisecond,
		drain:           100 * time.Millisecond,
		shutdown:        2 * time.Second,
	}
}

// rawKey is 32 distinct bytes, in no particular form.
func rawKey(b byte) Key {
	var k Key
	for i := range k {
		k[i] = b + byte(i)
	}
	return k
}

// testKey is a key in the clamped form a tunnel key has.
func testKey(b byte) Key {
	k := rawKey(b)
	k[0] &= 248
	k[31] = k[31]&127 | 64
	return k
}

func validConfig() Config {
	return Config{
		Relay:                 Relay{Hostname: "relay.example.test", Port: 443, STUNPort: 3478},
		Port:                  4711,
		ClientKey:             testKey(1),
		ServerKey:             testKey(101),
		PresharedKey:          testKey(201),
		ConnectTimeoutSeconds: 30,
	}
}

// message frames body as the parent does: a 4-byte length, then the bytes.
func message(body []byte) []byte {
	out := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(out, uint32(len(body)))
	return append(out, body...)
}

func configMessage(t *testing.T, cfg Config) []byte {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return message(body)
}

// run is one session under test.
type run struct {
	t      *testing.T
	stdin  *io.PipeWriter
	events chan Event
	exit   chan int

	mu     sync.Mutex
	stdout bytes.Buffer
	raw    bytes.Buffer // every byte written to the events pipe
}

// start runs a session with opts, whose pipes it supplies.
func start(t *testing.T, opts Options) *run {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	eventsR, eventsW := io.Pipe()
	r := &run{t: t, stdin: stdinW, events: make(chan Event, 256), exit: make(chan int, 1)}

	opts.Stdin = stdinR
	opts.Stdout = writerFunc(func(p []byte) (int, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.stdout.Write(p)
	})
	opts.Events = eventsW
	if opts.Version == "" {
		opts.Version = "1.2.3"
	}
	if opts.timing == nil {
		opts.timing = fastTiming()
	}
	if opts.probeRelay == nil {
		opts.probeRelay = func(context.Context, Relay) bool { return true }
	}

	go func() {
		defer close(r.events)
		sc := bufio.NewScanner(eventsR)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			r.mu.Lock()
			r.raw.Write(sc.Bytes())
			r.raw.WriteByte('\n')
			r.mu.Unlock()
			var ev Event
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				t.Errorf("event line is not JSON: %q", sc.Text())
				continue
			}
			r.events <- ev
		}
	}()
	go func() {
		code := Run(opts)
		_ = eventsW.Close()
		r.exit <- code
	}()
	t.Cleanup(func() { _ = stdinW.Close() })
	return r
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// next returns the next event that is not a log line.
func (r *run) next() Event {
	r.t.Helper()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				r.t.Fatal("the events ended")
			}
			if ev.Event == EventLog {
				continue
			}
			return ev
		case <-time.After(10 * time.Second):
			r.t.Fatal("no event within 10s")
		}
	}
}

func (r *run) expect(want Event) {
	r.t.Helper()
	if got := r.next(); got != want {
		r.t.Fatalf("event = %+v, want %+v", got, want)
	}
}

func (r *run) expectHello() {
	r.t.Helper()
	r.expect(Event{Event: EventHello, Version: "1.2.3", Protocol: Protocol})
}

func (r *run) send(p []byte) {
	r.t.Helper()
	if _, err := r.stdin.Write(p); err != nil {
		r.t.Fatalf("write to stdin: %v", err)
	}
}

func (r *run) exitCode() int {
	r.t.Helper()
	select {
	case code := <-r.exit:
		return code
	case <-time.After(10 * time.Second):
		r.t.Fatal("the session did not end within 10s")
		return -1
	}
}

func (r *run) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stdout.String()
}

func (r *run) rawEvents() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.raw.String()
}

func (r *run) waitOutput(want string) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for r.output() != want {
		if time.Now().After(deadline) {
			r.t.Fatalf("stdout = %q, want %q", r.output(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// echoTunnel answers the session's connection with whatever it is sent.
func echoTunnel() (*fakeTunnel, func() net.Conn) {
	var mu sync.Mutex
	var device net.Conn
	f := &fakeTunnel{}
	f.dial = func(context.Context) (net.Conn, error) {
		ours, theirs := net.Pipe()
		mu.Lock()
		device = theirs
		mu.Unlock()
		go func() { _, _ = io.Copy(theirs, theirs) }()
		return ours, nil
	}
	return f, func() net.Conn {
		mu.Lock()
		defer mu.Unlock()
		return device
	}
}

func withTunnel(f *fakeTunnel) Options {
	return Options{newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) { return f, nil }}
}

func TestHelloComesBeforeAnyKeyIsRead(t *testing.T) {
	r := start(t, withTunnel(&fakeTunnel{}))
	// Nothing has been written to stdin yet.
	r.expectHello()
}

func TestAParentThatSendsNothingGetsNoError(t *testing.T) {
	built := false
	r := start(t, Options{newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) {
		built = true
		return &fakeTunnel{}, nil
	}})
	r.expectHello()
	_ = r.stdin.Close()
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if ev, ok := <-r.events; ok {
		t.Errorf("unexpected event after hello: %+v", ev)
	}
	if built {
		t.Error("a tunnel was built without a configuration")
	}
}

func TestBadConfigurationIsRefused(t *testing.T) {
	edit := func(f func(*Config)) []byte {
		cfg := validConfig()
		f(&cfg)
		return configMessage(t, cfg)
	}
	valid, _ := json.Marshal(validConfig())
	cases := []struct {
		name, detail string
		in           []byte
	}{
		{"length cut short", "the message ended inside its length", []byte{0, 0}},
		{"zero length", "the message length is out of range", []byte{0, 0, 0, 0}},
		{"length over the limit", "the message length is out of range", []byte{0, 0, 0x40, 1}},
		{"body cut short", "the message is shorter than its length", []byte{0, 0, 0, 9, '{'}},
		{"not JSON", "the message is not this version's configuration", message([]byte("hello"))},
		{"unknown field", "the message is not this version's configuration", message([]byte(`{"relay":{},"mode":"x"}`))},
		{"two objects", "the message holds more than one object", message(append(append([]byte{}, valid...), valid...))},
		{"key of the wrong size", "the message is not this version's configuration", message([]byte(`{"client_key":"AAAA"}`))},
		{"no relay host", "relay.hostname is not a host name or an address", edit(func(c *Config) { c.Relay.Hostname = "" })},
		{"relay host with a path", "relay.hostname is not a host name or an address", edit(func(c *Config) { c.Relay.Hostname = "relay.example.test/x" })},
		{"relay host with a space", "relay.hostname is not a host name or an address", edit(func(c *Config) { c.Relay.Hostname = "relay example" })},
		{"relay port zero", "relay.port is not 1 to 65535", edit(func(c *Config) { c.Relay.Port = 0 })},
		{"relay port too high", "relay.port is not 1 to 65535", edit(func(c *Config) { c.Relay.Port = 65536 })},
		{"no STUN port", "relay.stun_port is not 1 to 65535", edit(func(c *Config) { c.Relay.STUNPort = 0 })},
		{"negative port", "port is not 1 to 65535", edit(func(c *Config) { c.Port = -1 })},
		{"no client key", "client_key is missing", edit(func(c *Config) { c.ClientKey = Key{} })},
		{"no server key", "server_key is missing", edit(func(c *Config) { c.ServerKey = Key{} })},
		{"no pre-shared key", "preshared_key is missing", edit(func(c *Config) { c.PresharedKey = Key{} })},
		{"a client key that is not clamped: low bits", "client_key is not a clamped key", edit(func(c *Config) { c.ClientKey[0] |= 1 })},
		{"a client key that is not clamped: top bit", "client_key is not a clamped key", edit(func(c *Config) { c.ClientKey[31] |= 0x80 })},
		{"a device key that is not clamped: second bit", "server_key is not a clamped key", edit(func(c *Config) { c.ServerKey[31] &^= 0x40 })},
		{"one key for both ends", "client_key and server_key are the same key", edit(func(c *Config) { c.ServerKey = c.ClientKey })},
		{"no connect timeout", "connect_timeout_seconds is not 1 to 600", edit(func(c *Config) { c.ConnectTimeoutSeconds = 0 })},
		{"connect timeout too long", "connect_timeout_seconds is not 1 to 600", edit(func(c *Config) { c.ConnectTimeoutSeconds = 601 })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			built := false
			r := start(t, Options{newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) {
				built = true
				return &fakeTunnel{}, nil
			}})
			r.expectHello()
			r.send(tc.in)
			_ = r.stdin.Close()
			r.expect(Event{Event: EventError, Reason: ReasonBadConfig, Detail: tc.detail})
			if code := r.exitCode(); code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if built {
				t.Error("a tunnel was built from a refused configuration")
			}
			if out := r.output(); out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
		})
	}
}

func TestARefusalNeverRepeatsAKey(t *testing.T) {
	cfg := validConfig()
	cfg.Relay.Port = 0
	keys := []Key{cfg.ClientKey, cfg.ServerKey, cfg.PresharedKey}

	r := start(t, withTunnel(&fakeTunnel{}))
	r.expectHello()
	r.send(configMessage(t, cfg))
	r.expect(Event{Event: EventError, Reason: ReasonBadConfig, Detail: "relay.port is not 1 to 65535"})
	r.exitCode()

	for _, k := range keys {
		text, _ := json.Marshal(k)
		if strings.Contains(r.rawEvents(), strings.Trim(string(text), `"`)) {
			t.Fatalf("the events repeat a key: %s", r.rawEvents())
		}
	}
}

func TestValidHostname(t *testing.T) {
	good := []string{"relay.example.test", "relay-1.example.test", "localhost", "192.0.2.7", "2001:db8::7", "a.b", strings.Repeat("a", 63) + ".test"}
	bad := []string{"", ".", "relay..test", "-relay.test", "relay-.test", "relay_1.test", "relay.test:443", "relay.test/", "re lay.test", "relay.test\n", "rélay.test", "fe80::1%eth0", strings.Repeat("a", 64) + ".test", strings.Repeat("a.", 127) + "a"}
	for _, h := range good {
		if !validHostname(h) {
			t.Errorf("validHostname(%q) = false, want true", h)
		}
	}
	for _, h := range bad {
		if validHostname(h) {
			t.Errorf("validHostname(%q) = true, want false", h)
		}
	}
}

func TestASessionCarriesBytesBothWays(t *testing.T) {
	tunnel, _ := echoTunnel()
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})

	r.send([]byte("first "))
	r.send([]byte("second"))
	r.waitOutput("first second")

	_ = r.stdin.Close()
	for ev := r.next(); ev.Event != EventClosed; ev = r.next() {
		if ev.Event != EventPath {
			t.Fatalf("unexpected event %+v", ev)
		}
	}
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !tunnel.drained.Load() || !tunnel.closed.Load() {
		t.Errorf("drained = %v, closed = %v; want the tunnel drained and closed", tunnel.drained.Load(), tunnel.closed.Load())
	}
	if out := r.output(); out != "first second" {
		t.Errorf("stdout = %q, want only the device's bytes", out)
	}
}

func TestBytesSentBeforeTheDeviceAnswersAreNotLost(t *testing.T) {
	release := make(chan struct{})
	tunnel, _ := echoTunnel()
	answer := tunnel.dial
	tunnel.dial = func(ctx context.Context) (net.Conn, error) {
		<-release
		return answer(ctx)
	}
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(append(configMessage(t, validConfig()), "early"...))
	close(release)
	r.expect(Event{Event: EventConnected})
	r.waitOutput("early")
}

func TestTheParentClosingEndsTheSession(t *testing.T) {
	tunnel, _ := echoTunnel()
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	_ = r.stdin.Close()
	for ev := r.next(); ; ev = r.next() {
		if ev.Event == EventPath {
			continue
		}
		if want := (Event{Event: EventClosed, Reason: ReasonParentClosed}); ev != want {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
		break
	}
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestTheDeviceClosingEndsTheSession(t *testing.T) {
	tunnel, device := echoTunnel()
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	_ = device().Close()
	for ev := r.next(); ; ev = r.next() {
		if ev.Event == EventPath {
			continue
		}
		if want := (Event{Event: EventClosed, Reason: ReasonPeerClosed}); ev != want {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
		break
	}
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !tunnel.closed.Load() {
		t.Error("the tunnel was left open")
	}
}

func TestTheParentLeavingWhileTheDeviceIsSoughtStopsTheSearch(t *testing.T) {
	stopped := make(chan struct{})
	tunnel := &fakeTunnel{dial: func(ctx context.Context) (net.Conn, error) {
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}}
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	_ = r.stdin.Close()
	r.expect(Event{Event: EventClosed, Reason: ReasonParentClosed})
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Error("the attempt to reach the device was not cancelled")
	}
	if !tunnel.closed.Load() {
		t.Error("the tunnel was left open")
	}
}

func TestTheDeviceIsSoughtUntilItAnswers(t *testing.T) {
	tunnel, _ := echoTunnel()
	answer := tunnel.dial
	tunnel.dial = func(ctx context.Context) (net.Conn, error) {
		if tunnel.dials.Load() < 4 {
			return nil, errors.New("not yet")
		}
		return answer(ctx)
	}
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	if n := tunnel.dials.Load(); n != 4 {
		t.Errorf("dialed %d times, want 4", n)
	}
}

func TestADeviceThatNeverAnswers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		relayUp   bool
		reason    string
		wantProbe Relay
	}{
		{"relay reachable", true, ReasonNoAnswer, validConfig().Relay},
		{"relay unreachable", false, ReasonRelayUnreachable, validConfig().Relay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tunnel := &fakeTunnel{dial: func(context.Context) (net.Conn, error) { return nil, errors.New("silence") }}
			opts := withTunnel(tunnel)
			var probed Relay
			opts.probeRelay = func(_ context.Context, relay Relay) bool {
				probed = relay
				return tc.relayUp
			}
			cfg := validConfig()
			cfg.ConnectTimeoutSeconds = 1
			r := start(t, opts)
			r.expectHello()
			began := time.Now()
			r.send(configMessage(t, cfg))
			r.expect(Event{Event: EventError, Reason: tc.reason})
			if waited := time.Since(began); waited < time.Second {
				t.Errorf("gave up after %v, before the 1s connect timeout", waited)
			}
			if code := r.exitCode(); code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if probed != tc.wantProbe {
				t.Errorf("probed %+v, want %+v", probed, tc.wantProbe)
			}
			if tunnel.dials.Load() < 2 {
				t.Errorf("dialed %d times, want more than one attempt", tunnel.dials.Load())
			}
			if !tunnel.closed.Load() {
				t.Error("the tunnel was left open")
			}
		})
	}
}

func TestThePathIsReportedWhenItChanges(t *testing.T) {
	answers := []bool{false, false, true, true, true, false}
	var asked atomic.Int32
	tunnel, _ := echoTunnel()
	tunnel.direct = func(context.Context) (bool, error) {
		i := int(asked.Add(1)) - 1
		if i >= len(answers) {
			i = len(answers) - 1
		}
		return answers[i], nil
	}
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	r.expect(Event{Event: EventPath, Path: PathRelayed})
	r.expect(Event{Event: EventPath, Path: PathDirect})
	r.expect(Event{Event: EventPath, Path: PathRelayed})

	// Many more checks have answered the same since; none is an event.
	for asked.Load() < int32(len(answers))+20 {
		time.Sleep(time.Millisecond)
	}
	_ = r.stdin.Close()
	r.expect(Event{Event: EventClosed, Reason: ReasonParentClosed})
}

// Whether the device is still there is for the session's two ends to find
// out, with the frames they exchange. A path check that goes unanswered is
// not that: it says nothing, however long it lasts.
func TestAnUnansweredPathCheckSaysNothing(t *testing.T) {
	var asked atomic.Int32
	tunnel, _ := echoTunnel()
	tunnel.direct = func(context.Context) (bool, error) {
		if asked.Add(1) <= 30 {
			return false, errors.New("no answer")
		}
		return true, nil
	}
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	// Thirty checks go unanswered; the first event after them is the path.
	r.expect(Event{Event: EventPath, Path: PathDirect})
	r.send([]byte("still up"))
	r.waitOutput("still up")
}

// tcpTunnel answers the session's connection over loopback TCP, which can
// close one direction at a time as the tunnel's connection can.
func tcpTunnel(t *testing.T, device func(net.Conn)) *fakeTunnel {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		device(conn)
	}()
	return &fakeTunnel{dial: func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", listener.Addr().String())
	}}
}

// When this end leaves it closes its side first, so the device learns at
// once, and what the device still had to say arrives before the process ends.
func TestThisEndLeavingLetsTheDeviceFinish(t *testing.T) {
	sawEnd := make(chan struct{})
	tunnel := tcpTunnel(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
		close(sawEnd)
		_, _ = io.WriteString(conn, "last words")
		_ = conn.Close()
	})
	opts := withTunnel(tunnel)
	opts.timing = fastTiming()
	opts.timing.lastWords = 5 * time.Second
	r := start(t, opts)
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	_ = r.stdin.Close()

	select {
	case <-sawEnd:
	case <-time.After(5 * time.Second):
		t.Fatal("the device did not see this end close its side")
	}
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if out := r.output(); out != "last words" {
		t.Errorf("stdout = %q, want the device's last words", out)
	}
}

// A device that does not close its side in time does not hold this end up.
func TestADeviceThatDoesNotFinishIsNotWaitedFor(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	tunnel := tcpTunnel(t, func(conn net.Conn) {
		<-release
		_ = conn.Close()
	})
	r := start(t, withTunnel(tunnel))
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventConnected})
	began := time.Now()
	_ = r.stdin.Close()
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if waited := time.Since(began); waited > 2*time.Second {
		t.Errorf("leaving took %v with a 100ms wait for the device", waited)
	}
	if !tunnel.closed.Load() {
		t.Error("the tunnel was left open")
	}
}

func TestTerminateEndsTheSession(t *testing.T) {
	for _, connected := range []bool{false, true} {
		name := "while the device is sought"
		if connected {
			name = "once connected"
		}
		t.Run(name, func(t *testing.T) {
			tunnel, _ := echoTunnel()
			seeking := make(chan struct{})
			if !connected {
				tunnel.dial = func(ctx context.Context) (net.Conn, error) {
					close(seeking)
					<-ctx.Done()
					return nil, ctx.Err()
				}
			}
			terminate := make(chan struct{})
			opts := withTunnel(tunnel)
			opts.Terminate = terminate
			r := start(t, opts)
			r.expectHello()
			r.send(configMessage(t, validConfig()))
			if connected {
				r.expect(Event{Event: EventConnected})
			} else {
				<-seeking
			}
			close(terminate)
			for ev := r.next(); ; ev = r.next() {
				if ev.Event == EventPath {
					continue
				}
				if want := (Event{Event: EventClosed, Reason: ReasonTerminated}); ev != want {
					t.Fatalf("event = %+v, want %+v", ev, want)
				}
				break
			}
			if code := r.exitCode(); code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if !tunnel.closed.Load() {
				t.Error("the tunnel was left open")
			}
		})
	}
}

func TestATunnelThatCannotBeBuilt(t *testing.T) {
	r := start(t, Options{newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) {
		return nil, errors.New("library text that must not reach the parent")
	}})
	r.expectHello()
	r.send(configMessage(t, validConfig()))
	r.expect(Event{Event: EventError, Reason: ReasonInternal})
	if code := r.exitCode(); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if strings.Contains(r.rawEvents(), "library text") {
		t.Errorf("the events carry the tunnel's own error text: %s", r.rawEvents())
	}
}

func TestTheKeysAreWipedOnceTheTunnelHasThem(t *testing.T) {
	want := validConfig()
	var seen Config
	var held *Config
	tunnel, _ := echoTunnel()
	r := start(t, Options{newTunnel: func(cfg *Config, _ func(string, ...any)) (Tunnel, error) {
		seen, held = *cfg, cfg
		return tunnel, nil
	}})
	r.expectHello()
	r.send(configMessage(t, want))
	r.expect(Event{Event: EventConnected})
	_ = r.stdin.Close()
	r.exitCode()

	if seen != want {
		t.Errorf("the tunnel was built from %+v, want %+v", seen, want)
	}
	if !held.ClientKey.isZero() || !held.ServerKey.isZero() || !held.PresharedKey.isZero() {
		t.Error("the configuration still holds keys after the tunnel was built")
	}
}

func TestLogLinesAreEventsOnlyWhenAskedFor(t *testing.T) {
	for _, debug := range []bool{false, true} {
		name := "debug off"
		if debug {
			name = "debug on"
		}
		t.Run(name, func(t *testing.T) {
			tunnel, _ := echoTunnel()
			r := start(t, Options{newTunnel: func(_ *Config, logf func(string, ...any)) (Tunnel, error) {
				logf("first line\nsecond line with a \"quote\"\n")
				return tunnel, nil
			}})
			cfg := validConfig()
			cfg.Debug = debug
			r.expectHello()
			r.send(configMessage(t, cfg))
			r.expect(Event{Event: EventConnected})
			_ = r.stdin.Close()
			r.exitCode()

			raw := r.rawEvents()
			logged := strings.Contains(raw, `"event":"log"`)
			if logged != debug {
				t.Fatalf("log events present = %v, want %v:\n%s", logged, debug, raw)
			}
			for line := range strings.SplitSeq(strings.TrimSuffix(raw, "\n"), "\n") {
				var ev Event
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Errorf("line is not one JSON object: %q", line)
				}
			}
			if debug && !strings.Contains(raw, `"message":"first line\nsecond line with a \"quote\""`) {
				t.Errorf("the log line did not arrive whole, on one line:\n%s", raw)
			}
			if out := r.output(); out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
		})
	}
}

func TestKeyJSON(t *testing.T) {
	k := rawKey(7)
	text, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"BwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyAhIiMkJSY="`; string(text) != want {
		t.Errorf("key JSON = %s, want %s", text, want)
	}
	var back Key
	if err := json.Unmarshal(text, &back); err != nil || back != k {
		t.Errorf("round trip = %v, %v", back, err)
	}
	for _, in := range []string{`7`, `"not base64!"`, `"AAAA"`, `""`} {
		var k Key
		err := json.Unmarshal([]byte(in), &k)
		if err == nil {
			t.Errorf("Unmarshal(%s) succeeded", in)
			continue
		}
		if strings.Contains(err.Error(), strings.Trim(in, `"`)) && in != `""` {
			t.Errorf("Unmarshal(%s) repeats its input: %v", in, err)
		}
	}
}

func TestClamped(t *testing.T) {
	if !testKey(9).clamped() {
		t.Error("a clamped key reads as not clamped")
	}
	for name, spoil := range map[string]func(*Key){
		"bit 0":         func(k *Key) { k[0] |= 1 },
		"bit 1":         func(k *Key) { k[0] |= 2 },
		"bit 2":         func(k *Key) { k[0] |= 4 },
		"the top bit":   func(k *Key) { k[31] |= 0x80 },
		"no second bit": func(k *Key) { k[31] &^= 0x40 },
	} {
		k := testKey(9)
		spoil(&k)
		if k.clamped() {
			t.Errorf("%s: the key still reads as clamped", name)
		}
	}
}

// The parent may stop reading the session before it closes stdin. The write
// that fails then ends the session like any other parent leaving: a closing
// event, the tunnel closed in order.
func TestAParentThatStopsReadingEndsTheSession(t *testing.T) {
	tunnel, _ := echoTunnel()
	stdinR, stdinW := io.Pipe()
	eventsR, eventsW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(eventsR)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	exit := make(chan int, 1)
	go func() {
		code := Run(Options{
			Version:   "1.2.3",
			Stdin:     stdinR,
			Stdout:    writerFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe }),
			Events:    eventsW,
			newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) { return tunnel, nil },
			timing:    fastTiming(),
		})
		_ = eventsW.Close()
		exit <- code
	}()
	if _, err := stdinW.Write(append(configMessage(t, validConfig()), "to the device and back"...)); err != nil {
		t.Fatal(err)
	}
	var last string
	for line := range lines {
		last = line
	}
	if last != `{"event":"closed","reason":"parent_closed"}` {
		t.Errorf("last event = %s, want the session closed because its parent left", last)
	}
	if code := <-exit; code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !tunnel.drained.Load() || !tunnel.closed.Load() {
		t.Error("the tunnel was not closed in order")
	}
}

func TestTerminateWhileTheConfigurationIsAwaited(t *testing.T) {
	terminate := make(chan struct{})
	built := false
	r := start(t, Options{Terminate: terminate, newTunnel: func(*Config, func(string, ...any)) (Tunnel, error) {
		built = true
		return &fakeTunnel{}, nil
	}})
	r.expectHello()
	close(terminate)
	r.expect(Event{Event: EventClosed, Reason: ReasonTerminated})
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if built {
		t.Error("a tunnel was built without a configuration")
	}
}

// Once the device has not answered, the relay is probed to say which of the
// two failed. The parent leaving ends that wait too.
func TestTheParentLeavingEndsTheRelayProbe(t *testing.T) {
	probing := make(chan struct{})
	tunnel := &fakeTunnel{dial: func(context.Context) (net.Conn, error) { return nil, errors.New("silence") }}
	opts := withTunnel(tunnel)
	opts.probeRelay = func(ctx context.Context, _ Relay) bool {
		close(probing)
		<-ctx.Done()
		return false
	}
	opts.timing = fastTiming()
	opts.timing.probe = time.Minute
	cfg := validConfig()
	cfg.ConnectTimeoutSeconds = 1
	r := start(t, opts)
	r.expectHello()
	r.send(configMessage(t, cfg))
	select {
	case <-probing:
	case <-time.After(10 * time.Second):
		t.Fatal("the relay was not probed")
	}
	began := time.Now()
	_ = r.stdin.Close()
	r.expect(Event{Event: EventClosed, Reason: ReasonParentClosed})
	if code := r.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if waited := time.Since(began); waited > 5*time.Second {
		t.Errorf("the session took %v to end while the relay was being probed", waited)
	}
}

// The closing event is the last one: what is found out while the session
// winds down is not reported after it.
func TestNothingFollowsTheClosingEvent(t *testing.T) {
	var out bytes.Buffer
	e := &emitter{w: &out}
	e.emit(Event{Event: EventConnected})
	e.emit(Event{Event: EventClosed, Reason: ReasonPeerClosed})
	e.emit(Event{Event: EventPath, Path: PathDirect})
	e.logf("a late log line")
	e.emit(Event{Event: EventError, Reason: ReasonInternal})
	if got, want := out.String(), "{\"event\":\"connected\"}\n{\"event\":\"closed\",\"reason\":\"peer_closed\"}\n"; got != want {
		t.Errorf("events = %q, want %q", got, want)
	}
}
