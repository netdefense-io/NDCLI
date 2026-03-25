package output

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// ConnectSpinner wraps the spinner library for connection status updates
type ConnectSpinner struct {
	spinner   *spinner.Spinner
	enabled   bool
	lastMsg   string
}

// NewConnectSpinner creates a new spinner for device connection
func NewConnectSpinner(deviceName string) *ConnectSpinner {
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	cs := &ConnectSpinner{
		enabled: isTTY,
	}

	if isTTY {
		s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
		s.Suffix = " Connecting to " + deviceName + "..."
		s.Start()
		cs.spinner = s
		cs.lastMsg = "Connecting to " + deviceName + "..."
	}

	return cs
}

// UpdateMessage updates the spinner message
func (cs *ConnectSpinner) UpdateMessage(msg string) {
	cs.lastMsg = msg
	if cs.enabled && cs.spinner != nil {
		cs.spinner.Suffix = " " + msg
	}
}

// Stop stops the spinner
func (cs *ConnectSpinner) Stop() {
	if cs.enabled && cs.spinner != nil {
		cs.spinner.Stop()
	}
}

// IsTTY returns whether we're running in a TTY
func (cs *ConnectSpinner) IsTTY() bool {
	return cs.enabled
}
