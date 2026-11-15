package companion

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/envknob"
	"tailscale.com/net/netcheck"
	"tailscale.com/syncs"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

// TestMain keeps the tunnel under test off the real network, the way the
// tunnel library's own tests do. A test binary links features the released
// binary leaves out (build-tags.txt), and two of them reach outside during
// every network check: the port mapper queries the default gateway, and
// captive-portal detection calls hosts on the internet.
func TestMain(m *testing.M) {
	envknob.Setenv("IN_TS_TEST", "true")
	netcheck.HookStartCaptivePortalDetection.SetForTest(func(context.Context, *netcheck.Client, *tailcfg.DERPMap, tailcfg.DERPRegionID, func(bool)) (<-chan struct{}, func()) {
		return syncs.ClosedChan(), func() {}
	})
	os.Exit(m.Run())
}

// device is the far end of the tunnel in a test: the library's server on a
// relay running in this process, serving one port.
type device struct {
	relay *tailcfg.DERPNode
	// closed yields once for each connection the device has finished with.
	closed chan struct{}
}

// startDevice starts a relay on loopback and a device behind it. The device
// expects the keys of expect, admits only that client, and serves each
// connection to its port with serve.
func startDevice(t *testing.T, expect Config, serve func(net.Conn)) *device {
	t.Helper()
	quiet := func(string, ...any) {}
	relayMap := integration.RunDERPAndSTUN(t, quiet, "127.0.0.1")
	region := relayMap.Regions[1]

	serverKey, err := nodeKey(expect.ServerKey)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := nodeKey(expect.ClientKey)
	if err != nil {
		t.Fatal(err)
	}
	d := &device{relay: region.Nodes[0], closed: make(chan struct{}, 16)}
	port := uint16(expect.Port)
	server := &tailcat.Server{
		Key:            serverKey,
		PresharedKey:   tailcat.PresharedKey(expect.PresharedKey),
		Logf:           quiet,
		Region:         region,
		AllowedClients: []key.NodePublic{clientKey.Public()},
		ServedTCPPorts: []filter.PortRange{{First: port, Last: port}},
		OnTCP: func(p uint16) func(net.Conn) {
			if p != port {
				return nil
			}
			return func(c net.Conn) {
				serve(c)
				d.closed <- struct{}{}
			}
		},
	}
	if err := server.Start(); err != nil {
		t.Fatalf("device: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return d
}

// through runs sessions over d's relay: the node of the test relay stands in
// for the one the configuration names, since only it can mark the relay's
// test certificate as trusted.
func (d *device) through() Options {
	return Options{
		newTunnel: func(cfg *Config, logf func(string, ...any)) (Tunnel, error) {
			return newTunnelOn(cfg, logf, d.relay)
		},
		timing: defaultTiming(),
	}
}

// configFor is a configuration that names d's relay.
func (d *device) configFor(cfg Config) Config {
	cfg.Relay = Relay{Hostname: d.relay.HostName, Port: d.relay.DERPPort, STUNPort: d.relay.STUNPort}
	return cfg
}

func echo(c net.Conn) {
	_, _ = io.Copy(c, c)
	_ = c.Close()
}

func TestTheTunnelCarriesASession(t *testing.T) {
	cfg := validConfig()
	d := startDevice(t, cfg, echo)

	opts := d.through()
	opts.timing.pathPollFast = 100 * time.Millisecond
	r := start(t, opts)
	r.expectHello()
	r.send(configMessage(t, d.configFor(cfg)))
	r.expect(Event{Event: EventConnected})

	r.send([]byte("over the tunnel"))
	r.waitOutput("over the tunnel")

	path := r.next()
	if path.Event != EventPath || (path.Path != PathDirect && path.Path != PathRelayed) {
		t.Fatalf("event = %+v, want a path event", path)
	}

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
	// The device must see the session end as soon as this end leaves.
	select {
	case <-d.closed:
	case <-time.After(10 * time.Second):
		t.Error("the device did not see the connection close")
	}
	if out := r.output(); out != "over the tunnel" {
		t.Errorf("stdout = %q, want only the device's bytes", out)
	}
}

func TestTheTunnelSeesTheDeviceClose(t *testing.T) {
	cfg := validConfig()
	d := startDevice(t, cfg, func(c net.Conn) {
		_, _ = io.WriteString(c, "bye")
		_ = c.Close()
	})
	r := start(t, d.through())
	r.expectHello()
	r.send(configMessage(t, d.configFor(cfg)))
	r.expect(Event{Event: EventConnected})
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
	if out := r.output(); out != "bye" {
		t.Errorf("stdout = %q, want %q", out, "bye")
	}
}

// A device holding other keys than the session's never answers: the relay is
// there, the tunnel does not come up, and the parent is told only that.
func TestTheTunnelNeedsTheSessionsKeys(t *testing.T) {
	for _, tc := range []struct {
		name   string
		differ func(*Config)
	}{
		{"another pre-shared key", func(c *Config) { c.PresharedKey = testKey(77) }},
		{"another client key", func(c *Config) { c.ClientKey = testKey(78) }},
		{"another device key", func(c *Config) { c.ServerKey = testKey(79) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deviceCfg := validConfig()
			d := startDevice(t, deviceCfg, echo)

			ours := d.configFor(validConfig())
			tc.differ(&ours)
			ours.ConnectTimeoutSeconds = 3
			r := start(t, d.through())
			r.expectHello()
			r.send(configMessage(t, ours))
			r.expect(Event{Event: EventError, Reason: ReasonNoAnswer})
			if code := r.exitCode(); code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if out := r.output(); out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
			if raw := strings.ToLower(r.rawEvents()); strings.Contains(raw, "tailcat") || strings.Contains(raw, "derp") {
				t.Errorf("the events name the tunnel library: %s", raw)
			}
		})
	}
}

func TestARelayThatIsNotThere(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := validConfig()
	cfg.Relay = Relay{Hostname: "127.0.0.1", Port: port, STUNPort: port}
	cfg.ConnectTimeoutSeconds = 2
	opts := Options{
		newTunnel: func(cfg *Config, logf func(string, ...any)) (Tunnel, error) {
			node := relayNode(cfg.Relay)
			node.IPv4, node.IPv6 = "127.0.0.1", "none"
			return newTunnelOn(cfg, logf, node)
		},
		probeRelay: relayAcceptsConnections,
		timing:     defaultTiming(),
	}
	r := start(t, opts)
	r.expectHello()
	r.send(configMessage(t, cfg))
	r.expect(Event{Event: EventError, Reason: ReasonRelayUnreachable})
	if code := r.exitCode(); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

// recordingTransport notes every request made through the default HTTP
// client, which is how the tunnel library fetches a relay map.
type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.urls = append(rt.urls, req.URL.String())
	rt.mu.Unlock()
	return nil, http.ErrUseLastResponse
}

// A session names its relay itself. It must never ask the library's default
// relay map which relay to use: that would contact a third party and could
// put the session on a relay that is not ours.
func TestTheTunnelLooksUpNoRelayMap(t *testing.T) {
	recorder := &recordingTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = previous })
	// Checked on every way out: a session sent to a relay map does not come
	// up, and the request is why.
	defer func() {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		if len(recorder.urls) != 0 {
			t.Errorf("the session made HTTP requests through the default client: %v", recorder.urls)
		}
	}()

	cfg := validConfig()
	d := startDevice(t, cfg, echo)
	r := start(t, d.through())
	r.expectHello()
	r.send(configMessage(t, d.configFor(cfg)))
	r.expect(Event{Event: EventConnected})
	r.send([]byte("ping"))
	r.waitOutput("ping")
	_ = r.stdin.Close()
	r.exitCode()
}

func TestTheDeviceAddressCarriesItsKeysAndTheRelay(t *testing.T) {
	cfg := validConfig()
	addr, err := deviceAddress(&cfg, relayNode(cfg.Relay))
	if err != nil {
		t.Fatal(err)
	}
	info, err := tailcat.ParseAddr(addr)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := nodeKey(cfg.ServerKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := serverKey.Raw32(); got != [32]byte(cfg.ServerKey) {
		t.Errorf("the device key was altered on its way to the tunnel: %x, want %x", got, cfg.ServerKey)
	}
	if info.ServerPublic.NodePublic != serverKey.Public() {
		t.Error("the address does not carry the device's public key")
	}
	if !info.ServerDiscoPublic.Equal(tailcat.DiscoPublicForNode(serverKey)) {
		t.Error("the address does not carry the device's path-discovery key")
	}
	if info.PresharedKey != tailcat.PresharedKey(cfg.PresharedKey) {
		t.Error("the address does not carry the pre-shared key")
	}
	if info.RegionID != 0 {
		t.Errorf("the address names relay region %d of a relay map; it must carry the relay itself", info.RegionID)
	}
	if len(info.Region) != 1 || len(info.Region[0].Nodes) != 1 {
		t.Fatalf("the address carries %d relay regions, want one with one node", len(info.Region))
	}
	node := info.Region[0].Nodes[0]
	if node.HostName != cfg.Relay.Hostname || node.DERPPort != cfg.Relay.Port || node.STUNPort != cfg.Relay.STUNPort {
		t.Errorf("relay node = %s:%d (STUN %d), want %s:%d (STUN %d)", node.HostName, node.DERPPort, node.STUNPort, cfg.Relay.Hostname, cfg.Relay.Port, cfg.Relay.STUNPort)
	}
	if node.InsecureForTests {
		t.Error("the relay's certificate would go unchecked")
	}
}

// The path check is also the only sign of life on an idle session, so it has
// to keep answering for as long as the tunnel is up.
func TestThePathCheckKeepsAnswering(t *testing.T) {
	cfg := validConfig()
	d := startDevice(t, cfg, echo)
	theirs := d.configFor(cfg)
	tunnel, err := newTunnelOn(&theirs, func(string, ...any) {}, d.relay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tunnel.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var conn net.Conn
	for conn == nil {
		if conn, err = tunnel.Dial(ctx); err != nil && ctx.Err() != nil {
			t.Fatalf("dial: %v", err)
		}
	}
	defer func() { _ = conn.Close() }()

	for i := range 5 {
		checkCtx, done := context.WithTimeout(ctx, 5*time.Second)
		if _, err := tunnel.Direct(checkCtx); err != nil {
			t.Errorf("path check %d: %v", i+1, err)
		}
		done()
	}
}

// Leaving a session cleanly needs a connection that can close one direction
// and still be read: this end closes its side, and the device's answer to
// that, its own close, arrives.
func TestTheTunnelsConnectionCanCloseOneSide(t *testing.T) {
	cfg := validConfig()
	d := startDevice(t, cfg, func(c net.Conn) {
		_, _ = io.Copy(io.Discard, c)
		_, _ = io.WriteString(c, "after your close")
		_ = c.Close()
	})
	theirs := d.configFor(cfg)
	tunnel, err := newTunnelOn(&theirs, func(string, ...any) {}, d.relay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tunnel.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var conn net.Conn
	for conn == nil {
		if conn, err = tunnel.Dial(ctx); err != nil && ctx.Err() != nil {
			t.Fatalf("dial: %v", err)
		}
	}
	defer func() { _ = conn.Close() }()

	half, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatalf("the tunnel's connection (%T) cannot close one side", conn)
	}
	if err := half.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil || string(got) != "after your close" {
		t.Errorf("after closing this side: read %q, %v", got, err)
	}
}
