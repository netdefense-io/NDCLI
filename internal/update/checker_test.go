package update

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessHeaders(t *testing.T) {
	// Use a temp directory for state
	tmpDir := t.TempDir()
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDGConfig)

	// Create ndcli config dir
	os.MkdirAll(filepath.Join(tmpDir, "ndcli"), 0700)

	// Reset global state
	ResetState()
	ResetChecker()

	// Force checker to be enabled for test
	checker := &Checker{
		currentVersion: "1.0.0",
		state:          GetState(),
		disabled:       false,
	}

	headers := http.Header{}
	headers.Set(HeaderMinVersion, "0.9.0")
	headers.Set(HeaderLatestVersion, "1.1.0")

	checker.ProcessHeaders(headers)

	// Give background goroutine time to save
	time.Sleep(50 * time.Millisecond)

	state := GetState()
	if state.GetMinVersion() != "0.9.0" {
		t.Errorf("MinVersion = %q, want %q", state.GetMinVersion(), "0.9.0")
	}
	if state.GetLatestVersion() != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q", state.GetLatestVersion(), "1.1.0")
	}
}

func TestGetVersionInfo(t *testing.T) {
	// Use a temp directory for state
	tmpDir := t.TempDir()
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDGConfig)

	os.MkdirAll(filepath.Join(tmpDir, "ndcli"), 0700)
	ResetState()
	ResetChecker()

	tests := []struct {
		name           string
		currentVersion string
		minVersion     string
		latestVersion  string
		wantCompatible bool
		wantUpdate     bool
	}{
		{
			name:           "up to date",
			currentVersion: "1.0.0",
			minVersion:     "0.9.0",
			latestVersion:  "1.0.0",
			wantCompatible: true,
			wantUpdate:     false,
		},
		{
			name:           "update available",
			currentVersion: "1.0.0",
			minVersion:     "0.9.0",
			latestVersion:  "1.1.0",
			wantCompatible: true,
			wantUpdate:     true,
		},
		{
			name:           "incompatible",
			currentVersion: "0.8.0",
			minVersion:     "0.9.0",
			latestVersion:  "1.0.0",
			wantCompatible: false,
			wantUpdate:     true,
		},
		{
			name:           "dev build",
			currentVersion: "dev",
			minVersion:     "99.0.0",
			latestVersion:  "1.0.0",
			wantCompatible: true,
			wantUpdate:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetState()

			state := GetState()
			state.Update(tt.minVersion, tt.latestVersion, "")

			checker := &Checker{
				currentVersion: tt.currentVersion,
				state:          state,
				disabled:       false,
			}

			info := checker.GetVersionInfo()
			if info == nil {
				t.Fatal("GetVersionInfo returned nil")
			}

			if info.IsCompatible != tt.wantCompatible {
				t.Errorf("IsCompatible = %v, want %v", info.IsCompatible, tt.wantCompatible)
			}
			if info.HasUpdate != tt.wantUpdate {
				t.Errorf("HasUpdate = %v, want %v", info.HasUpdate, tt.wantUpdate)
			}
		})
	}
}

func TestShouldNotify(t *testing.T) {
	tmpDir := t.TempDir()
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDGConfig)

	os.MkdirAll(filepath.Join(tmpDir, "ndcli"), 0700)

	tests := []struct {
		name         string
		isCritical   bool
		lastNotified time.Time
		notifiedVer  string
		latestVer    string
		want         bool
	}{
		{
			name:         "critical always notifies",
			isCritical:   true,
			lastNotified: time.Now(),
			want:         true,
		},
		{
			name:         "first notification",
			isCritical:   false,
			lastNotified: time.Time{},
			latestVer:    "1.1.0",
			want:         true,
		},
		{
			name:         "same version recent",
			isCritical:   false,
			lastNotified: time.Now(),
			notifiedVer:  "1.1.0",
			latestVer:    "1.1.0",
			want:         false,
		},
		{
			name:         "same version expired",
			isCritical:   false,
			lastNotified: time.Now().Add(-25 * time.Hour),
			notifiedVer:  "1.1.0",
			latestVer:    "1.1.0",
			want:         true,
		},
		{
			name:         "new version available",
			isCritical:   false,
			lastNotified: time.Now(),
			notifiedVer:  "1.0.0",
			latestVer:    "1.1.0",
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetState()
			state := GetState()
			state.NotifiedAt = tt.lastNotified
			state.NotifiedVersion = tt.notifiedVer
			state.LatestVersion = tt.latestVer

			got := state.ShouldNotify(tt.isCritical)
			if got != tt.want {
				t.Errorf("ShouldNotify(%v) = %v, want %v", tt.isCritical, got, tt.want)
			}
		})
	}
}

func TestDeprecatedButAlreadyOnLatest(t *testing.T) {
	tmpDir := t.TempDir()
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDGConfig)

	os.MkdirAll(filepath.Join(tmpDir, "ndcli"), 0700)
	ResetState()

	state := GetState()
	// Current == latest, but deprecation date is in the past
	state.Update("0.20.0", "0.23.2", "2026-01-31")

	checker := &Checker{
		currentVersion: "0.23.2",
		state:          state,
		disabled:       false,
	}

	info := checker.GetVersionInfo()
	if info == nil {
		t.Fatal("GetVersionInfo returned nil")
	}
	if !info.IsDeprecated {
		t.Error("expected IsDeprecated=true (date is in the past)")
	}
	if info.HasUpdate {
		t.Error("expected HasUpdate=false (current == latest)")
	}

	notification := checker.GetNotification()
	if notification != "" {
		t.Errorf("expected no notification when already on latest, got: %s", notification)
	}
}

func TestFormatMessages(t *testing.T) {
	// Test that format functions don't panic and return non-empty strings
	critical := formatCriticalMessage("0.8.0", "0.9.0")
	if critical == "" {
		t.Error("formatCriticalMessage returned empty string")
	}
	if len(critical) < 50 {
		t.Error("formatCriticalMessage seems too short")
	}

	deprecated := formatDeprecatedMessage("0.8.0", "1.0.0", "2025-01-01")
	if deprecated == "" {
		t.Error("formatDeprecatedMessage returned empty string")
	}

	update := formatUpdateMessage("1.0.0")
	if update == "" {
		t.Error("formatUpdateMessage returned empty string")
	}
}

func TestDisabledChecker(t *testing.T) {
	tmpDir := t.TempDir()
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDGConfig)

	os.MkdirAll(filepath.Join(tmpDir, "ndcli"), 0700)
	ResetState()
	ResetChecker()

	// Create a fresh state for this test
	freshState := &State{}

	checker := &Checker{
		currentVersion: "1.0.0",
		state:          freshState,
		disabled:       true,
	}

	// Should not process headers when disabled
	headers := http.Header{}
	headers.Set(HeaderMinVersion, "99.0.0")
	checker.ProcessHeaders(headers)

	if freshState.GetMinVersion() != "" {
		t.Error("Disabled checker should not process headers")
	}

	// Should return nil version info when disabled
	info := checker.GetVersionInfo()
	if info != nil {
		t.Error("Disabled checker should return nil version info")
	}

	// Should return empty notification when disabled
	notification := checker.GetNotification()
	if notification != "" {
		t.Error("Disabled checker should return empty notification")
	}
}

func TestEnvVarDisablesCheck(t *testing.T) {
	// Set the disable env var
	origVal := os.Getenv(EnvNoUpdateCheck)
	os.Setenv(EnvNoUpdateCheck, "1")
	defer os.Setenv(EnvNoUpdateCheck, origVal)

	if ShouldCheck() {
		t.Error("ShouldCheck() should return false when NDCLI_NO_UPDATE_CHECK is set")
	}
}

func TestCIEnvVarDisablesCheck(t *testing.T) {
	for _, envVar := range ciEnvVars {
		t.Run(envVar, func(t *testing.T) {
			// Clear the no-check env var
			origNoCheck := os.Getenv(EnvNoUpdateCheck)
			os.Unsetenv(EnvNoUpdateCheck)
			defer os.Setenv(EnvNoUpdateCheck, origNoCheck)

			origVal := os.Getenv(envVar)
			os.Setenv(envVar, "true")
			defer func() {
				if origVal == "" {
					os.Unsetenv(envVar)
				} else {
					os.Setenv(envVar, origVal)
				}
			}()

			if ShouldCheck() {
				t.Errorf("ShouldCheck() should return false when %s is set", envVar)
			}
		})
	}
}
