package service

import (
	"errors"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// APICodeConnectAgentTooOld is NDManager's refusal of a read-only CONNECT to a
// device whose agent would ignore read_only and open a full session: one
// older than 1.13.6, or one whose version it cannot read. It comes before any
// relay session or task exists, so there is nothing to cancel.
const APICodeConnectAgentTooOld = "CONNECT_AGENT_TOO_OLD"

// ConnectAdvice adds hint, the front-end's own way forward, to a
// CONNECT_AGENT_TOO_OLD refusal; any other error comes back untouched. The
// result wraps err, so the code NDManager sent survives.
func ConnectAdvice(err error, hint string) error {
	if err == nil || api.GetErrorCode(err) != APICodeConnectAgentTooOld || strings.Contains(err.Error(), hint) {
		return err
	}
	code := CodeAPIError
	var svcErr *Error
	if errors.As(err, &svcErr) && svcErr.Code != "" {
		code = svcErr.Code
	}
	return &Error{Code: code, Message: err.Error() + "\n  Hint: " + hint, Err: err}
}
