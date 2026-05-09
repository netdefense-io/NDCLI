package service

import "fmt"

// Error codes used to map service errors to caller surfaces (CLI text, MCP
// ToolError code). Add new codes as new failure modes appear.
const (
	CodeNotAuthenticated = "NOT_AUTHENTICATED"
	CodeAuthFailed       = "AUTH_FAILED"
	CodeOrgRequired      = "ORG_REQUIRED"
	CodeInvalidInput     = "INVALID_INPUT"
	CodeAPIError         = "API_ERROR"
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

// wrapAPI wraps a raw transport/parse error in a service Error tagged
// CodeAPIError, preserving the underlying cause via Unwrap.
func wrapAPI(format string, err error) error {
	return &Error{
		Code:    CodeAPIError,
		Message: fmt.Sprintf(format, err),
		Err:     err,
	}
}
