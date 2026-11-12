package providers

import "github.com/netdefense-io/NDCLI/internal/models"

// Provider defines the interface for OAuth2 providers
type Provider interface {
	// Name returns the provider name
	Name() string

	// RequestDeviceAuthorization initiates the device authorization flow
	RequestDeviceAuthorization(scopes string) (*models.DeviceAuthResponse, error)

	// PollForToken makes one poll for the access token during the device
	// flow; the caller paces the polls.
	PollForToken(deviceCode string) (*models.TokenResponse, error)

	// RefreshToken refreshes an access token using a refresh token
	RefreshToken(refreshToken string) (*models.TokenResponse, error)

	// RevokeToken revokes an access or refresh token
	RevokeToken(token, tokenTypeHint string) error

	// GetUserInfo retrieves user information from the provider
	GetUserInfo(accessToken string) (*models.UserInfo, error)

	// Close releases any resources held by the provider
	Close()
}
