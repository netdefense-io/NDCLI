package pathfinder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// A session on the direct-peer + encrypted relay transport runs through
// netdefense-connect, a companion program installed beside this one. It lives
// in a module of its own (netdefense-connect/), which this module does not
// import, so the few things the two agree on are written here a second time.
// companion_contract_test.go reads the companion's source and fails when the
// two copies differ.

// transportName is the transport's name in every message.
const transportName = "direct-peer + encrypted relay"

// companionName is the companion's file name, without the Windows suffix.
const companionName = "netdefense-connect"

// companionProtocol is the tunnel generation of the companion this build is
// released with.
const companionProtocol = 1

// The events the companion writes, the values of a path, and its reasons.
const (
	companionHello     = "hello"
	companionConnected = "connected"
	companionPath      = "path"
	companionClosed    = "closed"
	companionFailed    = "error"
	companionLog       = "log"

	companionPathDirect  = "direct"
	companionPathRelayed = "relayed"

	companionParentClosed     = "parent_closed"
	companionPeerClosed       = "peer_closed"
	companionTerminated       = "terminated"
	companionBadConfig        = "bad_config"
	companionRelayUnreachable = "relay_unreachable"
	companionNoAnswer         = "no_answer"
	companionInternal         = "internal"
)

// companionEvent is one line of the companion's stderr.
type companionEvent struct {
	Event    string `json:"event"`
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	Path     string `json:"path,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Message  string `json:"message,omitempty"`
}

// companionKey is 32 bytes of key material, base64 in JSON.
type companionKey [32]byte

func (k companionKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(k[:]))
}

type companionRelay struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	STUNPort int    `json:"stun_port"`
}

// companionConfig is the message written to the companion's stdin before the
// session's bytes: a 4-byte big-endian length, then this object.
type companionConfig struct {
	Relay                 companionRelay `json:"relay"`
	Port                  int            `json:"port"`
	ClientKey             companionKey   `json:"client_key"`
	ServerKey             companionKey   `json:"server_key"`
	PresharedKey          companionKey   `json:"preshared_key"`
	ConnectTimeoutSeconds int            `json:"connect_timeout_seconds"`
	Debug                 bool           `json:"debug,omitempty"`
}

// DirectFailure says which part of setting a direct-peer session up failed.
type DirectFailure string

const (
	// DirectCompanionMissing: netdefense-connect is not installed beside
	// this program.
	DirectCompanionMissing DirectFailure = "companion_missing"
	// DirectCompanionMismatch: it is, but it belongs to another release.
	DirectCompanionMismatch DirectFailure = "companion_mismatch"
	// DirectCompanionFailed: it did not start, or did not do its part.
	DirectCompanionFailed DirectFailure = "companion_failed"
	// DirectRelayUnreachable: the device did not answer, and the relay could
	// not be reached.
	DirectRelayUnreachable DirectFailure = "relay_unreachable"
	// DirectNoAnswer: the relay can be reached, and the device did not
	// answer through it in time.
	DirectNoAnswer DirectFailure = "no_answer"
)

// DirectError is a session that could not be set up on the direct-peer +
// encrypted relay transport. Such a session is not tried again on the
// websocket relay: which transport a session uses is decided before it
// starts, and not by this program.
type DirectError struct {
	Failure DirectFailure
	Message string
}

func (e *DirectError) Error() string { return e.Message }

// companionWaits are the waits of running a companion.
type companionWaits struct {
	// hello is how long a started companion has to say who it is.
	hello time.Duration
	// connectMargin is how much longer than the connect timeout the
	// companion gets to report that the device did not answer.
	connectMargin time.Duration
	// exit is how long closing a session waits for the queued frames to be
	// handed over, and then for the companion to leave on its own; it is
	// killed killAfter after it was told to leave.
	exit      time.Duration
	killAfter time.Duration
	// lastBytes is how long a session the device ended waits for the
	// companion to hand over the device's last bytes.
	lastBytes time.Duration
	// An end of a direct-peer session that has sent nothing for keepAlive
	// sends a keepalive frame, and one that has received no frame at all for
	// receive ends the session: a companion that dies sends nothing, and
	// nothing else would tell. check is how often both are looked at.
	keepAlive time.Duration
	receive   time.Duration
	check     time.Duration
}

// companionTiming is what a session starts with; each session keeps its own
// copy. A variable so tests can shorten the waits.
var companionTiming = companionWaits{
	hello:         10 * time.Second,
	connectMargin: 15 * time.Second,
	exit:          time.Second,
	killAfter:     5 * time.Second,
	lastBytes:     3 * time.Second,
	keepAlive:     20 * time.Second,
	receive:       60 * time.Second,
	check:         time.Second,
}

// executablePath is this program's path. A variable so tests can stand in
// for it.
var executablePath = os.Executable

// locateCompanion finds netdefense-connect in the directory this program is
// installed in, after resolving symbolic links, since a package manager may
// start the program through one. It never looks in PATH: the companion is
// handed the session's keys, so it must be the one installed with this
// program and nothing else.
func locateCompanion() (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", &DirectError{Failure: DirectCompanionMissing, Message: fmt.Sprintf("cannot tell where this program is installed (%v), so %s, which a session on %s needs, cannot be found", err, companionName, transportName)}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	name := companionName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", &DirectError{Failure: DirectCompanionMissing, Message: fmt.Sprintf("%s is missing from %s. A session on %s needs it: reinstall ndcli, which installs both", companionName, dir, transportName)}
	}
	return path, nil
}

// companionEnvironment is the environment a companion runs in: only the
// variables that choose the trusted certificate authorities, so it trusts
// what this program trusts. Everything else stays out, the debug switches of
// its libraries above all: one of them logs the session's address, which
// holds the pre-shared key. On Windows, where sockets need SYSTEMROOT,
// os/exec adds it to any environment that lacks it.
func companionEnvironment() []string {
	env := []string{}
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// companion is a started netdefense-connect and its three pipes.
type companion struct {
	path   string
	cmd    *exec.Cmd
	stdin  *os.File
	stdout *os.File
	// events yields each event line. It is closed when stderr ends, which
	// is when the process has gone.
	events chan companionEvent
	// exited is closed once the process has been collected.
	exited   chan struct{}
	stopOnce sync.Once
	waits    companionWaits
}

// startCompanion starts the companion at path and checks its hello. Only a
// companion of this release, on this build's tunnel generation, is handed
// anything.
func startCompanion(path string) (*companion, error) {
	// The pipes are made here, not by exec.Cmd, whose own are closed by Wait:
	// these are read to their end whenever the process leaves.
	childIn, stdin, err := os.Pipe()
	if err != nil {
		return nil, companionStartError(path, err)
	}
	stdout, childOut, err := os.Pipe()
	if err != nil {
		closeAll(childIn, stdin)
		return nil, companionStartError(path, err)
	}
	stderr, childErr, err := os.Pipe()
	if err != nil {
		closeAll(childIn, stdin, stdout, childOut)
		return nil, companionStartError(path, err)
	}

	cmd := exec.Command(path)
	cmd.Env = companionEnvironment()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = childIn, childOut, childErr
	err = cmd.Start()
	closeAll(childIn, childOut, childErr)
	if err != nil {
		closeAll(stdin, stdout, stderr)
		return nil, companionStartError(path, err)
	}

	p := &companion{
		path:   path,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		events: make(chan companionEvent, 64),
		exited: make(chan struct{}),
		waits:  companionTiming,
	}
	go p.readEvents(stderr)
	go func() {
		_ = cmd.Wait()
		close(p.exited)
	}()

	select {
	case hello, ok := <-p.events:
		if !ok || hello.Event != companionHello {
			p.discard()
			return nil, &DirectError{Failure: DirectCompanionFailed, Message: fmt.Sprintf("%s did not start as expected; reinstall ndcli", path)}
		}
		if hello.Version != config.Version || hello.Protocol != companionProtocol {
			p.discard()
			return nil, &DirectError{Failure: DirectCompanionMismatch, Message: fmt.Sprintf("%s is version %s (tunnel generation %d), and this program is version %s (tunnel generation %d). Reinstall ndcli so both are replaced together", path, displayVersion(hello.Version), hello.Protocol, displayVersion(config.Version), companionProtocol)}
		}
	case <-time.After(p.waits.hello):
		p.discard()
		return nil, &DirectError{Failure: DirectCompanionFailed, Message: fmt.Sprintf("%s did not start within %s; reinstall ndcli", path, p.waits.hello)}
	}
	return p, nil
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

func companionStartError(path string, err error) error {
	return &DirectError{Failure: DirectCompanionFailed, Message: fmt.Sprintf("%s could not be started: %v", path, err)}
}

// displayVersion is a version as it reads in a message. It may come from
// another process, so it is cut to one short printable word.
func displayVersion(v string) string {
	v = cleanReason(v)
	if r := []rune(v); len(r) > 40 {
		v = string(r[:40])
	}
	if v == "" {
		return "unknown"
	}
	return v
}

// readEvents turns the companion's stderr into events. Its log lines, and
// any line that is not an event (a crash trace, say), go to the debug log
// and nowhere else: their text is not written by this program.
func (p *companion) readEvents(stderr *os.File) {
	defer close(p.events)
	defer func() { _ = stderr.Close() }()
	lines := bufio.NewScanner(stderr)
	lines.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lines.Scan() {
		var ev companionEvent
		if err := json.Unmarshal(lines.Bytes(), &ev); err != nil || ev.Event == "" {
			debugLog("%s: %s", companionName, lines.Text())
			continue
		}
		if ev.Event == companionLog {
			debugLog("%s: %s", companionName, ev.Message)
			continue
		}
		p.events <- ev
	}
	// A line longer than the buffer ends the scan; the rest is still read,
	// so the companion never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, stderr)
}

// configure writes the configuration message.
func (p *companion) configure(cfg companionConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	defer clear(body)
	var message bytes.Buffer
	_ = binary.Write(&message, binary.BigEndian, uint32(len(body)))
	message.Write(body)
	defer clear(message.Bytes())
	_, err = p.stdin.Write(message.Bytes())
	return err
}

// awaitConnected waits for the companion to reach the device, and returns
// the companion's own account of a failure as a DirectError. It leaves the
// companion running whatever it returns: the caller discards it on an error.
func (p *companion) awaitConnected(ctx context.Context, cfg companionConfig) error {
	limit := time.Duration(cfg.ConnectTimeoutSeconds)*time.Second + p.waits.connectMargin
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	stopped := &DirectError{Failure: DirectCompanionFailed, Message: companionName + " stopped before the session was set up"}
	for {
		select {
		case ev, ok := <-p.events:
			if !ok {
				return stopped
			}
			switch ev.Event {
			case companionConnected:
				// ctx may have ended just as the device answered. The
				// connect is then given up like any other before the session
				// is up, as on the websocket relay: whoever gave it up is
				// never handed a session.
				if err := ctx.Err(); err != nil {
					return err
				}
				return nil
			case companionFailed:
				return companionFailure(ev, cfg)
			case companionClosed:
				return stopped
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return &DirectError{Failure: DirectCompanionFailed, Message: fmt.Sprintf("%s did not say within %s whether the device answered", companionName, limit)}
		}
	}
}

// companionFailure is the DirectError for an error event.
func companionFailure(ev companionEvent, cfg companionConfig) error {
	relay := fmt.Sprintf("%s:%d", cfg.Relay.Hostname, cfg.Relay.Port)
	switch ev.Reason {
	case companionNoAnswer:
		return &DirectError{Failure: DirectNoAnswer, Message: fmt.Sprintf("the device did not answer on %s within %ds (relay %s)", transportName, cfg.ConnectTimeoutSeconds, relay)}
	case companionRelayUnreachable:
		return &DirectError{Failure: DirectRelayUnreachable, Message: fmt.Sprintf("the relay %s cannot be reached, and a session on %s starts through it", relay, transportName)}
	case companionBadConfig:
		return &DirectError{Failure: DirectCompanionFailed, Message: fmt.Sprintf("%s refused its configuration (%s)", companionName, cleanReason(ev.Detail))}
	}
	return &DirectError{Failure: DirectCompanionFailed, Message: companionName + " could not set the session up"}
}

// stop tells the companion to leave: a closed stdin is its signal. It is
// killed if it is still there killAfter later.
func (p *companion) stop() {
	p.stopOnce.Do(func() {
		_ = p.stdin.Close()
		kill := time.AfterFunc(p.waits.killAfter, func() { _ = p.cmd.Process.Kill() })
		go func() {
			<-p.exited
			kill.Stop()
		}()
	})
}

// discard stops a companion no session will run through, and reads what it
// still writes, so nothing can hold it up on its way out.
func (p *companion) discard() {
	p.stop()
	go func() {
		_, _ = io.Copy(io.Discard, p.stdout)
		_ = p.stdout.Close()
	}()
	go func() {
		for range p.events {
		}
	}()
}

// companionEndReason is why a companion's closing event, or its leaving
// without one, ends a session, in the words shown to the user.
func companionEndReason(reason string) string {
	switch reason {
	case companionPeerClosed:
		return "the device ended the session"
	case companionTerminated:
		return companionName + " was stopped"
	}
	return companionName + " stopped unexpectedly"
}
