package pathfinder

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	WebAdminEnabled bool             // Enable webadmin tunnel (default: true)
	WebAdminPort    int              // 0 = auto-assign, >0 = specific port
	WebAdminOnly    bool             // Skip the interactive shell; serve webadmin until quit
	OnProgress      ProgressCallback // Optional progress callback
	IsTTY           bool             // Whether output is a TTY (for WebAdmin box)
}

// Client represents a Pathfinder WebSocket client
type Client struct {
	host            string
	sslVerify       bool
	sessionID       string
	webAdminEnabled bool
	webAdminPort    int
	webAdminOnly    bool
	onProgress      ProgressCallback
	relayRetryAfter time.Duration
	isTTY           bool
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
		webAdminOnly:    cfg.WebAdminOnly,
		onProgress:      cfg.OnProgress,
		isTTY:           cfg.IsTTY,
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

// RetryAfterSeconds is d in whole seconds, rounded up: a sub-second wait reads
// as 1, and 0 means no wait at all.
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// RelayRetryAfter is the Retry-After the last relay dial waited out before its
// retry, or 0 when it needed none.
func (c *Client) RelayRetryAfter() time.Duration {
	return c.relayRetryAfter
}

// dialRelay connects a new RelayClient. A 429 whose Retry-After is at most
// maxRelayRetryWait is waited out and dialed once more; a second refusal, a
// longer Retry-After, or a context deadline sooner than the wait is returned
// as is. The returned stop func releases the ctx watcher.
func (c *Client) dialRelay(ctx context.Context, wsURL string) (*RelayClient, func(), error) {
	c.relayRetryAfter = 0
	for attempt := 0; ; attempt++ {
		relay := NewRelayClient(wsURL, c.sessionID, c.sslVerify)
		stop := context.AfterFunc(ctx, relay.cancel)
		err := relay.Connect()
		if err == nil {
			return relay, func() { stop() }, nil
		}
		stop()

		var rl *RateLimitedError
		if attempt == 0 && errors.As(err, &rl) && relayRetryAllowed(rl.RetryAfter) {
			if dl, ok := ctx.Deadline(); !ok || time.Until(dl) >= rl.RetryAfter {
				c.progress(fmt.Sprintf("Relay is rate limiting; retrying in %ds...", RetryAfterSeconds(rl.RetryAfter)))
				timer := time.NewTimer(rl.RetryAfter)
				select {
				case <-timer.C:
					c.relayRetryAfter = rl.RetryAfter
					continue
				case <-ctx.Done():
					timer.Stop()
					return nil, nil, &PathfinderError{Message: "failed to connect to relay: " + ctx.Err().Error(), Err: ctx.Err()}
				}
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
//   - Webadmin-only (--webadmin-only, a non-TTY, or a refused shell): no
//     terminal is started; the tunnel serves until the user quits.
func (c *Client) Connect() error {
	// Build WebSocket URL
	wsURL := c.buildWebSocketURL()

	c.progress("Connecting to relay server...")

	// Connect to the relay server
	relay, stopWatch, err := c.dialRelay(context.Background(), wsURL)
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
	defer c.reportEnd(relay) // keep above relay.Close in run order; see the Close defer

	// Create stream manager and wire it to the relay so the relay can close
	// all streams when the connection dies
	streamMgr := NewStreamManager(relay)
	relay.SetStreamManager(streamMgr)

	// Start webadmin tunnel if enabled
	var tunnel *Tunnel
	var webAdminURL string
	if c.webAdminEnabled {
		tunnel = NewTunnel(c.webAdminPort, "webadmin", streamMgr)
		if err := tunnel.Start(); err != nil {
			// Non-fatal: just skip the tunnel
		} else {
			webAdminURL = "http://localhost:" + itoa(tunnel.Port())
		}
	}
	if tunnel != nil {
		defer tunnel.Stop()
	}

	// Show the WebAdmin link up front — while the terminal (if any) is still
	// active — so the user knows where to connect before interacting with the
	// shell. The keep-alive wait below repeats only a short reminder.
	if c.isTTY && webAdminURL != "" {
		output.WebAdminBox(webAdminURL)
	}

	// Whether we can/should run an interactive shell. A non-TTY (piped) stdin
	// has no terminal, and --webadmin-only opts out explicitly.
	tryShell := c.isTTY && !c.webAdminOnly

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
