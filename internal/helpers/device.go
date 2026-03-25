package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// DeviceResolver is an interface for resolving device names to IDs
type DeviceResolver interface {
	Get(ctx context.Context, path string, params map[string]string) (*http.Response, error)
}

// FindDeviceByName searches for a device by name and returns its ID
func FindDeviceByName(ctx context.Context, client DeviceResolver, org, name string) (string, error) {
	// Search with pagination
	params := map[string]string{
		"organization": org,
		"name":         name,
		"per_page":     "500",
	}

	resp, err := client.Get(ctx, "/api/v1/devices", params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to search for device: status %d", resp.StatusCode)
	}

	var result models.DeviceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse device list: %w", err)
	}

	devices := result.GetItems()

	// Look for exact match first
	for _, d := range devices {
		if d.Name == name {
			return d.Name, nil
		}
	}

	// If no exact match, check if there's only one partial match
	var matches []models.Device
	for _, d := range devices {
		if contains(d.Name, name) {
			matches = append(matches, d)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("device '%s' not found", name)
	}

	if len(matches) > 1 {
		return "", fmt.Errorf("multiple devices match '%s', please be more specific", name)
	}

	return matches[0].Name, nil
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0)
}

// IsUUID checks if a string looks like a UUID
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	// Basic check for UUID format: 8-4-4-4-12
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	return true
}

// ResolveDeviceIdentifier resolves a device name or ID to an ID
func ResolveDeviceIdentifier(ctx context.Context, client DeviceResolver, org, identifier string) (string, error) {
	if IsUUID(identifier) {
		return identifier, nil
	}
	return FindDeviceByName(ctx, client, org, identifier)
}
