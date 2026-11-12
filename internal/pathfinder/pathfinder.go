package pathfinder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/output"
)

// ProgressCallback is called with status updates during connection
type ProgressCallback func(message string)

// ClientConfig holds configuration for the Pathfinder client
type ClientConfig struct {
	SessionID       string
	WebAdminEnabled bool // Enable webadmin tunnel (default: true)
	WebAdminPort    int  // 0 = auto-assign, >0 = specific port
	// WebAdminListener, when set, is the tunnel's listener, bound by the
	// caller before the connect began; it replaces WebAdminPort.
	WebAdminListener net.Listener
	WebAdminOnly     bool             // Skip the interactive shell; serve webadmin until quit
	OnProgress       ProgressCallback // Optional progress callback
	OnPaired         func()           // Optional; called once the device has paired
	IsTTY            bool             // Whether stdout is a terminal (WebAdmin box, messages)
	StdinIsTTY       bool             // Whether stdin is a terminal; the shell needs both
}

// Client represents a Pathfinder WebSocket client
type Client struct {
	host            string
	sslVerify       bool
	sessionID       string
	webAdminEnabled bool
	webAdminPort    int
	webAdminLn      net.Listener
	webAdminOnly    bool
	onProgress      ProgressCallback
	onPaired        func()
	relayRetryAfter time.Duration
	isTTY           bool
	stdinIsTTY      bool
	endReported     bool
}

// ShellOutcome reports how an interactive shell session ended.
type ShellOutcome struct {
	// Refused is true when the remote closed the shell stream with zero bytes
	// of output (the read-only enforcement path on the agent), as opposed to a
	// real terminal session that the user ended.
	Refused bool
}

// NewClient creates a new Pathfinder client with the given configuration.
// This is the single point both relay entry points funnel through — the
// interactive `device connect` CLI command (Client.Connect) and the MCP
// console_open tool (Client.ConnectExec) — so the TLS warning below fires
// once for both.
func NewClient(cfg ClientConfig) (*Client, error) {
	appCfg := config.Get()
	if appCfg.Pathfinder.Host == "" {
		return nil, ErrPathfinderNotConfigured
	}
	warnIfPathfinderSSLVerifyDisabled(os.Stderr)
	warnIfPathfinderPlaintext(os.Stderr, appCfg.Pathfinder.Host)
	return &Client{
		host:            appCfg.Pathfinder.Host,
		sslVerify:       appCfg.Pathfinder.SSLVerify,
		sessionID:       cfg.SessionID,
		webAdminEnabled: cfg.WebAdminEnabled,
		webAdminPort:    cfg.WebAdminPort,
		webAdminLn:      cfg.WebAdminListener,
		webAdminOnly:    cfg.WebAdminOnly,
		onProgress:      cfg.OnProgress,
		onPaired:        cfg.OnPaired,
		isTTY:           cfg.IsTTY,
		stdinIsTTY:      cfg.StdinIsTTY,
	}, nil
}

// warnIfPathfinderSSLVerifyDisabled prints a stderr warning when TLS
// certificate verification is disabled for the Pathfinder relay connection
// (pathfinder.ssl_verify=false, e.g. via NDCLI_PATHFINDER_SSL_VERIFY=false).
// With verification disabled, an on-path attacker can intercept the relay
// WebSocket, read or inject relay frames, and reach the device's shell —
// this mirrors warnIfTLSVerifyDisabled in cli/auth.go for the controlplane
// connection.
func warnIfPathfinderSSLVerifyDisabled(w io.Writer) {
	if config.Get().Pathfinder.SSLVerify {
		return
	}
	color.New(color.FgYellow).Fprintln(w, "Warning: TLS certificate verification is disabled for the connection relay "+
		"(pathfinder.ssl_verify=false). An on-path attacker can intercept the relay connection, reading or injecting "+
		"traffic and potentially reaching an interactive shell on the device.")
}

// warnIfPathfinderPlaintext prints a stderr line when a plaintext relay host
// (ws:// or http://) is dialed over TLS instead.
func warnIfPathfinderPlaintext(w io.Writer, host string) {
	wsURL, upgraded := relayWebSocketURL(host)
	if !upgraded {
		return
	}
	color.New(color.FgYellow).Fprintf(w, "Warning: the connection relay host (pathfinder.host) %s is plaintext; connecting over TLS to %s instead. Set pathfinder.host to a wss:// URL to stop this warning.\n", host, wsURL)
}

// ErrPathfinderNotConfigured is returned when the pathfinder host is not set
var ErrPathfinderNotConfigured = &PathfinderError{Message: "connection relay host not configured (pathfinder.host)"}

// PathfinderError represents a pathfinder-specific error
type PathfinderError struct {
	Message string
	Err     error
}

func (e *PathfinderError) Unwrap() error { return e.Err }

func (e *PathfinderError) Error() string {
	return e.Message
}

// maxRelayRetryWait is the longest Retry-After the client waits out before its
// single retry of a relay dial refused with 429.
const maxRelayRetryWait = 10 * time.Second

// relayRetryAllowed reports whether a relay 429 asking to wait retryAfter is
// waited out and dialed once more.
func relayRetryAllowed(retryAfter time.Duration) bool {
	return retryAfter > 0 && retryAfter <= maxRelayRetryWait
}

// maxRelayBusyWait is the longest Retry-After a relay 503 is waited out for;
// the relay sends 15 s when its cap on unregistered connections is reached.
const maxRelayBusyWait = 15 * time.Second

// maxRelayRetries is how many times one dial is retried in all: a 503 that
// is waited out counts, and so does the one retry a 429 gets.
const maxRelayRetries = 2

// relayBusyRetryAllowed reports whether a relay 503 asking to wait
// retryAfter is waited out and dialed again. A 503 without Retry-After is
// not: nothing says the relay will take the connection later.
func relayBusyRetryAllowed(retryAfter time.Duration) bool {
	return retryAfter > 0 && retryAfter <= maxRelayBusyWait
}

// relayRefusal is a relay refusal that is waited out.
type relayRefusal int

const (
	relayRateLimited relayRefusal = iota + 1
	relayBusy
)

// line is the progress line while left remains of the wait.
func (r relayRefusal) line(left time.Duration) string {
	if r == relayRateLimited {
		return fmt.Sprintf("Relay is rate limiting; retrying in %ds...", RetryAfterSeconds(left))
	}
	return fmt.Sprintf("Relay is at capacity; retrying in %ds...", RetryAfterSeconds(left))
}

// relayBusyJitter is added to a 503's wait, so the clients the relay's cap
// refused at once do not all come back at once. A variable so a test can fix
// it.
var relayBusyJitter = func() time.Duration { return rand.N(time.Second) }

// relayRetryWait is how long to wait before dialing again after err, and which
// refusal it was; kind is 0 when err is not retried. retries is how many times
// this dial has been retried, rateLimited whether a 429 was.
func relayRetryWait(err error, retries int, rateLimited bool) (wait time.Duration, kind relayRefusal) {
	if retries >= maxRelayRetries {
		return 0, 0
	}
	var rl *RateLimitedError
	if errors.As(err, &rl) && !rateLimited && relayRetryAllowed(rl.RetryAfter) {
		return rl.RetryAfter, relayRateLimited
	}
	var busy *RelayBusyError
	if errors.As(err, &busy) && relayBusyRetryAllowed(busy.RetryAfter) {
		return busy.RetryAfter + relayBusyJitter(), relayBusy
	}
	return 0, 0
}

// waitOutRelay waits out a relay refusal, repeating its progress line every
// pairingHeartbeat so a caller that resets its timeout on progress keeps the
// connect alive through a wait longer than that.
func (c *Client) waitOutRelay(ctx context.Context, wait time.Duration, kind relayRefusal) error {
	end := time.Now().Add(wait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	beat := time.NewTicker(pairingHeartbeat)
	defer beat.Stop()
	c.progress(kind.line(wait))
	for {
		select {
		case <-timer.C:
			return nil
		case <-beat.C:
			// A beat that fires with the timer would announce "retrying in 0s";
			// any time left rounds up to at least 1s.
			if left := time.Until(end); left > 0 {
				c.progress(kind.line(left))
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// RetryAfterSeconds is d in whole seconds, rounded up: a sub-second wait reads
// as 1, and 0 means no wait at all.
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// RelayRetryAfter is how long the last relay dial waited before it got in,
// summed over its retries, or 0 when it needed none: the relay's Retry-After,
// plus the jitter a 503's wait gets.
func (c *Client) RelayRetryAfter() time.Duration {
	return c.relayRetryAfter
}

// dialRelay connects a new RelayClient. A refusal the relay asks to retry is
// waited out and dialed again (relayRetryWait): a 429 with a Retry-After of at
// most maxRelayRetryWait once, a 503 with one of at most maxRelayBusyWait (plus
// up to a second of jitter) while the dial has been retried fewer than
// maxRelayRetries times. Any other
// refusal, a longer Retry-After, one retry too many, or a context deadline
// sooner than the wait is returned as is. The returned stop func releases the
// ctx watcher; it reports false when the watcher had already fired, so ctx has
// ended the connection.
func (c *Client) dialRelay(ctx context.Context, wsURL string) (*RelayClient, func() bool, error) {
	c.relayRetryAfter = 0
	rateLimited := false
	for retries := 0; ; retries++ {
		relay := NewRelayClient(wsURL, c.sessionID, c.sslVerify)
		stop := context.AfterFunc(ctx, relay.cancel)
		err := relay.Connect()
		if err == nil {
			return relay, stop, nil
		}
		stop()

		if wait, kind := relayRetryWait(err, retries, rateLimited); kind != 0 {
			if dl, ok := ctx.Deadline(); !ok || time.Until(dl) >= wait {
				rateLimited = rateLimited || kind == relayRateLimited
				if werr := c.waitOutRelay(ctx, wait, kind); werr != nil {
					return nil, nil, &PathfinderError{Message: "failed to connect to relay: " + werr.Error(), Err: werr}
				}
				c.relayRetryAfter += wait
				continue
			}
		}
		return nil, nil, &PathfinderError{Message: "failed to connect to relay: " + err.Error(), Err: err}
	}
}

// progress sends a progress update if a callback is configured
func (c *Client) progress(msg string) {
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

// Connect establishes a WebSocket connection to Pathfinder.
//
// The local webadmin tunnel — not the interactive shell — is the session's
// lifetime anchor. The connection stays up until the user quits (Ctrl-C) or
// the relay drops, independent of whether a terminal is opened or closed:
//
//   - With a terminal: the interactive shell runs in the foreground. When it
//     ends (the user types exit, or the agent refuses it on a read-only
//     session), the webadmin tunnel keeps serving until the user quits.
//   - Webadmin-only (--webadmin-only, no terminal on stdin or stdout, or a
//     refused shell): no terminal is started; the tunnel serves until the
//     user quits.
func (c *Client) Connect() error {
	return c.ConnectContext(context.Background())
}

// ConnectContext is Connect that gives up, and closes the relay connection,
// when ctx ends before the device pairs. Once it has paired the session no
// longer follows ctx: the user ends it, as Connect describes.
func (c *Client) ConnectContext(ctx context.Context) error {
	// Build WebSocket URL
	wsURL := c.buildWebSocketURL()

	c.progress("Connecting to relay server...")

	// Connect to the relay server
	relay, stopWatch, err := c.dialRelay(ctx, wsURL)
	if err != nil {
		return err
	}
	defer stopWatch()
	// Defers run last-in first-out: reportEnd (deferred below) must run
	// before this Close, or a user quit would be reported as a relay end.
	defer relay.Close()

	// Wait for registration confirmation
	if err := relay.WaitForRegistration(10 * time.Second); err != nil {
		return &PathfinderError{Message: "registration failed: " + err.Error()}
	}

	c.progress("Waiting for device...")

	// Wait for the device to connect and pair
	if err := relay.WaitForPairing(120 * time.Second); err != nil {
		return &PathfinderError{Message: "pairing failed: " + err.Error()}
	}
	// ctx may have ended just as the device paired; the relay is closing
	// then, so the connect is given up like any other before pairing.
	if !stopWatch() {
		return &PathfinderError{Message: "pairing failed: " + context.Cause(ctx).Error(), Err: ctx.Err()}
	}
	if c.onPaired != nil {
		c.onPaired()
	}
	defer c.reportEnd(relay) // keep above relay.Close in run order; see the Close defer

	// Create stream manager and wire it to the relay so the relay can close
	// all streams when the connection dies
	streamMgr := NewStreamManager(relay)
	relay.SetStreamManager(streamMgr)

	var tunnel *Tunnel
	var webAdminURL string
	if c.webAdminEnabled {
		var err error
		tunnel, err = c.startTunnel(streamMgr, os.Stderr)
		if err != nil {
			return err
		}
		if tunnel != nil {
			defer func() { _ = tunnel.Stop() }()
			webAdminURL = "http://localhost:" + itoa(tunnel.Port())
		}
	}

	// Show the WebAdmin link up front — while the terminal (if any) is still
	// active — so the user knows where to connect before interacting with the
	// shell. The keep-alive wait below repeats only a short reminder.
	if webAdminURL != "" {
		c.announceWebAdmin(os.Stderr, webAdminURL)
	}

	tryShell := c.shellWanted()

	if tryShell {
		c.progress("Starting shell...")
		stage("Connect: StartShellSession begin")
		outcome, shellErr := StartShellSession(streamMgr)
		stage("Connect: StartShellSession returned")
		if shellErr != nil {
			// Hard error opening the shell streams. Fall through to webadmin
			// keep-alive if a tunnel is up; otherwise surface the error.
			if tunnel == nil {
				return &PathfinderError{Message: "shell session error: " + shellErr.Error()}
			}
			fmt.Fprintf(os.Stderr, "Terminal unavailable (%s).\n", shellErr.Error())
		} else if outcome.Refused {
			// Read-only session: the agent refused the shell. Keep serving
			// webadmin (the whole point of an RO connect) until the user quits.
			if err := c.refusedShellError(tunnel != nil); err != nil {
				return err
			}
			if c.isTTY {
				fmt.Println("Read-only session — terminal disabled by the device.")
			}
		} else if c.isTTY {
			// A real terminal session ended. The remote shell's final output
			// (e.g. the OPNsense menu's "Enter an option:" prompt) leaves the
			// cursor at an inconsistent column with 0, 1, or 2 trailing
			// newlines. Normalize to a single fresh line so the keep-alive
			// message below always prints cleanly on its own line.
			fmt.Println()
		}
		// On a normal terminal exit we also fall through: the user closing the
		// shell must not tear the webadmin tunnel down.
	}

	// Nothing left to keep alive — no shell ran and no tunnel came up.
	if tunnel == nil {
		return nil
	}

	stage("Connect: entering waitForQuit")
	c.waitForQuit(relay, webAdminURL)
	return nil
}

// startTunnel opens the local webadmin tunnel. When it cannot listen (a busy
// --webadmin-port, say) and a shell will run, the session goes on without it
// and says so on stderr; with no shell there is nothing left to serve, so the
// failure is the command's.
func (c *Client) startTunnel(streamMgr *StreamManager, stderr io.Writer) (*Tunnel, error) {
	tunnel := NewTunnel(c.webAdminPort, "webadmin", streamMgr)
	if c.webAdminLn != nil {
		tunnel = NewTunnelOn(c.webAdminLn, "webadmin", streamMgr)
	}
	if err := tunnel.Start(); err != nil {
		if !c.shellWanted() {
			return nil, &PathfinderError{Message: "webadmin tunnel did not start: " + err.Error(), Err: err}
		}
		_, _ = fmt.Fprintf(stderr, "WebAdmin tunnel did not start: %v\n", err)
		return nil, nil
	}
	return tunnel, nil
}

// refusedShellError is the session's failure when the device refused the
// shell and no tunnel is up to serve instead; nil while one is.
func (c *Client) refusedShellError(tunnelUp bool) error {
	if tunnelUp {
		return nil
	}
	if c.webAdminEnabled {
		return &PathfinderError{Message: "the device refused the terminal (read-only session) and the webadmin tunnel did not start"}
	}
	return &PathfinderError{Message: "the device refused the terminal (read-only session), and --no-webadmin left nothing to serve"}
}

// shellWanted says whether to open the interactive shell. It needs a terminal
// on stdin (raw mode, keystrokes) and on stdout (the remote screen), and
// --webadmin-only opts out.
func (c *Client) shellWanted() bool {
	return c.isTTY && c.stdinIsTTY && !c.webAdminOnly
}

// announceWebAdmin shows where the tunnel listens: the box on a terminal, and
// otherwise a plain line on stderr, so a script or a nohup run can find a port
// that was assigned automatically.
func (c *Client) announceWebAdmin(stderr io.Writer, url string) {
	if c.isTTY {
		output.WebAdminBox(url)
		return
	}
	_, _ = fmt.Fprintf(stderr, "WebAdmin tunnel: %s\n", url)
}

// waitForQuit blocks until the user quits (Ctrl-C / SIGTERM) or the relay
// connection drops. While it blocks, the local webadmin tunnel keeps serving
// requests with zero persistent streams open — the agent keeps the session
// alive for the lifetime of the relay connection.
//
// The WebAdmin link box is printed by Connect before the shell runs, so here
// we only show a short reminder (with the URL repeated for convenience) once
// the foreground shell has exited or for terminal-less sessions.
func (c *Client) waitForQuit(relay *RelayClient, webAdminURL string) {
	if c.isTTY && webAdminURL != "" {
		fmt.Printf("WebAdmin tunnel still active at %s — press Ctrl-C to close.\n", webAdminURL)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Poll the relay connection so a dropped tunnel doesn't hang the process
	// waiting on a Ctrl-C that will never serve any traffic.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			if c.isTTY {
				fmt.Println("\nClosing connection.")
			}
			return
		case <-ticker.C:
			if !relay.IsConnected() {
				if c.isTTY {
					fmt.Printf("\nSession ended: %s. Closing.\n", endReasonOrDefault(relay))
					c.endReported = true
				}
				return
			}
		}
	}
}

func endReasonOrDefault(relay *RelayClient) string {
	if r := relay.EndReason(); r != "" {
		return r
	}
	return "connection to the relay was lost"
}

// reportEnd tells the user why a session the relay ended is over. It prints
// nothing when the user ended the session.
func (c *Client) reportEnd(relay *RelayClient) {
	if reason := relay.EndReason(); reason != "" && !c.endReported {
		c.endReported = true
		fmt.Fprintf(os.Stderr, "\nSession ended: %s\n", reason)
	}
}

// itoa converts an integer to a string without importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// buildWebSocketURL constructs the WebSocket URL from the configured host
func (c *Client) buildWebSocketURL() string {
	wsURL, _ := relayWebSocketURL(c.host)
	return wsURL
}

// relayWebSocketURL turns the configured relay host into the URL to dial. The
// relay stream carries the session key and the device's shell and WebAdmin
// traffic, so a plaintext scheme (ws://, http://) is dialed as wss:// unless
// the host is loopback; upgraded reports that rewrite. A host with no scheme
// is wss://.
func relayWebSocketURL(host string) (wsURL string, upgraded bool) {
	switch {
	case strings.HasPrefix(host, "wss://"):
	case strings.HasPrefix(host, "https://"):
		host = "wss://" + strings.TrimPrefix(host, "https://")
	case strings.HasPrefix(host, "ws://"), strings.HasPrefix(host, "http://"):
		rest := host[strings.Index(host, "://")+len("://"):]
		if isLoopbackHost(rest) {
			host = "ws://" + rest
		} else {
			host = "wss://" + rest
			upgraded = true
		}
	default:
		host = "wss://" + host
	}

	if !strings.HasSuffix(host, "/ws") {
		host = strings.TrimSuffix(host, "/") + "/ws"
	}
	return host, upgraded
}

// isLoopbackHost reports whether the authority at the start of rest
// (host[:port][/path]) names localhost or a loopback address.
func isLoopbackHost(rest string) bool {
	u, err := url.Parse("ws://" + rest)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
