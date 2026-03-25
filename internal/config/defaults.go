package config

// Version and build information (set via ldflags)
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Default configuration values
const (
	// OAuth2 defaults
	DefaultOAuth2Provider = "auth0"
	DefaultOAuth2Domain   = "auth.netdefense.io"
	DefaultOAuth2ClientID = "hEt3Ol5Zj9Ca9nbiaUJBBhHNxBAL2jJw"
	DefaultOAuth2Audience = "authcli"
	DefaultOAuth2Scopes   = "openid profile email offline_access"

	// API defaults
	DefaultAPIHost   = "https://control.netdefense.io"
	DefaultSSLVerify = true

	// Pagination defaults
	DefaultPerPage = 30
	MaxPerPage     = 500

	// Output defaults
	DefaultOutputFormat = "detailed"
	DefaultTimezone     = "Local"

	// Update check defaults
	DefaultUpdateCheckEnabled = true

	// Pathfinder defaults
	DefaultPathfinderHost      = "wss://pathfinder.netdefense.io"
	DefaultPathfinderSSLVerify = true

	// Debug defaults
	DefaultDebugEnabled = false
	DefaultDebugLogFile = "" // Empty means use default path in config dir
)

// ValidOutputFormats contains all valid output format options
var ValidOutputFormats = []string{"table", "simple", "detailed", "json"}
