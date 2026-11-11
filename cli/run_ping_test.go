package cli

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const runPingResponse = `{"type":"PING","organization":"acme","total":1,"tasks":[{"task":"T1","device_name":"fw1","status":"PENDING"}]}`

// NDAgent clamps a ping count to 1..100, so the command refuses anything
// outside it before a request is sent.
func TestRunPingRefusesACountOutOfRange(t *testing.T) {
	for _, count := range []string{"0", "-1", "101", "1000"} {
		t.Run(count, func(t *testing.T) {
			requests, stdout, stderr, code := runRecorded(t, http.StatusCreated, runPingResponse,
				"run", "ping", "--device", "fw1", "--host", "1.1.1.1", "--count="+count)

			if code != 1 {
				t.Fatalf("exit %d, want 1\nstderr: %s", code, stderr)
			}
			if want := "Error: --count must be between 1 and 100\n"; !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
			if stdout != "" || len(requests) != 0 {
				t.Errorf("stdout %q, %d request(s); want neither", stdout, len(requests))
			}
		})
	}
}

// The bounds themselves are accepted and sent; the default of 4 is left to
// the agent.
func TestRunPingSendsACountInRange(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want map[string]any
	}{
		{nil, map[string]any{"target": "1.1.1.1"}},
		{[]string{"--count", "1"}, map[string]any{"target": "1.1.1.1", "count": float64(1)}},
		{[]string{"--count", "100"}, map[string]any{"target": "1.1.1.1", "count": float64(100)}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append([]string{"-f", "json", "run", "ping", "--device", "fw1", "--host", "1.1.1.1"}, tc.args...)
			requests, _, stderr, code := runRecorded(t, http.StatusCreated, runPingResponse, args...)

			if code != 0 {
				t.Fatalf("exit %d\nstderr: %s", code, stderr)
			}
			if len(requests) != 1 || !reflect.DeepEqual(requests[0].body["payload"], tc.want) {
				t.Errorf("requests = %+v, want one with payload %v", requests, tc.want)
			}
		})
	}
}

func TestRunPingHelpStatesTheCountRange(t *testing.T) {
	usage := findCommand(t, "run", "ping").Flags().FlagUsages()
	if !strings.Contains(usage, "Number of ping packets, 1 to 100 (default 4)") {
		t.Errorf("--count help does not state the range:\n%s", usage)
	}
}
