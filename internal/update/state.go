package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/adrg/xdg"
)

const (
	// StateFileName is the name of the file storing update check state
	StateFileName = "update-state.json"

	// NotificationInterval is the minimum time between showing update notifications
	NotificationInterval = 24 * time.Hour
)

// State holds the cached version check information
type State struct {
	// LastCheck is when we last checked for updates (from headers)
	LastCheck time.Time `json:"last_check"`

	// LatestVersion is the most recent version available
	LatestVersion string `json:"latest_version,omitempty"`

	// MinVersion is the minimum compatible version required by the server
	MinVersion string `json:"min_version,omitempty"`

	// DeprecationDate is when the current version becomes unsupported
	DeprecationDate string `json:"deprecation_date,omitempty"`

	// NotifiedAt is when we last showed an update notification to the user
	NotifiedAt time.Time `json:"notified_at"`

	// NotifiedVersion is the version we last notified the user about
	NotifiedVersion string `json:"notified_version,omitempty"`

	mu sync.Mutex `json:"-"`
}

var (
	globalState *State
	stateMu     sync.Mutex
)

// GetState returns the global update state, loading it from disk if needed
func GetState() *State {
	stateMu.Lock()
	defer stateMu.Unlock()

	if globalState == nil {
		globalState = &State{}
		globalState.Load()
	}
	return globalState
}

// ResetState clears the global state (useful for testing)
func ResetState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	globalState = nil
}

// GetStatePath returns the path to the state file
func GetStatePath() string {
	return filepath.Join(xdg.ConfigHome, "ndcli", StateFileName)
}

// Load reads the state from disk
func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(GetStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet, use defaults
		}
		return err
	}

	return json.Unmarshal(data, s)
}

// Save writes the state to disk
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(GetStatePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(GetStatePath(), data, 0600)
}

// Update updates the state with new version information from headers
func (s *State) Update(minVersion, latestVersion, deprecationDate string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastCheck = time.Now()

	if minVersion != "" {
		s.MinVersion = minVersion
	}
	if latestVersion != "" {
		s.LatestVersion = latestVersion
	}
	if deprecationDate != "" {
		s.DeprecationDate = deprecationDate
	}
}

// ShouldNotify returns true if enough time has passed since the last notification
// for the same version. Critical version incompatibility always returns true.
func (s *State) ShouldNotify(isCritical bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Critical (incompatible) notifications are always shown
	if isCritical {
		return true
	}

	// Don't notify if we already notified about this version recently
	if s.NotifiedVersion == s.LatestVersion && time.Since(s.NotifiedAt) < NotificationInterval {
		return false
	}

	return true
}

// MarkNotified records that we showed a notification
func (s *State) MarkNotified(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.NotifiedAt = time.Now()
	s.NotifiedVersion = version
}

// GetMinVersion returns the cached minimum version (thread-safe)
func (s *State) GetMinVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MinVersion
}

// GetLatestVersion returns the cached latest version (thread-safe)
func (s *State) GetLatestVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LatestVersion
}

// GetDeprecationDate returns the cached deprecation date (thread-safe)
func (s *State) GetDeprecationDate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DeprecationDate
}

// HasVersionInfo returns true if we have received version info from the server
func (s *State) HasVersionInfo() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MinVersion != "" || s.LatestVersion != ""
}
