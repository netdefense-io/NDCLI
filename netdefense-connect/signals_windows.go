package main

import (
	"os"
	"os/signal"
)

// ignoreSignals turns off the console's interrupt, which reaches every
// process attached to it: the parent decides when the session ends.
func ignoreSignals() {
	signal.Ignore(os.Interrupt)
}
