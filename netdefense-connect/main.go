// netdefense-connect carries a remote session for ndcli, netdefense-mcp and
// the netdefense TUI. They start one per session; see internal/companion for
// what it is told and what it reports.
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/netdefense-io/NDCLI/netdefense-connect/internal/companion"
)

// version is the release, set by the build to the parent's own.
var version = "dev"

const usage = `netdefense-connect carries remote sessions for ndcli, netdefense-mcp and
netdefense over the direct-peer + encrypted relay transport. They start it
when a session needs it; it is not meant to be run by hand.

  netdefense-connect --version    print the version
`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-version":
			fmt.Printf("netdefense-connect %s\n", version)
			return 0
		case "--help", "-help", "-h":
			fmt.Print(usage)
			return 0
		}
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	// stdout carries the session and stderr the events, so nothing else may
	// write to either: whatever the libraries print goes nowhere.
	stdin, stdout, events := os.Stdin, os.Stdout, os.Stderr
	sink, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 1
	}
	os.Stdin, os.Stdout, os.Stderr = sink, sink, sink
	log.SetOutput(io.Discard)

	// The parent decides when the session ends, and closes stdin: ending
	// here first would drop the session without telling the device.
	ignoreSignals()
	// Registered before the hello is written: a parent may ask the companion
	// to stop as soon as it has read that line, and a SIGTERM nobody has
	// asked for yet would kill the process instead of ending it in order.
	terminate := make(chan struct{})
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	go func() {
		<-stop
		close(terminate)
	}()

	return companion.Run(companion.Options{
		Version:   version,
		Stdin:     stdin,
		Stdout:    stdout,
		Events:    events,
		Terminate: terminate,
	})
}
