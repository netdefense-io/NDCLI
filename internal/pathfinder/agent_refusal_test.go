package pathfinder

import (
	"errors"
	"testing"
	"time"
)

// The device's report of a refused relay upgrade (NDAgent #286) reads as the
// refusal ndcli would have met itself: a 429 with its Retry-After, a 503 as a
// busy relay, anything else as nothing.
func TestAgentRelayRefusal(t *testing.T) {
	prefix := "Relay session ended: connect failed: dial failed: "
	var rl *RateLimitedError
	if err := AgentRelayRefusal(prefix + "the relay answered HTTP 429 (Retry-After: 15): websocket: bad handshake"); !errors.As(err, &rl) || rl.RetryAfter != 15*time.Second {
		t.Errorf("429 with Retry-After: %v", err)
	}
	if err := AgentRelayRefusal(prefix + "the relay answered HTTP 429: websocket: bad handshake"); !errors.As(err, &rl) || rl.RetryAfter != 0 {
		t.Errorf("429 without Retry-After: %v", err)
	}
	var busy *RelayBusyError
	if err := AgentRelayRefusal(prefix + "the relay answered HTTP 503 (Retry-After: 15): websocket: bad handshake"); !errors.As(err, &busy) || busy.RetryAfter != 15*time.Second {
		t.Errorf("503 with Retry-After: %v", err)
	}
	for _, huge := range []string{"9999999999", "99999999999999999999999"} {
		if err := AgentRelayRefusal(prefix + "the relay answered HTTP 503 (Retry-After: " + huge + "): x"); !errors.As(err, &busy) || busy.RetryAfter != maxRelayRetryAfter {
			t.Errorf("Retry-After %s: %v, want it capped", huge, err)
		}
	}
	for _, msg := range []string{
		prefix + "the relay answered HTTP 403: websocket: bad handshake",
		prefix + "the relay answered HTTP 403: proxy said the relay answered HTTP 429 (Retry-After: 1)",
		prefix + "the relay answered HTTP 4290: x",
		"Relay session ended: the relay answered HTTP 429 (Retry-After: 15)",
		"Relay session ended: connect failed: dial failed: websocket: bad handshake",
		"device went offline",
		"",
	} {
		if err := AgentRelayRefusal(msg); err != nil {
			t.Errorf("%q: %v, want nil", msg, err)
		}
	}
}
