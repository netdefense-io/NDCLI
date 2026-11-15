package companion

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// The events written to stderr, one JSON object per line.
const (
	// EventHello is always first: who this program is, before it is given
	// any key.
	EventHello = "hello"
	// EventConnected says the session's bytes now flow.
	EventConnected = "connected"
	// EventPath says how the device is reached, each time that changes.
	EventPath = "path"
	// EventClosed ends a session that needs no explanation from the tunnel.
	EventClosed = "closed"
	// EventError ends a session that could not be set up.
	EventError = "error"
	// EventLog is a line of the tunnel's own log, sent only when the
	// configuration asks for it. It is for a debug log, never for the user:
	// its text is written by third-party code.
	EventLog = "log"
)

// The values of a path event.
const (
	PathDirect  = "direct"
	PathRelayed = "relayed"
)

// The reasons of a closed event.
const (
	// ReasonParentClosed: the parent closed stdin, or stopped reading stdout.
	ReasonParentClosed = "parent_closed"
	// ReasonPeerClosed: the device closed the connection.
	ReasonPeerClosed = "peer_closed"
	// ReasonTerminated: this process was asked to stop.
	ReasonTerminated = "terminated"
)

// The reasons of an error event.
const (
	// ReasonBadConfig: the configuration message was refused; Detail says why.
	ReasonBadConfig = "bad_config"
	// ReasonRelayUnreachable: the device did not answer and the relay could
	// not be reached either.
	ReasonRelayUnreachable = "relay_unreachable"
	// ReasonNoAnswer: the relay is reachable, but the device did not answer
	// through it in time.
	ReasonNoAnswer = "no_answer"
	// ReasonInternal: this program could not do its part.
	ReasonInternal = "internal"
)

// Event is one line on stderr. Reason is one of the constants above; no
// field ever carries an error text from the tunnel library, except Message
// of a log event.
type Event struct {
	Event    string `json:"event"`
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	Path     string `json:"path,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Message  string `json:"message,omitempty"`
}

// maxLogMessage bounds the text of one log event.
const maxLogMessage = 4096

type emitter struct {
	mu sync.Mutex
	w  io.Writer
	// over is set by the closed or error event, which is the last one: a
	// path found or a log line written while the session winds down is
	// dropped, so the parent never reads an event after the end.
	over bool
}

// emit writes ev as one line, in one write.
func (e *emitter) emit(ev Event) {
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	line = append(line, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.over {
		return
	}
	e.over = ev.Event == EventClosed || ev.Event == EventError
	_, _ = e.w.Write(line)
}

// logf turns a log line into a log event.
func (e *emitter) logf(format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	if len(msg) > maxLogMessage {
		msg = msg[:maxLogMessage]
	}
	e.emit(Event{Event: EventLog, Message: msg})
}

// logWriter sends what the standard logger writes as log events.
type logWriter struct{ e *emitter }

func (w logWriter) Write(p []byte) (int, error) {
	w.e.logf("%s", p)
	return len(p), nil
}
