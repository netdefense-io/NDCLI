//go:build windows

package pathfinder

import "fmt"

// StartShellSession is not supported on Windows.
func StartShellSession(streamManager *StreamManager) (ShellOutcome, error) {
	return ShellOutcome{}, fmt.Errorf("interactive shell sessions are not supported on Windows")
}
