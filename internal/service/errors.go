package service

import (
	"errors"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/api"
)

// Error codes used to map service errors to caller surfaces (CLI text, MCP
// ToolError code). Add new codes as new failure modes appear.
const (
	CodeNotAuthenticated = "NOT_AUTHENTICATED"
	CodeAuthFailed       = "AUTH_FAILED"
	CodeOrgRequired      = "ORG_REQUIRED"
	CodeInvalidInput     = "INVALID_INPUT"
	// CodeDestinationExists is returned when a download would replace an
	// existing local file. It has its own code so each front-end can name
	// its own way of overriding — a --overwrite flag in the CLI, an
	// overwrite parameter over MCP — while the shared message stays
	// surface-neutral.
	CodeDestinationExists = "DESTINATION_EXISTS"
	CodeAPIError          = "API_ERROR"
	// CodeServerTooOld is returned when the control plane answers a write in a
	// way that shows it predates the feature the request needs: it ignored a
	// field it does not know, and either refused the request as empty or did
	// not echo the field in its answer. The write was not made (or, when it
	// carried more than that field, only the rest was).
	CodeServerTooOld = "SERVER_TOO_OLD"
)

// Error is the typed error returned by service methods. Callers can type-
// assert to read Code; falling back to err.Error() for the message is also
// fine.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil && e.Message == "" {
		return e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// ErrorCode is the code a front-end shows with err: the one NDManager answered
// with, which is what a user can look up. The exception is a version skew. A
// control plane that predates a feature refuses the request for it with an
// ordinary code (a 400 MISSING_FIELD for an update whose only field it ignored),
// and the service says why with CodeServerTooOld while keeping the refusal
// underneath; showing MISSING_FIELD would send the user looking for a field
// they did give.
func ErrorCode(err error) string {
	var svcErr *Error
	if errors.As(err, &svcErr) && svcErr.Code == CodeServerTooOld {
		return CodeServerTooOld
	}
	return api.GetErrorCode(err)
}

// wrapAPI wraps a raw transport/parse error in a service Error tagged
// CodeAPIError, preserving the underlying cause via Unwrap.
func wrapAPI(format string, err error) error {
	return &Error{
		Code:    CodeAPIError,
		Message: fmt.Sprintf(format, err),
		Err:     err,
	}
}
