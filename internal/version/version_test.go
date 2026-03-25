package version

import (
	"testing"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{"equal versions", "1.0.0", "1.0.0", 0},
		{"v1 less than v2", "1.0.0", "2.0.0", -1},
		{"v1 greater than v2", "2.0.0", "1.0.0", 1},
		{"with v prefix", "v1.0.0", "1.0.0", 0},
		{"patch difference", "1.0.1", "1.0.0", 1},
		{"minor difference", "1.1.0", "1.0.0", 1},
		{"prerelease", "1.0.0-alpha", "1.0.0", -1},
		{"dev is neutral", "dev", "1.0.0", 0},
		{"both dev", "dev", "dev", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Compare(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestIsCompatible(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		minimum  string
		expected bool
	}{
		{"exact match", "1.0.0", "1.0.0", true},
		{"above minimum", "1.1.0", "1.0.0", true},
		{"below minimum", "0.9.0", "1.0.0", false},
		{"dev always compatible", "dev", "99.0.0", true},
		{"with v prefix", "v1.1.0", "1.0.0", true},
		{"empty version compatible", "", "1.0.0", true},
		{"invalid minimum treated as compatible", "1.0.0", "not-a-version", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCompatible(tt.current, tt.minimum)
			if result != tt.expected {
				t.Errorf("IsCompatible(%q, %q) = %v, want %v", tt.current, tt.minimum, result, tt.expected)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected bool
	}{
		{"newer available", "1.0.0", "1.1.0", true},
		{"same version", "1.0.0", "1.0.0", false},
		{"already ahead", "1.1.0", "1.0.0", false},
		{"dev never needs update", "dev", "99.0.0", false},
		{"major version update", "1.0.0", "2.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNewer(tt.current, tt.latest)
			if result != tt.expected {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.latest, result, tt.expected)
			}
		})
	}
}

func TestIsDev(t *testing.T) {
	tests := []struct {
		version  string
		expected bool
	}{
		{"dev", true},
		{"DEV", true},
		{"Dev", true},
		{"", true},
		{"unknown", true},
		{"1.0.0", false},
		{"v1.0.0", false},
		{"1.0.0-dev", false}, // This is a prerelease, not a dev build
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := IsDev(tt.version)
			if result != tt.expected {
				t.Errorf("IsDev(%q) = %v, want %v", tt.version, result, tt.expected)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		version   string
		expectErr bool
	}{
		{"1.0.0", false},
		{"v1.0.0", false},
		{"1.2.3", false},
		{"1.0.0-alpha", false},
		{"1.0.0-beta.1", false},
		{"dev", true},
		{"", true},
		{"not-a-version", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			_, err := ParseVersion(tt.version)
			if (err != nil) != tt.expectErr {
				t.Errorf("ParseVersion(%q) error = %v, expectErr = %v", tt.version, err, tt.expectErr)
			}
		})
	}
}

func TestMustParseVersion(t *testing.T) {
	// Should not panic for valid version
	v := MustParseVersion("1.0.0")
	if v == nil {
		t.Error("MustParseVersion returned nil for valid version")
	}

	// Should panic for invalid version
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseVersion did not panic for invalid version")
		}
	}()
	MustParseVersion("dev")
}
