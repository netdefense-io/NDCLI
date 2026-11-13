package cli

import (
	"errors"
	"fmt"
)

// ExitPartial is the exit status of a bulk command that did part of its work:
// some targets succeeded and some failed. 1 stays the status of a command that
// failed, so a script can tell "nothing happened" from "check the failures".
const ExitPartial = 3

// partialFailure is what a bulk command returns, after printing its result,
// when some of its targets failed and the rest succeeded.
type partialFailure struct {
	failed, total int
	what          string
}

func (e *partialFailure) Error() string {
	return fmt.Sprintf("%d of %d %s failed", e.failed, e.total, e.what)
}

// ExitCode is the process exit status for the error Execute returned: 0 for
// none, ExitPartial for a partial failure, 1 for any other.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var partial *partialFailure
	if errors.As(err, &partial) {
		return ExitPartial
	}
	return 1
}
