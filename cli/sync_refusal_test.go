package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// syncRefusedServer answers the sync review with two devices and the apply
// with the given status and body.
func syncRefusedServer(t *testing.T, status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/status":
			_, _ = io.WriteString(w, `{"items":[{"device_name":"fw01","organization":"acme"},{"device_name":"fw02","organization":"acme"}],"total":2}`)
		case "/api/v1/sync":
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// Every device refused: the result is shown under a ✗ headline, never a ✓,
// and the command fails.
func TestSyncApplyRefusedForEveryDeviceFails(t *testing.T) {
	cmd := findCommand(t, "sync", "apply")
	body := `{"message":"Sync triggered for 0 device(s); 2 error(s)","devices_affected":0,"skipped":0,"tasks":[],` +
		`"errors":[{"device_name":"fw01","error":"device offline"},{"device_name":"fw02","error":"device offline"}]}`

	var err error
	stdout, _ := runStreams(t, syncRefusedServer(t, http.StatusBadRequest, body), "table", "y\n", func() error {
		err = runSyncApply(cmd, nil)
		return nil
	})
	if err == nil || err.Error() != "sync failed for all devices" {
		t.Errorf("err = %v, want \"sync failed for all devices\"", err)
	}
	if !strings.HasPrefix(stdout, "✗ Sync triggered for 0 device(s); 2 error(s)\n") {
		t.Errorf("stdout does not open with the ✗ headline:\n%s", stdout)
	}
	if strings.Contains(stdout, "✓") {
		t.Errorf("stdout shows a success mark:\n%s", stdout)
	}
	for _, want := range []string{"fw01: device offline", "fw02: device offline"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// A refusal of the whole request prints nothing on stdout and fails with the
// server's code; it used to print an empty ✓ headline and exit 0.
func TestSyncApplyRefusedRequestIsAnError(t *testing.T) {
	cmd := findCommand(t, "sync", "apply")
	const message = "This personal access token is read-only"

	var err error
	stdout, _ := runStreams(t, syncRefusedServer(t, http.StatusForbidden, `{"error":"`+message+`","code":"TOKEN_READ_ONLY"}`), "table", "y\n", func() error {
		err = runSyncApply(cmd, nil)
		return nil
	})
	assertRefusal(t, err, stdout, "TOKEN_READ_ONLY", message)
}

// A 400 that names no refused device is a refusal of the request, not an empty
// sync result: nothing on stdout, the server's code and message, a failure.
func TestSyncApplyPlainBadRequestIsAnError(t *testing.T) {
	cmd := findCommand(t, "sync", "apply")
	const message = "Invalid device pattern"

	var err error
	stdout, _ := runStreams(t, syncRefusedServer(t, http.StatusBadRequest, `{"error":"`+message+`","code":"INVALID_PATTERN"}`), "table", "y\n", func() error {
		err = runSyncApply(cmd, nil)
		return nil
	})
	assertRefusal(t, err, stdout, "INVALID_PATTERN", message)
}
