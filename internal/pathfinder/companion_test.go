package pathfinder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// fakeCompanion is testdata/fakecompanion, built once for the package.
var fakeCompanion string

func TestMain(m *testing.M) {
	os.Exit(buildFakeCompanionAndRun(m))
}

func buildFakeCompanionAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "pathfinder-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	fakeCompanion = filepath.Join(dir, "fakecompanion")
	if out, err := exec.Command("go", "build", "-o", fakeCompanion, "./testdata/fakecompanion").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the fake companion: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// scenario is what the fake companion does; see testdata/fakecompanion.
type scenario map[string]any

func companionFileName() string {
	if runtime.GOOS == "windows" {
		return companionName + ".exe"
	}
	return companionName
}

// installCompanion puts the fake companion, told to play sc, in a directory
// of its own and makes this program appear to be installed there. It returns
// the directory, where the fake records what it saw.
func installCompanion(t *testing.T, sc scenario) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := sc["hello"]; !ok {
		sc["hello"] = map[string]any{"event": "hello", "version": config.Version, "protocol": companionProtocol}
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(fakeCompanion)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, companionFileName()), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	installedIn(t, dir)
	// The fake writes into dir until it has left, and the directory cannot
	// be removed before.
	t.Cleanup(func() { waitCompanionGone(dir) })
	return dir
}

// waitCompanionGone waits, for a bounded time, until the fake companion
// started from dir has left.
func waitCompanionGone(dir string) {
	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if proc.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
}

// installedIn makes this program appear to be installed in dir.
func installedIn(t *testing.T, dir string) {
	t.Helper()
	previous := executablePath
	executablePath = func() (string, error) { return filepath.Join(dir, "ndcli"), nil }
	t.Cleanup(func() { executablePath = previous })
}

// companionWaitsFor changes the waits sessions start with, until the test
// ends.
func companionWaitsFor(t *testing.T, change func(*companionWaits)) {
	t.Helper()
	previous := companionTiming
	change(&companionTiming)
	t.Cleanup(func() { companionTiming = previous })
}

// linkTimers sets how often a link sends a keepalive, how long it waits for
// a frame, and how often it looks at both.
func linkTimers(t *testing.T, keepAlive, receive, check time.Duration) {
	t.Helper()
	companionWaitsFor(t, func(w *companionWaits) { w.keepAlive, w.receive, w.check = keepAlive, receive, check })
}

// quickCompanionWaits shortens the waits of starting and stopping a
// companion.
func quickCompanionWaits(t *testing.T) {
	t.Helper()
	companionWaitsFor(t, func(w *companionWaits) {
		w.hello, w.connectMargin, w.exit, w.killAfter, w.lastBytes =
			500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond, time.Second
	})
}

// sessionKey is a key in the clamped form a session's tunnel keys have.
func sessionKey(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b + byte(i)
	}
	k[0] &= 248
	k[31] = k[31]&127 | 64
	return k
}

func directSession() *DirectSession {
	return &DirectSession{
		RelayHost:      "relay.example.test",
		RelayPort:      443,
		RelaySTUNPort:  3478,
		ClientKey:      sessionKey(1),
		DeviceKey:      sessionKey(101),
		PresharedKey:   sessionKey(201),
		ConnectTimeout: 30 * time.Second,
	}
}

func directClient(t *testing.T, session *DirectSession) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{Direct: session})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// recorded waits for a file the fake companion writes and returns it.
func recorded(t *testing.T, dir, name string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return string(raw)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the companion never recorded %q", name)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func notRecorded(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
		t.Errorf("the companion recorded %q", name)
	}
}

func directFailure(t *testing.T, err error) *DirectError {
	t.Helper()
	var failed *DirectError
	if !errors.As(err, &failed) {
		t.Fatalf("err = %v (%T), want a *DirectError", err, err)
	}
	return failed
}

// namesNoLibrary fails when text a user reads names what the transport is
// built on.
func namesNoLibrary(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, word := range []string{"tailcat", "tailscale", "derp", "wireguard"} {
		if strings.Contains(lower, word) {
			t.Errorf("the message names %q: %s", word, text)
		}
	}
}

func TestADirectSessionRunsCommandsThroughTheCompanion(t *testing.T) {
	// What a companion must never inherit, and the one thing it may.
	t.Setenv("TS_DEBUG_ADDR", "1")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.test:3128")
	t.Setenv("NDCLI_TOKEN", "ndpat_secret")
	t.Setenv("SSL_CERT_FILE", "/etc/ssl/custom.pem")

	dir := installCompanion(t, scenario{})
	var progress []string
	c, err := NewClient(ClientConfig{Direct: directSession(), OnProgress: func(m string) { progress = append(progress, m) }})
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.ConnectExecContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code, truncated, err := h.Exec(t.Context(), "uname -a", time.Minute)
	if err != nil || stdout != "ran: uname -a" || stderr != "" || code != 0 || truncated {
		t.Fatalf("Exec = %q, %q, %d, %v, %v", stdout, stderr, code, truncated, err)
	}
	stdout, _, _, _, err = h.Exec(t.Context(), "second", time.Minute)
	if err != nil || stdout != "ran: second" {
		t.Fatalf("second Exec = %q, %v", stdout, err)
	}
	h.Close()

	if got := recorded(t, dir, "stdin-closed"); got != "during the session" {
		t.Errorf("the companion saw stdin close %q, want during the session", got)
	}
	recorded(t, dir, "exited")
	if reason := h.EndReason(); reason != "" {
		t.Errorf("EndReason after this end closed = %q, want none", reason)
	}

	if args := recorded(t, dir, "args"); args != "" {
		t.Errorf("the companion was given arguments: %q", args)
	}
	if env := recorded(t, dir, "env"); env != "SSL_CERT_FILE=/etc/ssl/custom.pem" {
		t.Errorf("the companion's environment = %q, want only SSL_CERT_FILE", env)
	}
	b64 := func(k [32]byte) string { return base64.StdEncoding.EncodeToString(k[:]) }
	want := `{"relay":{"hostname":"relay.example.test","port":443,"stun_port":3478},"port":1,` +
		`"client_key":"` + b64(sessionKey(1)) + `","server_key":"` + b64(sessionKey(101)) + `","preshared_key":"` + b64(sessionKey(201)) + `",` +
		`"connect_timeout_seconds":30}`
	if got := recorded(t, dir, "config.json"); got != want {
		t.Errorf("the configuration sent:\n got %s\nwant %s", got, want)
	}
	wantProgress := []string{"Starting a direct-peer + encrypted relay session...", "Waiting for device...", "Opening exec stream..."}
	if fmt.Sprint(progress) != fmt.Sprint(wantProgress) {
		t.Errorf("progress = %q, want %q", progress, wantProgress)
	}
}

func TestAMissingCompanion(t *testing.T) {
	dir := t.TempDir()
	installedIn(t, dir)
	asDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(asDirectory, companionFileName()), 0o700); err != nil {
		t.Fatal(err)
	}

	for name, where := range map[string]string{"nothing there": dir, "a directory of that name": asDirectory} {
		t.Run(name, func(t *testing.T) {
			installedIn(t, where)
			_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
			failed := directFailure(t, err)
			if failed.Failure != DirectCompanionMissing {
				t.Errorf("failure = %q, want %q", failed.Failure, DirectCompanionMissing)
			}
			want := "netdefense-connect is missing from " + where + ". A session on direct-peer + encrypted relay needs it: reinstall ndcli, which installs both"
			if failed.Message != want {
				t.Errorf("message = %q\n   want %q", failed.Message, want)
			}
		})
	}
}

// A package manager starts the program through a symbolic link; the
// companion sits beside the file the link points at.
func TestTheCompanionIsFoundBesideTheResolvedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	installed := installCompanion(t, scenario{})
	real := filepath.Join(installed, "ndcli")
	if err := os.WriteFile(real, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	link := filepath.Join(bin, "ndcli")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	previous := executablePath
	executablePath = func() (string, error) { return link, nil }
	t.Cleanup(func() { executablePath = previous })

	got, err := locateCompanion()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(installed, companionFileName()))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("located %s, want %s", got, want)
	}
}

// PATH is never searched: the companion is handed the session's keys.
func TestTheCompanionIsNeverTakenFromPATH(t *testing.T) {
	elsewhere := installCompanion(t, scenario{})
	t.Setenv("PATH", elsewhere+string(os.PathListSeparator)+os.Getenv("PATH"))
	installedIn(t, t.TempDir())

	_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
	if failed := directFailure(t, err); failed.Failure != DirectCompanionMissing {
		t.Errorf("failure = %q, want %q", failed.Failure, DirectCompanionMissing)
	}
	notRecorded(t, elsewhere, "args")
}

// A companion of another release is told nothing: the configuration, which
// holds the keys, is never written to it.
func TestACompanionOfAnotherReleaseIsGivenNoKeys(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hello map[string]any
		want  string
	}{
		{"another version", map[string]any{"event": "hello", "version": "0.9.0", "protocol": companionProtocol},
			"is version 0.9.0 (tunnel generation 1), and this program is version dev (tunnel generation 1). Reinstall ndcli so both are replaced together"},
		{"another tunnel generation", map[string]any{"event": "hello", "version": config.Version, "protocol": 99},
			"is version dev (tunnel generation 99), and this program is version dev (tunnel generation 1). Reinstall ndcli so both are replaced together"},
		{"a version made to be printed", map[string]any{"event": "hello", "version": "1.0\x1b[2J\nmore", "protocol": companionProtocol},
			"is version 1.0[2J more (tunnel generation 1), and this program is version dev (tunnel generation 1). Reinstall ndcli so both are replaced together"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quickCompanionWaits(t)
			dir := installCompanion(t, scenario{"hello": tc.hello})
			_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
			failed := directFailure(t, err)
			if failed.Failure != DirectCompanionMismatch {
				t.Errorf("failure = %q, want %q", failed.Failure, DirectCompanionMismatch)
			}
			if want := filepath.Join(dir, companionFileName()) + " " + tc.want; failed.Message != want {
				t.Errorf("message = %q\n   want %q", failed.Message, want)
			}
			if got := recorded(t, dir, "stdin-closed"); got != "before the configuration" {
				t.Errorf("the companion saw stdin close %q, want before the configuration", got)
			}
			notRecorded(t, dir, "config.json")
		})
	}
}

func TestACompanionThatDoesNotSayHello(t *testing.T) {
	for _, tc := range []struct {
		name string
		sc   scenario
		want string
	}{
		{"silence", scenario{"hello": nil}, " did not start within 500ms; reinstall ndcli"},
		{"only lines that are not events", scenario{"hello": nil, "stray": []string{"panic: something", "goroutine 1 [running]:"}}, " did not start within 500ms; reinstall ndcli"},
		{"another event first", scenario{"hello": map[string]any{"event": "connected"}}, " did not start as expected; reinstall ndcli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quickCompanionWaits(t)
			dir := installCompanion(t, tc.sc)
			_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
			failed := directFailure(t, err)
			if failed.Failure != DirectCompanionFailed {
				t.Errorf("failure = %q, want %q", failed.Failure, DirectCompanionFailed)
			}
			if want := filepath.Join(dir, companionFileName()) + tc.want; failed.Message != want {
				t.Errorf("message = %q\n   want %q", failed.Message, want)
			}
			notRecorded(t, dir, "config.json")
		})
	}
}

func TestACompanionThatCannotBeStarted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, companionFileName())
	if err := os.WriteFile(path, []byte("not a program"), 0o700); err != nil {
		t.Fatal(err)
	}
	installedIn(t, dir)
	_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
	failed := directFailure(t, err)
	if failed.Failure != DirectCompanionFailed || !strings.HasPrefix(failed.Message, path+" could not be started: ") {
		t.Errorf("failure = %q, message = %q", failed.Failure, failed.Message)
	}
}

func TestTheCompanionsAccountOfAFailedSession(t *testing.T) {
	for _, tc := range []struct {
		name, fail, detail string
		failure            DirectFailure
		want               string
	}{
		{"the device did not answer", "no_answer", "", DirectNoAnswer,
			"the device did not answer on direct-peer + encrypted relay within 30s (relay relay.example.test:443)"},
		{"the relay cannot be reached", "relay_unreachable", "", DirectRelayUnreachable,
			"the relay relay.example.test:443 cannot be reached, and a session on direct-peer + encrypted relay starts through it"},
		{"the configuration was refused", "bad_config", "relay.port is not 1 to 65535\x1b[2J", DirectCompanionFailed,
			"netdefense-connect refused its configuration (relay.port is not 1 to 65535[2J)"},
		{"it could not do its part", "internal", "", DirectCompanionFailed, "netdefense-connect could not set the session up"},
		{"a reason this build does not know", "from_the_future", "tailcat: something", DirectCompanionFailed, "netdefense-connect could not set the session up"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := installCompanion(t, scenario{"fail": tc.fail, "detail": tc.detail})
			_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
			failed := directFailure(t, err)
			if failed.Failure != tc.failure {
				t.Errorf("failure = %q, want %q", failed.Failure, tc.failure)
			}
			if failed.Message != tc.want {
				t.Errorf("message = %q\n   want %q", failed.Message, tc.want)
			}
			namesNoLibrary(t, failed.Message)
			recorded(t, dir, "exited")
		})
	}
}

func TestACompanionThatLeavesBeforeTheSessionIsUp(t *testing.T) {
	for name, reason := range map[string]string{
		"saying it was stopped":  "terminated",
		"saying its parent left": "parent_closed",
	} {
		t.Run(name, func(t *testing.T) {
			dir := installCompanion(t, scenario{"leave": reason})
			_, err := directClient(t, directSession()).ConnectExecContext(t.Context())
			failed := directFailure(t, err)
			if failed.Failure != DirectCompanionFailed || failed.Message != "netdefense-connect stopped before the session was set up" {
				t.Errorf("failure = %q, message = %q", failed.Failure, failed.Message)
			}
			recorded(t, dir, "exited")
		})
	}
}

// A companion that never reports is not waited on for ever: it has the
// connect timeout, which it enforces itself, and a margin.
func TestACompanionThatNeverReports(t *testing.T) {
	quickCompanionWaits(t)
	dir := installCompanion(t, scenario{"never": true})
	session := directSession()
	session.ConnectTimeout = time.Second
	began := time.Now()
	_, err := directClient(t, session).ConnectExecContext(t.Context())
	failed := directFailure(t, err)
	if failed.Failure != DirectCompanionFailed || failed.Message != "netdefense-connect did not say within 1.5s whether the device answered" {
		t.Errorf("failure = %q, message = %q", failed.Failure, failed.Message)
	}
	if waited := time.Since(began); waited < 1500*time.Millisecond || waited > 5*time.Second {
		t.Errorf("waited %v, want the 1s connect timeout and the 500ms margin", waited)
	}
	if got := recorded(t, dir, "stdin-closed"); got != "while the parent waited" {
		t.Errorf("the companion saw stdin close %q, want while the parent waited", got)
	}
}

func TestGivingADirectSessionUpStopsTheCompanion(t *testing.T) {
	dir := installCompanion(t, scenario{"never": true})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		// Give the session up once the companion has its configuration.
		for {
			if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	defer cancel()
	_, err := directClient(t, directSession()).ConnectExecContext(ctx)
	var pairing *PathfinderError
	if !errors.As(err, &pairing) || !errors.Is(err, context.Canceled) || pairing.Message != "pairing failed: context canceled" {
		t.Fatalf("err = %v (%T), want pairing failed: context canceled", err, err)
	}
	if got := recorded(t, dir, "stdin-closed"); got != "while the parent waited" {
		t.Errorf("the companion saw stdin close %q, want while the parent waited", got)
	}
	recorded(t, dir, "exited")
}

func TestADirectSessionReportsProgressWhileTheDeviceIsAwaited(t *testing.T) {
	original := pairingHeartbeat
	pairingHeartbeat = 30 * time.Millisecond
	t.Cleanup(func() { pairingHeartbeat = original })

	installCompanion(t, scenario{"never": true})
	var mu sync.Mutex
	var got []string
	c, err := NewClient(ClientConfig{Direct: directSession(), OnProgress: func(m string) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, err := c.ConnectExecContext(ctx); err == nil {
		t.Fatal("expected an error: the device never answers")
	}
	mu.Lock()
	defer mu.Unlock()
	beats := 0
	for _, m := range got {
		if strings.HasPrefix(m, "Still waiting for the device to answer (") {
			beats++
			if !strings.Contains(m, "(0s left)") {
				t.Errorf("heartbeat %q counts down past the 300 ms deadline; want 0s left", m)
			}
		}
	}
	if beats < 3 {
		t.Errorf("got %d heartbeats in a 300 ms wait at 30 ms, want at least 3: %q", beats, got)
	}
}

// openDirectLink pairs through the fake companion and returns the link and a
// stream manager on it.
func openDirectLink(t *testing.T, session *DirectSession) (*companionLink, *StreamManager) {
	t.Helper()
	ln, err := directClient(t, session).pairDirect(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	mgr := NewStreamManager(ln)
	ln.SetStreamManager(mgr)
	return ln, mgr
}

func waitEnded(t *testing.T, ln *companionLink) {
	t.Helper()
	select {
	case <-ln.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the link did not end")
	}
}

func TestHowADirectSessionEnds(t *testing.T) {
	for _, tc := range []struct {
		end, reason string
	}{
		{"peer_closed", "the device ended the session"},
		{"terminated", "netdefense-connect was stopped"},
		{"crash", "netdefense-connect stopped unexpectedly"},
		{"parent_closed", "netdefense-connect stopped unexpectedly"},
	} {
		t.Run(tc.end, func(t *testing.T) {
			quickCompanionWaits(t)
			installCompanion(t, scenario{"end": tc.end, "end_after_ms": 150})
			ln, mgr := openDirectLink(t, directSession())
			stream, err := mgr.OpenStream("exec")
			if err != nil {
				t.Fatal(err)
			}
			if !ln.IsConnected() || ln.EndReason() != "" {
				t.Fatalf("before the end: connected = %v, reason = %q", ln.IsConnected(), ln.EndReason())
			}
			// A reader is woken by the end, and finds the reason already there.
			if _, err := io.ReadAll(stream); err != nil {
				t.Fatalf("read: %v", err)
			}
			if got := ln.EndReason(); got != tc.reason {
				t.Errorf("reason when the stream closed = %q, want %q", got, tc.reason)
			}
			waitEnded(t, ln)
			if ln.IsConnected() {
				t.Error("still connected after the end")
			}
			if err := ln.SendFrame(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: []byte("x")})); err == nil || err.Error() != "not connected" {
				t.Errorf("SendFrame after the end = %v, want not connected", err)
			}
			namesNoLibrary(t, ln.EndReason())
		})
	}
}

// The device's last bytes travel on stdout while the event that ends the
// session travels on stderr; the end must not overtake them.
func TestTheDevicesLastBytesArriveBeforeTheEnd(t *testing.T) {
	installCompanion(t, scenario{"end": "peer_closed", "end_after_ms": 150, "last_words": "goodbye"})
	ln, mgr := openDirectLink(t, directSession())
	stream, err := mgr.OpenStream("exec")
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if want := base64.StdEncoding.EncodeToString([]byte("goodbye")); !strings.Contains(string(all), want) {
		t.Errorf("the stream ended with %q, without the device's last bytes", all)
	}
	waitEnded(t, ln)
}

func TestACommandInFlightLearnsWhyTheSessionEnded(t *testing.T) {
	linkTimers(t, time.Hour, 300*time.Millisecond, 20*time.Millisecond)
	installCompanion(t, scenario{})
	h, err := directClient(t, directSession()).ConnectExecContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	_, _, _, _, err = h.Exec(t.Context(), "hang", time.Minute)
	var ended *SessionEndedError
	if !errors.As(err, &ended) || ended.Reason != "the device stopped responding" {
		t.Fatalf("err = %v, want the session ended: the device stopped responding", err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the handle is not done")
	}
}

// A companion that is killed sends nothing, so a device that has gone looks
// like a quiet one. Each end therefore sends a keepalive when it has nothing
// else to send, and ends a session on which nothing arrives.
func TestASessionOnWhichNothingArrivesEnds(t *testing.T) {
	linkTimers(t, time.Hour, 300*time.Millisecond, 20*time.Millisecond)
	installCompanion(t, scenario{})
	began := time.Now()
	ln, _ := openDirectLink(t, directSession())
	waitEnded(t, ln)
	if got := ln.EndReason(); got != "the device stopped responding" {
		t.Errorf("reason = %q, want the device stopped responding", got)
	}
	if waited := time.Since(began); waited < 300*time.Millisecond {
		t.Errorf("the session ended after %v, before the 300ms it waits for a frame", waited)
	}
}

func TestTheDevicesKeepAlivesKeepAQuietSessionUp(t *testing.T) {
	linkTimers(t, time.Hour, 300*time.Millisecond, 20*time.Millisecond)
	installCompanion(t, scenario{"keepalive_ms": 50})
	ln, _ := openDirectLink(t, directSession())
	var delivered atomic.Int32
	ln.OnFrame(func([]byte) { delivered.Add(1) })

	select {
	case <-ln.Done():
		t.Fatalf("the session ended (%s) while the device was sending keepalives", ln.EndReason())
	case <-time.After(time.Second):
	}
	if n := delivered.Load(); n != 0 {
		t.Errorf("%d keepalive frames were handed on as if they belonged to a stream", n)
	}
}

func TestAQuietSessionSendsKeepAlives(t *testing.T) {
	linkTimers(t, 100*time.Millisecond, time.Hour, 20*time.Millisecond)
	dir := installCompanion(t, scenario{})
	ln, mgr := openDirectLink(t, directSession())
	stream, err := mgr.OpenStream("exec")
	if err != nil {
		t.Fatal(err)
	}

	keepAlives := func() int {
		raw, _ := os.ReadFile(filepath.Join(dir, "frames"))
		return strings.Count(string(raw), "5 0 0\n")
	}
	deadline := time.Now().Add(10 * time.Second)
	for keepAlives() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the device received %d keepalives from a quiet session, want at least 3", keepAlives())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// While frames are being sent anyway, none is needed.
	before := keepAlives()
	for range 30 {
		if _, err := stream.Write([]byte(`{"id":"x","command":"hang"}`)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after := keepAlives(); after > before+1 {
		t.Errorf("%d keepalives were sent while the session was sending frames", after-before)
	}
	_ = ln
}

func TestACompanionThatWillNotLeaveIsKilled(t *testing.T) {
	quickCompanionWaits(t)
	installCompanion(t, scenario{"stubborn": true})
	ln, _ := openDirectLink(t, directSession())
	_ = ln.Close()
	select {
	case <-ln.proc.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the companion is still running after it was told to leave")
	}
}

// A sender of data waits when the companion is not taking frames, and the
// frames that keep the stream protocol going do not.
func TestDataWaitsForRoomAndControlFramesDoNot(t *testing.T) {
	quickCompanionWaits(t)
	installCompanion(t, scenario{"deaf": true})
	ln, _ := openDirectLink(t, directSession())

	chunk := EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: make([]byte, 64*1024)})
	sent := make(chan int, 1)
	blockedErr := make(chan error, 1)
	go func() {
		n := 0
		for {
			progress := make(chan error, 1)
			go func() { progress <- ln.SendFrame(chunk) }()
			select {
			case err := <-progress:
				if err != nil {
					blockedErr <- err
					return
				}
				n++
			case <-time.After(300 * time.Millisecond):
				sent <- n
				blockedErr <- <-progress
				return
			}
		}
	}()

	var accepted int
	select {
	case accepted = <-sent:
	case <-time.After(20 * time.Second):
		t.Fatal("a sender of data never had to wait")
	}
	// The queue holds linkQueueBytes; the pipe to the companion holds a
	// little more.
	if low, high := linkQueueBytes/len(chunk), linkQueueBytes/len(chunk)+8; accepted < low || accepted > high {
		t.Errorf("%d frames were accepted before the sender waited, want %d to %d", accepted, low, high)
	}

	control := make(chan error, 1)
	go func() { control <- ln.SendFrame(EncodeFrame(&Frame{Type: FrameTypeClose, StreamID: 1})) }()
	select {
	case err := <-control:
		if err != nil {
			t.Errorf("a control frame was refused: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("a control frame waited for room")
	}

	_ = ln.Close()
	select {
	case err := <-blockedErr:
		if err == nil || err.Error() != "not connected" {
			t.Errorf("the waiting sender got %v, want not connected", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the link did not release the waiting sender")
	}
}

func TestAFrameTooLargeToBeOneEndsTheSession(t *testing.T) {
	quickCompanionWaits(t)
	installCompanion(t, scenario{"frame_bytes": maxStreamFrameData + 1})
	ln, _ := openDirectLink(t, directSession())
	waitEnded(t, ln)
	if got := ln.EndReason(); got != "the session's data could not be read" {
		t.Errorf("reason = %q", got)
	}
}

func TestTheLargestFrameIsDelivered(t *testing.T) {
	if maxStreamFrameData != 65536 {
		t.Fatalf("a frame carries at most %d bytes; both ends of a session agreed on 65536", maxStreamFrameData)
	}
	installCompanion(t, scenario{"frame_bytes": maxStreamFrameData})
	ln, err := directClient(t, directSession()).pairDirect(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan []byte, 1)
	ln.OnFrame(func(frame []byte) {
		select {
		case got <- frame:
		default:
		}
	})
	select {
	case frame := <-got:
		if len(frame) != frameHeaderLen+maxStreamFrameData || frame[0] != FrameTypeData {
			t.Errorf("delivered a frame of %d bytes, type %d", len(frame), frame[0])
		}
	case <-ln.Done():
		t.Fatalf("the session ended on a frame of the largest size: %s", ln.EndReason())
	case <-time.After(10 * time.Second):
		t.Fatal("the frame was not delivered")
	}
}

func TestThePathIsPassedOn(t *testing.T) {
	installCompanion(t, scenario{"paths": []string{"relayed", "direct", "relayed"}})
	session := directSession()
	paths := make(chan bool, 8)
	session.OnPath = func(direct bool) { paths <- direct }
	openDirectLink(t, session)
	for i, want := range []bool{false, true, false} {
		select {
		case got := <-paths:
			if got != want {
				t.Errorf("path %d: direct = %v, want %v", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("path %d was not passed on", i+1)
		}
	}
}

// A line on stderr that is not an event (a crash trace, a library's print)
// is never taken for one and never ends the session.
func TestLinesThatAreNotEventsAreIgnored(t *testing.T) {
	installCompanion(t, scenario{"stray": []string{"panic: something", "", "{not json", `{"no":"event"}`, `{"event":"log","message":"tailcat: a library line"}`}})
	h, err := directClient(t, directSession()).ConnectExecContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if stdout, _, _, _, err := h.Exec(t.Context(), "still here", time.Minute); err != nil || stdout != "ran: still here" {
		t.Fatalf("Exec = %q, %v", stdout, err)
	}
}

// The interactive connect, with no terminal: the webadmin tunnel serves
// through the companion until the device ends the session.
func TestAnInteractiveDirectSessionServesTheTunnel(t *testing.T) {
	installCompanion(t, scenario{"end": "peer_closed", "end_after_ms": 1500})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	paired := make(chan struct{})
	c, err := NewClient(ClientConfig{
		Direct:           directSession(),
		WebAdminEnabled:  true,
		WebAdminListener: listener,
		OnPaired:         func() { close(paired) },
	})
	if err != nil {
		t.Fatal(err)
	}
	link := make(linkWriter, 1)
	c.linkOut = link
	finished := make(chan error, 1)
	go func() { finished <- c.ConnectContext(t.Context()) }()

	select {
	case <-paired:
	case err := <-finished:
		t.Fatalf("the session ended before it paired: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not pair")
	}
	// As a script does: the printed link with a cookie jar, then the page.
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	var addr string
	select {
	case addr = <-link:
	case <-time.After(10 * time.Second):
		t.Fatal("no tunnel link on stderr")
	}
	exchange, err := browser.Get(addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = exchange.Body.Close()
	resp, err := browser.Get(strings.Split(addr, "?")[0])
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "device" {
		t.Errorf("through the tunnel: %q, want the device's reply", body)
	}

	select {
	case err := <-finished:
		if err != nil {
			t.Errorf("ConnectContext = %v, want nil once the device ended the session", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the session did not end when the device ended it")
	}
}

func TestTheCompanionsEnvironment(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/a/file")
	t.Setenv("SSL_CERT_DIR", "")
	t.Setenv("TS_DEBUG_ADDR", "1")
	t.Setenv("HOME", "/home/someone")
	t.Setenv("GODEBUG", "x509ignoreCN=0")
	got := companionEnvironment()
	if want := []string{"SSL_CERT_FILE=/a/file", "SSL_CERT_DIR="}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("environment = %q, want %q", got, want)
	}
	if err := os.Unsetenv("SSL_CERT_FILE"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("SSL_CERT_DIR"); err != nil {
		t.Fatal(err)
	}
	// Empty, and not nil: a nil environment hands the child this process's.
	if got := companionEnvironment(); got == nil || len(got) != 0 {
		t.Errorf("environment = %#v, want an empty one that is not nil", got)
	}
}

// The websocket relay's settings are not asked of a direct-peer session.
func TestADirectSessionNeedsNoRelayHost(t *testing.T) {
	cfg := config.Get()
	host := cfg.Pathfinder.Host
	cfg.Pathfinder.Host = ""
	t.Cleanup(func() { cfg.Pathfinder.Host = host })

	if _, err := NewClient(ClientConfig{SessionID: "k"}); !errors.Is(err, ErrPathfinderNotConfigured) {
		t.Errorf("a websocket relay session without a relay host: %v, want %v", err, ErrPathfinderNotConfigured)
	}
	if _, err := NewClient(ClientConfig{Direct: directSession()}); err != nil {
		t.Errorf("a direct-peer session without a relay host: %v", err)
	}
}

// A connect may be given up at the very moment the device answers: both are
// then ready, and which one a select takes is chance. Whoever gave the
// connect up must never be handed a session, as on the websocket relay.
func TestAConnectGivenUpAsTheDeviceAnswersGetsNoSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := companionConfig{ConnectTimeoutSeconds: 30}
	for i := range 200 {
		events := make(chan companionEvent, 1)
		events <- companionEvent{Event: companionConnected}
		p := &companion{events: events, waits: companionTiming}
		if err := p.awaitConnected(ctx, cfg); !errors.Is(err, context.Canceled) {
			t.Fatalf("attempt %d: awaitConnected = %v, want the connect given up", i+1, err)
		}
	}

	// With nothing given up, the same answer is a session.
	events := make(chan companionEvent, 1)
	events <- companionEvent{Event: companionConnected}
	p := &companion{events: events, waits: companionTiming}
	if err := p.awaitConnected(context.Background(), cfg); err != nil {
		t.Errorf("awaitConnected = %v, want the session", err)
	}
}

// A version comes from another process and goes into a message: it is cut to
// one short printable word, by characters, so a cut never splits one.
func TestAVersionFromTheCompanionIsCutByCharacters(t *testing.T) {
	accented := strings.Repeat("\U000000e9", 50)
	got := displayVersion(accented)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 40 {
		t.Errorf("a version of 50 two-byte characters was cut to %d characters, valid = %v; want 40 whole ones", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	for in, want := range map[string]string{
		"1.50.0":                   "1.50.0",
		"":                         "unknown",
		"   ":                      "unknown",
		strings.Repeat("9", 45):    strings.Repeat("9", 40),
		"1.0\x1b[31m\nsecond line": "1.0[31m second line",
	} {
		if got := displayVersion(in); got != want {
			t.Errorf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// Once the companion has the keys, this process keeps none: not in the
// client, and not in the session value the caller still holds.
func TestTheSessionsKeysAreClearedOnceTheCompanionHasThem(t *testing.T) {
	installCompanion(t, scenario{})
	session := directSession()
	c, err := NewClient(ClientConfig{Direct: session})
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.ConnectExecContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var zero [32]byte
	if session.ClientKey != zero || session.DeviceKey != zero || session.PresharedKey != zero {
		t.Error("the session's keys are still in memory after the companion was configured")
	}
	if stdout, _, _, _, err := h.Exec(t.Context(), "uname -a", time.Minute); err != nil || stdout != "ran: uname -a" {
		t.Errorf("the session does not work: %q, %v", stdout, err)
	}
}

// linkWriter hands on the tunnel link a session prints, as a script reads it.
type linkWriter chan string

func (w linkWriter) Write(p []byte) (int, error) {
	if u, ok := strings.CutPrefix(strings.TrimSpace(string(p)), "WebAdmin tunnel: "); ok {
		select {
		case w <- u:
		default:
		}
	}
	return len(p), nil
}
