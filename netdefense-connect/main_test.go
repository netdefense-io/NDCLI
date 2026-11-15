package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"tailscale.com/tstest/deptest"
)

const testVersion = "9.9.9-test"

// companionPath is netdefense-connect built as a release builds it.
var companionPath string

func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

func buildAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "netdefense-connect-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	companionPath = filepath.Join(dir, "netdefense-connect")
	if runtime.GOOS == "windows" {
		companionPath += ".exe"
	}
	build := exec.Command("go", "build", "-tags", releaseTags(), "-ldflags", "-X main.version="+testVersion, "-o", companionPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building netdefense-connect: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

func releaseTags() string {
	tags, err := os.ReadFile("build-tags.txt")
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(tags))
}

// event mirrors the fields of an event line this file reads.
type event struct {
	Event    string `json:"event"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	Reason   string `json:"reason"`
	Detail   string `json:"detail"`
}

// child is one running netdefense-connect.
type child struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan event
	exited chan struct{}

	mu     sync.Mutex
	stdout bytes.Buffer
	stderr []string
}

// spawn starts the binary with no arguments and an empty environment, as a
// parent does.
func spawn(t *testing.T) *child {
	t.Helper()
	cmd := exec.Command(companionPath)
	cmd.Env = []string{}
	c := &child{t: t, cmd: cmd, events: make(chan event, 1024), exited: make(chan struct{})}
	var err error
	if c.stdin, err = cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			c.mu.Lock()
			c.stdout.Write(buf[:n])
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer readers.Done()
		defer close(c.events)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			c.mu.Lock()
			c.stderr = append(c.stderr, sc.Text())
			c.mu.Unlock()
			var ev event
			if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Event != "log" {
				c.events <- ev
			}
		}
	}()
	go func() {
		readers.Wait()
		_ = cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		_ = c.stdin.Close()
		select {
		case <-c.exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-c.exited
		}
	})
	return c
}

func (c *child) next() event {
	c.t.Helper()
	select {
	case ev, ok := <-c.events:
		if !ok {
			c.t.Fatalf("the events ended; stderr:\n%s", strings.Join(c.lines(), "\n"))
		}
		return ev
	case <-time.After(20 * time.Second):
		c.t.Fatalf("no event within 20s; stderr:\n%s", strings.Join(c.lines(), "\n"))
		return event{}
	}
}

func (c *child) expectHello() {
	c.t.Helper()
	if got, want := c.next(), (event{Event: "hello", Version: testVersion, Protocol: 1}); got != want {
		c.t.Fatalf("first event = %+v, want %+v", got, want)
	}
}

func (c *child) exitCode() int {
	c.t.Helper()
	select {
	case <-c.exited:
		return c.cmd.ProcessState.ExitCode()
	case <-time.After(20 * time.Second):
		c.t.Fatal("the process did not exit within 20s")
		return -1
	}
}

func (c *child) running() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

func (c *child) lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.stderr...)
}

func (c *child) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdout.String()
}

// checkPipes fails when stdout carried anything, or stderr a line that is
// not one JSON object: the parent reads both as protocol.
func (c *child) checkPipes() {
	c.t.Helper()
	if out := c.output(); out != "" {
		c.t.Errorf("stdout = %q; only session bytes may be written there", out)
	}
	for _, line := range c.lines() {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			c.t.Errorf("stderr line is not a JSON object: %q", line)
		}
	}
}

func (c *child) send(p []byte) {
	c.t.Helper()
	if _, err := c.stdin.Write(p); err != nil {
		c.t.Fatalf("write to stdin: %v", err)
	}
}

// deadRelayConfig is a configuration message whose relay is a loopback port
// nothing listens on, so the session keeps looking for its device.
func deadRelayConfig(t *testing.T, debug bool) []byte {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	key := func(b byte) string {
		k := make([]byte, 32)
		for i := range k {
			k[i] = b + byte(i)
		}
		k[0] &= 248
		k[31] = k[31]&127 | 64
		return `"` + base64.StdEncoding.EncodeToString(k) + `"`
	}
	body := fmt.Sprintf(`{"relay":{"hostname":"127.0.0.1","port":%d,"stun_port":%d},"port":4711,`+
		`"client_key":%s,"server_key":%s,"preshared_key":%s,"connect_timeout_seconds":120,"debug":%v}`,
		port, port, key(1), key(2), key(3), debug)
	return frame([]byte(body))
}

func frame(body []byte) []byte {
	out := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(out, uint32(len(body)))
	return append(out, body...)
}

func runOnce(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(companionPath, args...)
	cmd.Env = []string{}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return out.String(), errOut.String(), exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), 0
}

func TestVersion(t *testing.T) {
	stdout, stderr, code := runOnce(t, "--version")
	if stdout != "netdefense-connect "+testVersion+"\n" || stderr != "" || code != 0 {
		t.Errorf("--version: stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

func TestHelpNamesTheTransportAndNotItsLibrary(t *testing.T) {
	stdout, stderr, code := runOnce(t, "--help")
	if code != 0 || stderr != "" {
		t.Errorf("--help: stderr %q, exit %d", stderr, code)
	}
	if !strings.Contains(stdout, "direct-peer + encrypted relay") {
		t.Errorf("--help does not name the transport:\n%s", stdout)
	}
	lower := strings.ToLower(stdout)
	for _, word := range []string{"tailcat", "tailscale", "derp", "wireguard"} {
		if strings.Contains(lower, word) {
			t.Errorf("--help names %q:\n%s", word, stdout)
		}
	}
}

func TestAnUnknownArgumentIsRefused(t *testing.T) {
	stdout, stderr, code := runOnce(t, "connect")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "not meant to be run by hand") {
		t.Errorf("unknown argument: stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

func TestHelloThenACleanExitWhenNothingIsSent(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	_ = c.stdin.Close()
	if code := c.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if lines := c.lines(); len(lines) != 1 {
		t.Errorf("stderr = %q, want the hello line alone", lines)
	}
	c.checkPipes()
}

func TestARefusedConfigurationEndsTheProcess(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	c.send(frame([]byte(`{"relay":{"hostname":"relay.example.test","port":443,"stun_port":3478}}`)))
	if got, want := c.next(), (event{Event: "error", Reason: "bad_config", Detail: "port is not 1 to 65535"}); got != want {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if code := c.exitCode(); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	c.checkPipes()
}

// A parent that dies takes its companion along: the closed stdin is all it
// needs.
func TestClosingStdinEndsASessionStillLookingForItsDevice(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	c.send(deadRelayConfig(t, false))
	time.Sleep(time.Second)
	if !c.running() {
		t.Fatalf("the process exited while its stdin was open; stderr:\n%s", strings.Join(c.lines(), "\n"))
	}
	_ = c.stdin.Close()
	if got, want := c.next(), (event{Event: "closed", Reason: "parent_closed"}); got != want {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if code := c.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	c.checkPipes()
}

// With debug on, the tunnel's own log lines travel as events; nothing they
// say may reach stdout or break the one-object-per-line stream.
func TestTheTunnelsLogStaysInsideEvents(t *testing.T) {
	c := spawn(t)
	c.expectHello()
	c.send(deadRelayConfig(t, true))
	deadline := time.Now().Add(20 * time.Second)
	logged := func() bool {
		for _, line := range c.lines() {
			if strings.Contains(line, `"event":"log"`) {
				return true
			}
		}
		return false
	}
	for !logged() {
		if time.Now().After(deadline) {
			t.Fatalf("no log event within 20s; stderr:\n%s", strings.Join(c.lines(), "\n"))
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = c.stdin.Close()
	if code := c.exitCode(); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	c.checkPipes()
}

func TestTheReleaseBinaryLeavesOutWhatItNeverRuns(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			deptest.DepChecker{
				GOOS:   goos,
				GOARCH: "amd64",
				Tags:   releaseTags(),
				BadDeps: map[string]string{
					"github.com/tailscale/gliderssh":         "an SSH server",
					"golang.org/x/crypto/ssh":                "an SSH server",
					"github.com/pkg/sftp":                    "a file service",
					"golang.org/x/net/proxy":                 "outbound proxies",
					"tailscale.com/client/web":               "a web interface",
					"tailscale.com/tsweb":                    "a debug HTTP server",
					"tailscale.com/net/portmapper":           "port mapping on the local gateway",
					"tailscale.com/net/captivedetection":     "requests to third-party hosts",
					"tailscale.com/derp/derpserver":          "a relay server",
					"tailscale.com/tstest/integration":       "test scaffolding",
					"tailscale.com/feature/condregister":     "every optional feature at once",
					"tailscale.com/clientupdate":             "self-update",
					"tailscale.com/feature/taildrop":         "file transfer",
					"tailscale.com/ssh/tailssh":              "an SSH server",
					"tailscale.com/cmd/tailscaled/childproc": "child processes",
				},
			}.Check(t)
		})
	}
}
