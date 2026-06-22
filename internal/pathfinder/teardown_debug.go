package pathfinder

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

// Teardown diagnostics.
//
// The connect-session teardown spans several goroutines (the relay read/write
// pumps, each stream's dataWorker, the shell output copy, the input reader) and
// a chain of deferred cleanup. A deadlock in any of them wedges the process
// with no output — the operator-visible symptom is "WebAdmin tunnel still
// active" never printing and Ctrl-C doing nothing.
//
// Isolated unit tests can't reproduce that cross-goroutine interaction, so this
// instrumentation captures the truth from a live hang: staged stage logging
// plus a watchdog that dumps ALL goroutine stacks if teardown stalls. Enable
// with NDCLI_DEBUG=1 (or any non-empty value). Output goes to stderr; the dump
// also goes to a timestamped file under the OS temp dir so it survives a
// terminal that has to be killed.

var teardownDebugOnce sync.Once
var teardownDebugEnabled bool

func teardownDebugOn() bool {
	teardownDebugOnce.Do(func() {
		teardownDebugEnabled = os.Getenv("NDCLI_DEBUG") != ""
	})
	return teardownDebugEnabled
}

// stage logs a named teardown stage to stderr when NDCLI_DEBUG is set. Stages
// are also mirrored to the regular debug log so they show up in debug.log.
func stage(name string) {
	debugLog("teardown stage: %s", name)
	if teardownDebugOn() {
		fmt.Fprintf(os.Stderr, "[ndcli-teardown %s] %s\n",
			time.Now().Format("15:04:05.000"), name)
	}
}

// dumpGoroutines writes all goroutine stacks to stderr and to a temp file,
// returning the file path (empty if the file could not be written). reason is
// included in the header so multiple dumps are distinguishable.
func dumpGoroutines(reason string) string {
	header := fmt.Sprintf("=== ndcli goroutine dump (%s) at %s ===\n",
		reason, time.Now().Format(time.RFC3339Nano))

	fmt.Fprint(os.Stderr, header)
	_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
	fmt.Fprintln(os.Stderr, "=== end goroutine dump ===")

	path := fmt.Sprintf("%s/ndcli-hang-%d.txt", os.TempDir(), time.Now().Unix())
	f, err := os.Create(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fmt.Fprint(f, header)
	// Use runtime.Stack for the file so we get the full raw stacks regardless of
	// pprof formatting.
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			f.Write(buf[:n])
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	fmt.Fprintf(os.Stderr, "[ndcli-teardown] goroutine dump written to %s\n", path)
	return path
}

// watchdog fires dumpGoroutines if it is not stopped within d. It is meant to
// bracket a teardown that is expected to complete quickly; if teardown wedges,
// the dump names the exact blocked goroutines. Returns a stop func that cancels
// the watchdog (call it once teardown completes). The watchdog is a no-op
// unless NDCLI_DEBUG is set, so production builds carry zero overhead beyond a
// single timer.
func watchdog(d time.Duration, reason string) (stop func()) {
	if !teardownDebugOn() {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(d):
			dumpGoroutines(reason)
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
