package config

import (
	"net/url"
	"strings"
)

// NormalizeHost normalizes a controlplane host URL to a consistent key format.
// It strips protocol, trailing slashes, default ports, and lowercases the result.
func NormalizeHost(host string) string {
	if host == "" {
		return ""
	}

	// Parse as URL if it has a scheme
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err == nil {
			host = u.Host
			// Remove default ports
			if strings.HasSuffix(host, ":443") && u.Scheme == "https" {
				host = strings.TrimSuffix(host, ":443")
			} else if strings.HasSuffix(host, ":80") && u.Scheme == "http" {
				host = strings.TrimSuffix(host, ":80")
			}
		}
	}

	// Lowercase and trim trailing slashes
	host = strings.ToLower(strings.TrimSuffix(host, "/"))

	return host
}

// BuildCredentialKey creates a composite key from email and host.
// Format: {email}@{normalized_host}
func BuildCredentialKey(email, host string) string {
	if email == "" {
		return ""
	}
	normalizedHost := NormalizeHost(host)
	if normalizedHost == "" {
		return email
	}
	return email + "@" + normalizedHost
}

// ParseCredentialKey extracts email and host from a composite key.
// Returns (email, host). If no host separator found, returns (key, "").
func ParseCredentialKey(key string) (email, host string) {
	if key == "" {
		return "", ""
	}

	// Email format: user@domain.com@host -> split on last @
	// We need to find the @ that separates email from host
	atCount := strings.Count(key, "@")
	if atCount < 2 {
		// No host component, just email
		return key, ""
	}

	lastAt := strings.LastIndex(key, "@")
	return key[:lastAt], key[lastAt+1:]
}

// GetCurrentHost returns the normalized host from the current configuration.
func GetCurrentHost() string {
	return NormalizeHost(Get().Controlplane.Host)
}
