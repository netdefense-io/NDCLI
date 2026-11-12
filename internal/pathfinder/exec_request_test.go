package pathfinder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The exec request goes out as one frame: HTML characters are not escaped
// into six bytes each, and a request that would not fit in one relay frame is
// refused, not sent.
func TestEncodeExecRequest(t *testing.T) {
	payload, err := encodeExecRequest(execRequest{ID: "r1", Command: "echo '<a>' && cat x", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"r1","command":"echo '<a>' && cat x","timeout_seconds":60}`; string(payload) != want {
		t.Errorf("payload %s, want %s", payload, want)
	}
	var back execRequest
	if err := json.Unmarshal(payload, &back); err != nil || back.Command != "echo '<a>' && cat x" {
		t.Errorf("payload does not decode back: %v, %+v", err, back)
	}

	if _, err := encodeExecRequest(execRequest{ID: "r1", Command: strings.Repeat("<", MaxExecFrameBytes)}); err == nil {
		t.Error("an oversized request was encoded")
	}
	_, err = encodeExecRequest(execRequest{ID: "r1", Command: strings.Repeat("\x01", 32*1024)})
	var tooLarge *ExecRequestTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Size <= MaxExecFrameBytes {
		t.Errorf("32 KiB of control characters, escaped six bytes each: err %v, want ExecRequestTooLargeError", err)
	}
}

// Exec refuses a request too large for one relay frame without writing
// anything or recording an abandoned id, and the session stays usable.
func TestExecRefusesAnOversizedRequestUnsent(t *testing.T) {
	client, agent := newFakeStreamPair()
	h := newTestExecHandleViaOverride(client)
	defer h.Close()

	_, _, _, _, err := h.Exec(context.Background(), strings.Repeat("\x01", 32*1024), time.Second)
	var tooLarge *ExecRequestTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want ExecRequestTooLargeError", err)
	}
	if n := len(client.agentRx); n != 0 {
		t.Errorf("%d frame(s) written for a refused request", n)
	}
	h.abandonedMu.Lock()
	abandoned := len(h.abandoned)
	h.abandonedMu.Unlock()
	if abandoned != 0 {
		t.Errorf("%d abandoned id(s) recorded for a request never sent", abandoned)
	}

	go func() {
		req := agentReadRequest(t, agent)
		agentSend(t, agent, execResponse{ID: req.ID, Type: "result", ExitCode: 0})
	}()
	if _, _, code, _, err := h.Exec(context.Background(), "true", 5*time.Second); err != nil || code != 0 {
		t.Errorf("next command: exit %d, err %v; want it to run", code, err)
	}
}
