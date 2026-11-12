package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// A CONNECT_AGENT_TOO_OLD refusal gets the front-end's hint and keeps the
// control plane's code; any other error is left as it is.
func TestConnectAdvice(t *testing.T) {
	refusal := wrapAPI("%v", &api.APIError{StatusCode: 409, Code: APICodeConnectAgentTooOld,
		Message: "This device's agent cannot open a read-only session."})
	got := ConnectAdvice(refusal, "ask a read-write member")
	if api.GetErrorCode(got) != APICodeConnectAgentTooOld || !strings.HasSuffix(got.Error(), "\n  Hint: ask a read-write member") ||
		!strings.Contains(got.Error(), "cannot open a read-only session") {
		t.Errorf("advice = %q (code %q)", got, api.GetErrorCode(got))
	}
	if again := ConnectAdvice(got, "ask a read-write member"); again.Error() != got.Error() {
		t.Errorf("advice given twice: %q", again)
	}
	other := wrapAPI("%v", &api.APIError{StatusCode: 409, Code: "DEVICE_NOT_BOUND", Message: "no key"})
	if ConnectAdvice(other, "x") != other {
		t.Error("another refusal was changed")
	}
	if ConnectAdvice(nil, "x") != nil {
		t.Error("nil became an error")
	}
	if plain := errors.New("boom"); ConnectAdvice(plain, "x") != plain {
		t.Error("a plain error was changed")
	}
}
