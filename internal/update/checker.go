package update

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/version"

	"golang.org/x/term"
)

// Header names for version information from NDManager
const (
	HeaderMinVersion       = "X-NDCLI-Min-Version"
	HeaderLatestVersion    = "X-NDCLI-Latest-Version"
	HeaderDeprecationDate  = "X-NDCLI-Deprecation-Date"
)

// Environment variables
const (
	EnvNoUpdateCheck     = "NDCLI_NO_UPDATE_CHECK"
	EnvIgnoreMinVersion  = "NDCLI_IGNORE_MIN_VERSION"
)

// CI environment variables to detect
var ciEnvVars = []string{
	"CI",
	"CONTINUOUS_INTEGRATION",
	"BUILD_NUMBER",
	"GITHUB_ACTIONS",
	"GITLAB_CI",
	"CIRCLECI",
	"TRAVIS",
	"JENKINS_URL",
	"TEAMCITY_VERSION",
	"BUILDKITE",
	"DRONE",
	"CODEBUILD_BUILD_ID",
}

// VersionInfo contains the result of a version check
type VersionInfo struct {
	CurrentVersion   string
	MinVersion       string
	LatestVersion    string
	DeprecationDate  string
	IsCompatible     bool
	HasUpdate        bool
	IsDeprecated     bool
}

// Checker handles version checking logic
type Checker struct {
	currentVersion string
	state          *State
	disabled       bool
	ignoreMin      bool
}

// NewChecker creates a new version checker
func NewChecker() *Checker {
	return &Checker{
		currentVersion: config.Version,
		state:          GetState(),
		disabled:       !ShouldCheck(),
		ignoreMin:      os.Getenv(EnvIgnoreMinVersion) != "",
	}
}

// ShouldCheck returns true if version checking should be performed
// Returns false for: CI environments, non-TTY, config disabled, or env var disabled
func ShouldCheck() bool {
	// Explicitly disabled via environment variable
	if os.Getenv(EnvNoUpdateCheck) != "" {
		return false
	}

	// Check config setting (if config is loaded)
	cfg := config.Get()
	if cfg != nil && !cfg.Update.CheckEnabled {
		return false
	}

	// Skip in CI environments
	for _, envVar := range ciEnvVars {
		if os.Getenv(envVar) != "" {
			return false
		}
	}

	// Skip if stdout is not a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	// Skip if stderr is not a terminal
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}

	return true
}

// ProcessHeaders extracts version information from API response headers
// and updates the cached state
func (c *Checker) ProcessHeaders(headers http.Header) {
	if c.disabled {
		return
	}

	minVersion := headers.Get(HeaderMinVersion)
	latestVersion := headers.Get(HeaderLatestVersion)
	deprecationDate := headers.Get(HeaderDeprecationDate)

	// Only update state if we received version headers
	if minVersion != "" || latestVersion != "" {
		c.state.Update(minVersion, latestVersion, deprecationDate)
		// Save in the background to avoid adding latency
		go c.state.Save()
	}
}

// GetVersionInfo returns the current version status
func (c *Checker) GetVersionInfo() *VersionInfo {
	if c.disabled || !c.state.HasVersionInfo() {
		return nil
	}

	info := &VersionInfo{
		CurrentVersion:  c.currentVersion,
		MinVersion:      c.state.GetMinVersion(),
		LatestVersion:   c.state.GetLatestVersion(),
		DeprecationDate: c.state.GetDeprecationDate(),
	}

	// Check compatibility
	if info.MinVersion != "" {
		info.IsCompatible = version.IsCompatible(c.currentVersion, info.MinVersion)
	} else {
		info.IsCompatible = true
	}

	// Check if update available
	if info.LatestVersion != "" {
		info.HasUpdate = version.IsNewer(c.currentVersion, info.LatestVersion)
	}

	// Check deprecation
	if info.DeprecationDate != "" {
		if t, err := time.Parse("2006-01-02", info.DeprecationDate); err == nil {
			info.IsDeprecated = time.Now().After(t)
		}
	}

	return info
}

// GetNotification returns a user-facing message if notification should be shown
// Returns empty string if no notification needed
func (c *Checker) GetNotification() string {
	if c.disabled {
		return ""
	}

	info := c.GetVersionInfo()
	if info == nil {
		return ""
	}

	// Critical: version is incompatible
	if !info.IsCompatible && !c.ignoreMin {
		if c.state.ShouldNotify(true) {
			c.state.MarkNotified(info.MinVersion)
			c.state.Save() // Synchronous to ensure state persists before CLI exits
			return formatCriticalMessage(c.currentVersion, info.MinVersion)
		}
	}

	// Warning: version deprecated but still works (only if there's actually an update to install)
	if info.IsDeprecated && info.IsCompatible && info.HasUpdate {
		if c.state.ShouldNotify(false) {
			c.state.MarkNotified(info.LatestVersion)
			c.state.Save()
			return formatDeprecatedMessage(c.currentVersion, info.LatestVersion, info.DeprecationDate)
		}
	}

	// Info: update available
	if info.HasUpdate && info.IsCompatible {
		if c.state.ShouldNotify(false) {
			c.state.MarkNotified(info.LatestVersion)
			c.state.Save()
			return formatUpdateMessage(info.LatestVersion)
		}
	}

	return ""
}

// formatCriticalMessage creates the critical/incompatible version message
func formatCriticalMessage(current, minimum string) string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("╭──────────────────────────────────────────────────────────────╮\n")
	sb.WriteString(fmt.Sprintf("│  ! NDCLI version %s is no longer compatible                 │\n", padRight(current, 10)))
	sb.WriteString(fmt.Sprintf("│    Minimum required: %-39s │\n", minimum))
	sb.WriteString("│                                                              │\n")
	sb.WriteString("│    Update: brew update && brew upgrade ndcli                  │\n")
	sb.WriteString("│        or: go install ndcli@latest                           │\n")
	sb.WriteString("╰──────────────────────────────────────────────────────────────╯\n")
	return sb.String()
}

// formatDeprecatedMessage creates the deprecation warning message
func formatDeprecatedMessage(current, latest, date string) string {
	return fmt.Sprintf("\n! NDCLI %s was deprecated on %s\n  Update to %s: brew update && brew upgrade ndcli\n",
		current, date, latest)
}

// formatUpdateMessage creates the update available info message
func formatUpdateMessage(latest string) string {
	return fmt.Sprintf("\nℹ A new version of NDCLI is available (%s)\n  Update: brew update && brew upgrade ndcli\n", latest)
}

// padRight pads a string to the specified length
func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

// Global checker instance
var globalChecker *Checker

// GetChecker returns the global checker instance
func GetChecker() *Checker {
	if globalChecker == nil {
		globalChecker = NewChecker()
	}
	return globalChecker
}

// ResetChecker clears the global checker (useful for testing)
func ResetChecker() {
	globalChecker = nil
}

// ProcessResponseHeaders is a convenience function that processes headers using the global checker
func ProcessResponseHeaders(headers http.Header) {
	GetChecker().ProcessHeaders(headers)
}

// GetUpdateNotification is a convenience function that returns a notification using the global checker
func GetUpdateNotification() string {
	return GetChecker().GetNotification()
}
