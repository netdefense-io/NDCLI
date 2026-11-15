package pathfinder

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realCompanionEnv names a built netdefense-connect to run these tests
// against. They are skipped without it: building the companion means
// building its whole module, which CI does in a job of its own and then runs
// this file there. To run them by hand:
//
//	(cd netdefense-connect && go build -tags "$(cat build-tags.txt)" -o /tmp/netdefense-connect .)
//	NETDEFENSE_CONNECT_TEST_BIN=/tmp/netdefense-connect go test ./internal/pathfinder -run RealCompanion
//
// The companion must be built without a version, as this test binary is.
const realCompanionEnv = "NETDEFENSE_CONNECT_TEST_BIN"

// installRealCompanion copies the built companion into a directory this
// program appears to be installed in.
func installRealCompanion(t *testing.T) {
	t.Helper()
	built := os.Getenv(realCompanionEnv)
	if built == "" {
		t.Skip(realCompanionEnv + " is not set")
	}
	binary, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, companionFileName()), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	installedIn(t, dir)
}

// deadRelaySession is a session whose relay is a loopback port nothing
// listens on, so nothing leaves this machine.
func deadRelaySession(t *testing.T) *DirectSession {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	session := directSession()
	session.RelayHost, session.RelayPort, session.RelaySTUNPort = "127.0.0.1", port, port
	return session
}

// The real companion accepts this build's hello check and configuration
// message, and its account of a failure is read as written: the two modules
// agree on more than their constants.
func TestTheRealCompanionTakesThisBuildsConfiguration(t *testing.T) {
	installRealCompanion(t)
	session := deadRelaySession(t)
	session.ConnectTimeout = 2 * time.Second

	_, err := directClient(t, session).ConnectExecContext(t.Context())
	failed := directFailure(t, err)
	if failed.Failure != DirectRelayUnreachable {
		t.Errorf("failure = %q (%s), want %q", failed.Failure, failed.Message, DirectRelayUnreachable)
	}
	if !strings.HasPrefix(failed.Message, "the relay 127.0.0.1:") {
		t.Errorf("message = %q", failed.Message)
	}
	namesNoLibrary(t, failed.Message)
}

func TestTheRealCompanionLeavesWhenTheSessionIsGivenUp(t *testing.T) {
	installRealCompanion(t)
	path, err := locateCompanion()
	if err != nil {
		t.Fatal(err)
	}
	proc, err := startCompanion(path)
	if err != nil {
		t.Fatal(err)
	}
	session := deadRelaySession(t)
	cfg := companionConfig{
		Relay:                 companionRelay{Hostname: session.RelayHost, Port: session.RelayPort, STUNPort: session.RelaySTUNPort},
		Port:                  directDevicePort,
		ClientKey:             session.ClientKey,
		ServerKey:             session.DeviceKey,
		PresharedKey:          session.PresharedKey,
		ConnectTimeoutSeconds: 120,
	}
	if err := proc.configure(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := proc.awaitConnected(ctx, cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("awaitConnected = %v, want the deadline", err)
	}
	proc.discard()
	select {
	case <-proc.exited:
	case <-time.After(companionTiming.killAfter - time.Second):
		t.Fatal("the companion did not leave on its own when its stdin closed")
	}
}
