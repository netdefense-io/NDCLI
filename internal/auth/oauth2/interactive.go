package oauth2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/mdp/qrterminal/v3"
	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/internal/auth/oauth2/providers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// AuthResult represents the result of the authentication flow
type AuthResult int

const (
	AuthSuccess AuthResult = iota
	AuthTimeout
	AuthDenied
	AuthCancelled
	AuthError
)

// screenState represents the current display state
type screenState int

const (
	stateMenu screenState = iota
	stateQR
)

// InteractiveAuth handles the interactive device flow authentication
type InteractiveAuth struct {
	URL       string
	UserCode  string
	ExpiresIn time.Duration
	Interval  time.Duration
	PollFunc  func() (*models.TokenResponse, error)
}

// NewInteractiveAuth creates a new interactive auth handler
func NewInteractiveAuth(authResp *models.DeviceAuthResponse, pollFunc func() (*models.TokenResponse, error)) *InteractiveAuth {
	return &InteractiveAuth{
		URL:       authResp.VerificationURIComplete,
		UserCode:  authResp.UserCode,
		ExpiresIn: time.Duration(authResp.ExpiresIn) * time.Second,
		Interval:  time.Duration(authResp.Interval) * time.Second,
		PollFunc:  pollFunc,
	}
}

// Wait waits for the user to complete authentication
func (ia *InteractiveAuth) Wait(ctx context.Context) (*models.TokenResponse, AuthResult) {
	// Save terminal state at the start so we can restore it on exit
	fd := int(syscall.Stdin)
	var originalState *term.State
	if term.IsTerminal(fd) {
		var err error
		originalState, err = term.GetState(fd)
		if err != nil {
			originalState = nil
		}
	}

	// Create a cancellable context for the key reader goroutine
	keyCtx, keyCancel := context.WithCancel(ctx)

	// Cleanup function to restore terminal and stop goroutine
	cleanup := func() {
		keyCancel()
		// Give the goroutine time to exit
		time.Sleep(100 * time.Millisecond)
		// Restore original terminal state
		if originalState != nil {
			term.Restore(fd, originalState)
		}
		// Clear screen and move cursor to top, then print newline for clean output
		fmt.Print("\033[2J\033[H")
	}

	// Setup signal handler for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Setup deadline and intervals
	deadline := time.Now().Add(ia.ExpiresIn)
	pollInterval := ia.Interval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	// Create tickers
	pollTicker := time.NewTicker(pollInterval)
	timerTicker := time.NewTicker(1 * time.Second)
	defer pollTicker.Stop()
	defer timerTicker.Stop()

	// Start key reader goroutine
	keyChan := make(chan byte, 1)
	go ia.keyReaderLoop(keyCtx, keyChan)

	// State management
	state := stateMenu
	statusMsg := ""
	statusClearTime := time.Time{}

	// Initial render
	ia.render(state, deadline, statusMsg)

	for {
		select {
		case <-ctx.Done():
			cleanup()
			return nil, AuthCancelled

		case <-sigChan:
			cleanup()
			color.Yellow("✗ Cancelled")
			fmt.Println()
			return nil, AuthCancelled

		case key := <-keyChan:
			// Handle cancel keys
			if key == 'q' || key == 'Q' || key == 0x03 { // 0x03 = Ctrl+C
				cleanup()
				color.Yellow("✗ Cancelled")
				fmt.Println()
				return nil, AuthCancelled
			}

			// Handle menu options
			switch key {
			case '1':
				// Toggle QR
				if state == stateQR {
					state = stateMenu
				} else {
					state = stateQR
				}
				statusMsg = ""
				ia.render(state, deadline, statusMsg)

			case '2':
				if err := copyToClipboard(ia.URL); err != nil {
					statusMsg = "✗ Failed to copy"
				} else {
					statusMsg = "✓ URL copied to clipboard"
				}
				statusClearTime = time.Now().Add(2 * time.Second)
				ia.render(state, deadline, statusMsg)

			case '3':
				if err := ia.OpenBrowser(); err != nil {
					statusMsg = "✗ Failed to open browser"
				} else {
					statusMsg = "✓ Opened in browser"
				}
				statusClearTime = time.Now().Add(2 * time.Second)
				ia.render(state, deadline, statusMsg)
			}

		case <-timerTicker.C:
			if time.Now().After(deadline) {
				cleanup()
				color.Red("✗ Authentication timed out")
				fmt.Println()
				return nil, AuthTimeout
			}

			// Clear status message after timeout
			if !statusClearTime.IsZero() && time.Now().After(statusClearTime) {
				statusClearTime = time.Time{}
				statusMsg = ""
				ia.render(state, deadline, statusMsg)
			} else {
				// Just update timer in place
				ia.updateTimer(deadline)
			}

		case <-pollTicker.C:
			if time.Now().After(deadline) {
				cleanup()
				color.Red("✗ Authentication timed out")
				fmt.Println()
				return nil, AuthTimeout
			}

			token, err := ia.PollFunc()
			if err != nil {
				// Check for expected "pending" errors
				if errors.Is(err, providers.ErrAuthorizationPending) {
					continue
				}
				if errors.Is(err, providers.ErrSlowDown) {
					// Increase interval
					pollInterval = pollInterval + 5*time.Second
					pollTicker.Reset(pollInterval)
					continue
				}

				// Real error
				cleanup()
				color.Red("✗ Authentication failed: %s", err)
				fmt.Println()
				return nil, AuthError
			}

			// Success!
			cleanup()
			return token, AuthSuccess
		}
	}
}

// render clears the screen and redraws everything
func (ia *InteractiveAuth) render(state screenState, deadline time.Time, statusMsg string) {
	// Clear screen and move cursor to top-left
	fmt.Print("\033[2J\033[H")

	// Use \r\n for all line endings to work correctly even if raw mode interferes
	// Header
	color.Cyan("=== NetDefense Authentication ===")
	fmt.Print("\r\n\r\n")

	// URL and Code
	fmt.Print("URL:  ")
	color.New(color.FgWhite, color.Bold).Print(ia.URL)
	fmt.Print("\r\n")
	fmt.Print("Code: ")
	color.New(color.FgGreen, color.Bold).Print(ia.UserCode)
	fmt.Print("\r\n\r\n")

	// QR code if in QR state
	if state == stateQR {
		// Capture QR output and fix line endings
		var buf bytes.Buffer
		qrterminal.GenerateHalfBlock(ia.URL, qrterminal.L, &buf)
		qrOutput := strings.ReplaceAll(buf.String(), "\n", "\r\n")
		fmt.Print(qrOutput)
		fmt.Print("\r\n")
	}

	// Menu options
	color.New(color.Faint).Print("Options:")
	fmt.Print("\r\n")
	if state == stateQR {
		fmt.Print("  [1] Hide QR code\r\n")
	} else {
		fmt.Print("  [1] Show QR code\r\n")
	}
	fmt.Print("  [2] Copy URL to clipboard\r\n")
	fmt.Print("  [3] Open in browser\r\n")
	fmt.Print("  [q] Cancel\r\n")
	fmt.Print("\r\n")

	// Status message (if any)
	if statusMsg != "" {
		if strings.HasPrefix(statusMsg, "✓") {
			color.Green(statusMsg)
		} else {
			color.Red(statusMsg)
		}
		fmt.Print("\r\n")
	}

	// Timer (last line)
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	mins := int(remaining.Minutes())
	secs := int(remaining.Seconds()) % 60
	fmt.Printf("Time remaining: %02d:%02d", mins, secs)
}

// updateTimer displays the countdown timer on the current line
func (ia *InteractiveAuth) updateTimer(deadline time.Time) {
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	mins := int(remaining.Minutes())
	secs := int(remaining.Seconds()) % 60
	fmt.Printf("\rTime remaining: %02d:%02d  ", mins, secs)
}

// keyReaderLoop continuously polls for keypresses in a goroutine
func (ia *InteractiveAuth) keyReaderLoop(ctx context.Context, keyChan chan<- byte) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Briefly enter raw mode, read with short timeout, exit raw mode
			if key, ok := ia.readSingleKey(); ok {
				select {
				case keyChan <- key:
				case <-ctx.Done():
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// readSingleKey briefly enters raw mode to read a single keypress
func (ia *InteractiveAuth) readSingleKey() (byte, bool) {
	fd := int(syscall.Stdin)
	if !term.IsTerminal(fd) {
		return 0, false
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false
	}
	defer term.Restore(fd, oldState) // Always restore immediately

	buf := make([]byte, 1)
	os.Stdin.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, _ := os.Stdin.Read(buf)
	os.Stdin.SetReadDeadline(time.Time{})

	if n > 0 {
		return buf[0], true
	}
	return 0, false
}

// OpenBrowser opens the authentication URL in the default browser
func (ia *InteractiveAuth) OpenBrowser() error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{ia.URL}
	case "linux":
		cmd = "xdg-open"
		args = []string{ia.URL}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", ia.URL}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

// copyToClipboard copies text to the system clipboard
func copyToClipboard(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("clip")
	default:
		return fmt.Errorf("unsupported platform")
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// getOAuth2Domain returns the OAuth2 domain from config
func getOAuth2Domain() string {
	// This is a helper to get the domain for display
	// In production, this would come from config
	return "auth-dev.netdefense.io"
}
