package version

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Compare compares two version strings.
// Returns:
//
//	-1 if v1 < v2
//	 0 if v1 == v2
//	 1 if v1 > v2
//
// Returns 0 if either version is invalid or "dev".
func Compare(v1, v2 string) int {
	ver1, err1 := ParseVersion(v1)
	ver2, err2 := ParseVersion(v2)

	if err1 != nil || err2 != nil {
		return 0
	}

	return ver1.Compare(ver2)
}

// IsCompatible returns true if the current version meets the minimum requirement.
// Always returns true for "dev" builds or invalid versions.
func IsCompatible(current, minimum string) bool {
	// Dev builds are always compatible
	if IsDev(current) {
		return true
	}

	currentVer, err := ParseVersion(current)
	if err != nil {
		return true
	}

	minVer, err := ParseVersion(minimum)
	if err != nil {
		return true
	}

	return currentVer.Compare(minVer) >= 0
}

// IsNewer returns true if latest is newer than current.
// Returns false for dev builds or invalid versions.
func IsNewer(current, latest string) bool {
	if IsDev(current) {
		return false
	}

	return Compare(current, latest) < 0
}

// IsDev returns true if the version string indicates a development build.
func IsDev(version string) bool {
	v := strings.TrimSpace(strings.ToLower(version))
	return v == "dev" || v == "" || v == "unknown"
}

// ParseVersion parses a version string into a semver.Version.
// Handles:
//   - Standard semver (1.2.3, 1.2.3-alpha)
//   - "v" prefix (v1.2.3)
//   - Dev builds (returns error)
func ParseVersion(version string) (*semver.Version, error) {
	// Handle dev builds
	if IsDev(version) {
		return nil, semver.ErrInvalidSemVer
	}

	// Strip "v" prefix if present
	version = strings.TrimPrefix(version, "v")

	return semver.NewVersion(version)
}

// MustParseVersion parses a version string and panics on error.
// Use only for known-valid versions in tests.
func MustParseVersion(version string) *semver.Version {
	v, err := ParseVersion(version)
	if err != nil {
		panic(err)
	}
	return v
}
