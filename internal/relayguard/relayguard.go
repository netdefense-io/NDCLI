// Package relayguard keeps a package's tests off the production relay, the
// config default for pathfinder.host.
package relayguard

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// ParentEnv carries the guard's relay host to child test processes, and
// ParentPIDEnv the process that set it: a value inherited from anywhere else
// (a shell, a CI step) must not switch a top-level run into child mode.
const (
	ParentEnv    = "RELAYGUARD_HOST"
	ParentPIDEnv = "RELAYGUARD_PID"
)

// point sends pathfinder.host to host, through the environment (which
// survives config.Load) and the loaded config.
func point(host string) {
	_ = os.Setenv("NDCLI_PATHFINDER_HOST", host)
	config.Get().Pathfinder.Host = host
}

// Run is the body of TestMain for a package whose tests could reach the
// relay. It points pathfinder.host at a local listener through
// NDCLI_PATHFINDER_HOST, which survives config.Load, and through the loaded
// config. A test that connects to it makes the package fail: a test must use
// its own relay server. It returns the exit code for os.Exit.
//
// A test binary started as a child (cli's runNdcli, which strips the NDCLI_
// variables) runs TestMain too. It finds the parent's listener in
// ParentEnv, which has no NDCLI_ prefix so it survives that stripping, and
// dials it instead of opening its own, so the parent counts the child's
// dials even when the child exits inside m.Run.
func Run(m *testing.M) int {
	if host := os.Getenv(ParentEnv); host != "" && os.Getenv(ParentPIDEnv) == strconv.Itoa(os.Getppid()) {
		point(host)
		return m.Run()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "relayguard: cannot listen: %v\n", err)
		return 1
	}
	// A dial is counted before its connection is closed, and a relay client
	// waits for that close (no handshake answer), so every real dial is
	// counted before the test that made it can finish.
	var dials atomic.Int32
	accepting := make(chan struct{})
	go func() {
		defer close(accepting)
		for {
			conn, err := ln.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				// A passing failure (out of file descriptors): keep
				// accepting, or later dials would hang uncounted.
				time.Sleep(10 * time.Millisecond)
				continue
			}
			dials.Add(1)
			_ = conn.Close()
		}
	}()

	host := "ws://" + ln.Addr().String()
	_ = os.Setenv(ParentEnv, host)
	_ = os.Setenv(ParentPIDEnv, strconv.Itoa(os.Getpid()))
	point(host)

	code := m.Run()
	_ = ln.Close()
	<-accepting
	if n := dials.Load(); n > 0 {
		fmt.Fprintf(os.Stderr, "relayguard: tests connected %d time(s) to the relay host from the config; point them at their own relay server\n", n)
		if code == 0 {
			code = 1
		}
	}
	return code
}
