//go:build windows

package storage

// setRestrictiveUmask is a no-op on Windows (file permissions work differently).
func setRestrictiveUmask() func() {
	return func() {}
}
