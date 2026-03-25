package pathfinder

import (
	"strings"
	"time"

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
	onProgress      ProgressCallback
	isTTY           bool
}

// NewClient creates a new Pathfinder client with the given configuration
func NewClient(cfg ClientConfig) (*Client, error) {
	appCfg := config.Get()
	if appCfg.Pathfinder.Host == "" {
		return nil, ErrPathfinderNotConfigured
	}
	return &Client{
		host:            appCfg.Pathfinder.Host,
		sslVerify:       appCfg.Pathfinder.SSLVerify,
		sessionID:       cfg.SessionID,
		webAdminEnabled: cfg.WebAdminEnabled,
		webAdminPort:    cfg.WebAdminPort,
		onProgress:      cfg.OnProgress,
		isTTY:           cfg.IsTTY,
	}, nil
}

// ErrPathfinderNotConfigured is returned when the pathfinder host is not set
var ErrPathfinderNotConfigured = &PathfinderError{Message: "pathfinder host not configured"}

// PathfinderError represents a pathfinder-specific error
type PathfinderError struct {
	Message string
}

func (e *PathfinderError) Error() string {
	return e.Message
}

// progress sends a progress update if a callback is configured
func (c *Client) progress(msg string) {
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

// Connect establishes a WebSocket connection to Pathfinder and starts an interactive shell
func (c *Client) Connect() error {
	// Build WebSocket URL
	wsURL := c.buildWebSocketURL()

	c.progress("Connecting to relay server...")

	// Create relay client
	relay := NewRelayClient(wsURL, c.sessionID, c.sslVerify)

	// Connect to the relay server
	if err := relay.Connect(); err != nil {
		return &PathfinderError{Message: "failed to connect to relay: " + err.Error()}
	}
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

	c.progress("Starting shell...")

	// Create stream manager and wire it to the relay so the relay can close
	// all streams when the connection dies
	streamMgr := NewStreamManager(relay)
	relay.SetStreamManager(streamMgr)

	// Start webadmin tunnel if enabled
	var tunnel *Tunnel
	if c.webAdminEnabled {
		tunnel = NewTunnel(c.webAdminPort, "webadmin", streamMgr)
		if err := tunnel.Start(); err != nil {
			// Non-fatal: just skip the tunnel
		} else if c.isTTY {
			output.WebAdminBox("http://localhost:" + itoa(tunnel.Port()))
		}
	}

	// Start the interactive shell session (blocking)
	shellErr := StartShellSession(streamMgr)

	// Clean up tunnel after shell exits
	if tunnel != nil {
		tunnel.Stop()
	}

	if shellErr != nil {
		return &PathfinderError{Message: "shell session error: " + shellErr.Error()}
	}

	return nil
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
	host := c.host

	// If already a WebSocket URL, use as-is
	if strings.HasPrefix(host, "ws://") || strings.HasPrefix(host, "wss://") {
		if !strings.HasSuffix(host, "/ws") {
			host = strings.TrimSuffix(host, "/") + "/ws"
		}
		return host
	}

	// Convert HTTP(S) to WS(S)
	if strings.HasPrefix(host, "https://") {
		host = "wss://" + strings.TrimPrefix(host, "https://")
	} else if strings.HasPrefix(host, "http://") {
		host = "ws://" + strings.TrimPrefix(host, "http://")
	} else {
		// Assume secure WebSocket if no scheme
		host = "wss://" + host
	}

	// Ensure /ws path
	if !strings.HasSuffix(host, "/ws") {
		host = strings.TrimSuffix(host, "/") + "/ws"
	}

	return host
}
