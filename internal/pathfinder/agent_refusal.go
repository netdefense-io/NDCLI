package pathfinder

import (
	"regexp"
	"strconv"
	"time"
)

// agentRelayRefusal matches how NDAgent (from #286) reports a relay that
// refused its upgrade, in a FAILED CONNECT task's message: "… dial failed: the
// relay answered HTTP 429 (Retry-After: 15): …". The parenthesis is there only
// when the relay sent whole seconds.
var agentRelayRefusal = regexp.MustCompile(`dial failed: the relay answered HTTP (\d{3})\b(?: \(Retry-After: (\d+)\))?`)

// AgentRelayRefusal reads the device's report of a refused relay upgrade:
// a 429 as a RateLimitedError, a 503 as a RelayBusyError, each with the
// Retry-After the relay sent, capped like a relay's own. Any other message,
// or another status, is nil. The message comes from the device, so nothing in
// it is trusted beyond the digits.
func AgentRelayRefusal(message string) error {
	m := agentRelayRefusal.FindStringSubmatch(message)
	if m == nil {
		return nil
	}
	var retryAfter time.Duration
	if m[2] != "" {
		// Capped before multiplying, so a huge value cannot wrap; one too long
		// for an int64 is as long as the cap.
		retryAfter = maxRelayRetryAfter
		if secs, err := strconv.ParseInt(m[2], 10, 64); err == nil {
			retryAfter = time.Duration(min(secs, int64(maxRelayRetryAfter/time.Second))) * time.Second
		}
	}
	switch m[1] {
	case "429":
		return &RateLimitedError{RetryAfter: retryAfter}
	case "503":
		return &RelayBusyError{RetryAfter: retryAfter}
	}
	return nil
}
