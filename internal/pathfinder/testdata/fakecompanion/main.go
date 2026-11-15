// fakecompanion stands in for netdefense-connect in the tests of package
// pathfinder. Like the real one it takes no arguments and is given no
// environment, so what it does comes from scenario.json beside its
// executable. It records what it was started with and what it was sent in
// files there, and plays the device itself: it answers the frames of the
// stream protocol, written here a second time on purpose.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type scenario struct {
	// Hello is the first event; nil says nothing at all.
	Hello        *event `json:"hello"`
	HelloDelayMS int    `json:"hello_delay_ms"`
	// Stray lines are written to stderr, as they are, after the hello.
	Stray []string `json:"stray"`
	// Fail, when set, is the reason of an error event sent in place of
	// connected, with Detail.
	Fail           string `json:"fail"`
	Detail         string `json:"detail"`
	ConnectDelayMS int    `json:"connect_delay_ms"`
	// Never leaves the parent waiting: no connected, no error.
	Never bool `json:"never"`
	// Leave, when set, is the reason of a closed event sent in place of
	// connected.
	Leave string `json:"leave"`
	// Paths are path events sent after connected.
	Paths []string `json:"paths"`
	// End, EndAfterMS after connected: peer_closed, terminated, parent_closed,
	// crash (leave without a word) or "" (run until stdin closes).
	End        string `json:"end"`
	EndAfterMS int    `json:"end_after_ms"`
	// LastWords is sent on the exec stream right before a peer_closed end.
	LastWords string `json:"last_words"`
	// FrameBytes, when set, sends one data frame of that many bytes on
	// stream 7 once connected.
	FrameBytes int `json:"frame_bytes"`
	// Deaf stops reading stdin after the configuration, so frames pile up.
	Deaf bool `json:"deaf"`
	// Stubborn keeps running after stdin closes.
	Stubborn bool `json:"stubborn"`
	// KeepAliveMS, when set, is how often the device sends a keepalive frame.
	KeepAliveMS int `json:"keepalive_ms"`
}

type event struct {
	Event    string `json:"event"`
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	Path     string `json:"path,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Message  string `json:"message,omitempty"`
}

const (
	frameData      = 0x01
	frameClose     = 0x02
	frameOpen      = 0x03
	frameAck       = 0x04
	frameKeepAlive = 0x05
)

var (
	dir      string
	stderrMu sync.Mutex
	stdoutMu sync.Mutex
)

// record writes a file a test waits for. It appears whole or not at all: a
// test that finds it reads it at once.
func record(name, content string) {
	tmp := filepath.Join(dir, "."+name+".tmp")
	if os.WriteFile(tmp, []byte(content), 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, name))
	}
}

func emit(ev event) {
	line, _ := json.Marshal(ev)
	stderrMu.Lock()
	defer stderrMu.Unlock()
	_, _ = os.Stderr.Write(append(line, '\n'))
}

func sendFrame(kind byte, stream uint32, data []byte) {
	frame := make([]byte, 9+len(data))
	frame[0] = kind
	binary.BigEndian.PutUint32(frame[1:5], stream)
	binary.BigEndian.PutUint32(frame[5:9], uint32(len(data)))
	copy(frame[9:], data)
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_, _ = os.Stdout.Write(frame)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(3)
	}
	dir = filepath.Dir(exe)
	defer record("exited", "")
	record("pid", fmt.Sprint(os.Getpid()))
	record("args", strings.Join(os.Args[1:], "\n"))
	record("env", strings.Join(os.Environ(), "\n"))

	var sc scenario
	raw, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil || json.Unmarshal(raw, &sc) != nil {
		os.Exit(3)
	}

	time.Sleep(time.Duration(sc.HelloDelayMS) * time.Millisecond)
	if sc.Hello != nil {
		emit(*sc.Hello)
	}
	for _, line := range sc.Stray {
		stderrMu.Lock()
		fmt.Fprintln(os.Stderr, line)
		stderrMu.Unlock()
	}

	in := bufio.NewReader(os.Stdin)
	var header [4]byte
	if _, err := io.ReadFull(in, header[:]); err != nil {
		record("stdin-closed", "before the configuration")
		return
	}
	body := make([]byte, binary.BigEndian.Uint32(header[:]))
	if _, err := io.ReadFull(in, body); err != nil {
		return
	}
	record("config.json", string(body))

	if sc.Never {
		_, _ = io.Copy(io.Discard, in)
		record("stdin-closed", "while the parent waited")
		return
	}
	time.Sleep(time.Duration(sc.ConnectDelayMS) * time.Millisecond)
	if sc.Fail != "" {
		emit(event{Event: "error", Reason: sc.Fail, Detail: sc.Detail})
		record("exited", "")
		os.Exit(1)
	}
	if sc.Leave != "" {
		emit(event{Event: "closed", Reason: sc.Leave})
		return
	}
	emit(event{Event: "connected"})
	for _, path := range sc.Paths {
		emit(event{Event: "path", Path: path})
	}
	if sc.FrameBytes > 0 {
		sendFrame(frameData, 7, make([]byte, sc.FrameBytes))
	}

	if sc.KeepAliveMS > 0 {
		go func() {
			for {
				time.Sleep(time.Duration(sc.KeepAliveMS) * time.Millisecond)
				sendFrame(frameKeepAlive, 0, nil)
			}
		}()
	}

	stdinClosed := make(chan struct{})
	execStream := make(chan uint32, 1)
	go func() {
		defer close(stdinClosed)
		if sc.Deaf {
			// Take nothing from the pipe, until the parent kills this process.
			time.Sleep(time.Hour)
		}
		device(in, execStream)
		record("stdin-closed", "during the session")
	}()

	var end <-chan time.Time
	if sc.End != "" {
		end = time.After(time.Duration(sc.EndAfterMS) * time.Millisecond)
	}
	select {
	case <-stdinClosed:
		if sc.Stubborn {
			time.Sleep(time.Hour)
		}
		emit(event{Event: "closed", Reason: "parent_closed"})
	case <-end:
		switch sc.End {
		case "crash":
			record("exited", "")
			os.Exit(2)
		case "peer_closed":
			if sc.LastWords != "" {
				select {
				case stream := <-execStream:
					answer, _ := json.Marshal(map[string]any{"id": "last", "type": "stdout", "data": base64.StdEncoding.EncodeToString([]byte(sc.LastWords))})
					sendFrame(frameData, stream, answer)
				case <-time.After(5 * time.Second):
				}
			}
		}
		emit(event{Event: "closed", Reason: sc.End})
	}
}

// device answers the stream protocol: it acknowledges every stream, runs
// "commands" on an exec stream and serves one canned reply on a webadmin one.
func device(in *bufio.Reader, execStream chan<- uint32) {
	services := map[uint32]string{}
	var frames strings.Builder
	for {
		var header [9]byte
		if _, err := io.ReadFull(in, header[:]); err != nil {
			return
		}
		kind := header[0]
		stream := binary.BigEndian.Uint32(header[1:5])
		data := make([]byte, binary.BigEndian.Uint32(header[5:9]))
		if _, err := io.ReadFull(in, data); err != nil {
			return
		}
		// Every frame received: its type, stream and length, one per line.
		fmt.Fprintf(&frames, "%d %d %d\n", kind, stream, len(data))
		record("frames", frames.String())
		switch kind {
		case frameOpen:
			services[stream] = string(data)
			if string(data) == "exec" {
				select {
				case execStream <- stream:
				default:
				}
			}
			sendFrame(frameAck, stream, nil)
		case frameData:
			switch services[stream] {
			case "exec":
				var req struct {
					ID      string `json:"id"`
					Command string `json:"command"`
				}
				if json.Unmarshal(data, &req) != nil || req.Command == "hang" {
					continue
				}
				out, _ := json.Marshal(map[string]any{"id": req.ID, "type": "stdout", "data": base64.StdEncoding.EncodeToString([]byte("ran: " + req.Command))})
				sendFrame(frameData, stream, out)
				result, _ := json.Marshal(map[string]any{"id": req.ID, "type": "result"})
				sendFrame(frameData, stream, result)
			case "webadmin":
				sendFrame(frameData, stream, []byte("HTTP/1.1 200 OK\r\nContent-Length: 6\r\nConnection: close\r\n\r\ndevice"))
				sendFrame(frameClose, stream, nil)
			}
		}
	}
}
