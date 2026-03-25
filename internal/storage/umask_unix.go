//go:build !windows

package storage

import "syscall"

// setRestrictiveUmask sets umask to 0077 and returns a function to restore the original.
func setRestrictiveUmask() func() {
	old := syscall.Umask(0077)
	return func() { syscall.Umask(old) }
}
