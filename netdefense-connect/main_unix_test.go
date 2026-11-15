//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A terminal's Ctrl-C reaches every process of the foreground group. The
// session must outlive it: its parent decides when it ends.
func TestAnInterruptIsIgnoredAndTerminateEndsTheSession(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	c.send(deadRelayConfig(t, false))
	time.Sleep(500 * time.Millisecond)

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
		if err := c.cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		if !c.running() {
			t.Fatalf("%v ended the process; stderr:\n%s", sig, strings.Join(c.lines(), "\n"))
		}
	}

	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if got, want := c.next(), (event{Event: "closed", Reason: "terminated"}); got != want {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if code := c.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	c.checkPipes()
}

// A write to a pipe the parent closed raises SIGPIPE, which ends a Go
// program when the pipe is its stdout or stderr. Here those two are the
// session and the events, so the companion has to outlive the signal and
// leave in order: a parent that stops reading must not turn into a session
// the device only notices at its next timeout.
func TestAClosedPipeDoesNotKillTheCompanion(t *testing.T) {
	cmd := exec.Command(companionPath)
	cmd.Env = []string{}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	eventsR, eventsW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = eventsW
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = eventsW.Close()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	hello := make([]byte, 256)
	if n, err := eventsR.Read(hello); err != nil || !strings.Contains(string(hello[:n]), `"event":"hello"`) {
		t.Fatalf("no hello: %q, %v", hello[:n], err)
	}
	if _, err := stdin.Write(deadRelayConfig(t, false)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	// Nobody reads the events any more; the closing event's write fails.
	_ = eventsR.Close()
	_ = stdin.Close()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the companion did not leave in order: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the companion did not exit")
	}
}

func TestTerminateBeforeAConfigurationEndsTheCompanion(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if got, want := c.next(), (event{Event: "closed", Reason: "terminated"}); got != want {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if code := c.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	c.checkPipes()
}
