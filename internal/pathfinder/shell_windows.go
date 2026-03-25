//go:build windows

package pathfinder

import "fmt"

// StartShellSession is not supported on Windows
func StartShellSession(streamManager *StreamManager) error {
	return fmt.Errorf("interactive shell sessions are not supported on Windows")
}
