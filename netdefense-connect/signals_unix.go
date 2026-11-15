//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// ignoreSignals turns off the signals that would end the process behind its
// parent's back. A terminal's interrupt and hangup reach the whole process
// group. SIGPIPE is what a write to a closed stdout or stderr raises, and
// those two are the session and the events here: a parent that stops reading
// must get a write error, which ends the session in order, not a kill.
func ignoreSignals() {
	signal.Ignore(os.Interrupt, syscall.SIGHUP, syscall.SIGPIPE)
}
