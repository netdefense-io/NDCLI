package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	OAuth2       OAuth2Config       `mapstructure:"oauth2"`
	Controlplane ControlplaneConfig `mapstructure:"controlplane"`
	Pathfinder   PathfinderConfig   `mapstructure:"pathfinder"`
	Organization OrganizationConfig `mapstructure:"organization"`
	Output       OutputConfig       `mapstructure:"output"`
	Auth         AuthConfig         `mapstructure:"auth"`
	Update       UpdateConfig       `mapstructure:"update"`
	Debug        DebugConfig        `mapstructure:"debug"`
}

// DebugConfig holds debug/logging settings
type DebugConfig struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
	LogFile string `mapstructure:"log_file" yaml:"log_file"`
}

// UpdateConfig holds update checking settings
type UpdateConfig struct {
	CheckEnabled bool `mapstructure:"check_enabled" yaml:"check_enabled"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	Path    string `mapstructure:"path"`    // Custom path for file-based storage
	Storage string `mapstructure:"storage"` // Storage backend: "keyring" or "file"
	Account string `mapstructure:"account"` // Account email for keyring lookup
}

// OutputConfig holds output formatting preferences
type OutputConfig struct {
	Format   string `mapstructure:"format"`
	Timezone string `mapstructure:"timezone"`
}

// OAuth2Config holds OAuth2 provider settings
type OAuth2Config struct {
	Provider string `mapstructure:"provider"`
	Domain   string `mapstructure:"domain"`
	ClientID string `mapstructure:"client_id"`
	Audience string `mapstructure:"audience"`
	Scopes   string `mapstructure:"scopes"`
}

// ControlplaneConfig holds API connection settings
type ControlplaneConfig struct {
	Host      string `mapstructure:"host"`
	SSLVerify bool   `mapstructure:"ssl_verify"`
}

// PathfinderConfig holds Pathfinder connection settings
type PathfinderConfig struct {
	Host      string `mapstructure:"host"`
	SSLVerify bool   `mapstructure:"ssl_verify"`
}

// OrganizationConfig holds default organization settings
type OrganizationConfig struct {
	Name string `mapstructure:"name"`
}

var (
	cfg            *Config
	configFilePath string
	authFilePath   string
)

// Load loads configuration from file and environment variables
func Load(customPath string) error {
	// Determine config file path
	if customPath != "" {
		configFilePath = customPath
	} else {
		configFilePath = filepath.Join(xdg.ConfigHome, "ndcli", "config.yaml")
	}

	// Set defaults
	setDefaults()

	// Configure viper
	viper.SetConfigFile(configFilePath)
	viper.SetConfigType("yaml")

	// Environment variable support (NDCLI_ prefix)
	viper.SetEnvPrefix("NDCLI")
	viper.AutomaticEnv()

	// Read config file if it exists, otherwise create with defaults
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file doesn't exist, create it with defaults
			if err := createDefaultConfigFile(); err != nil {
				return fmt.Errorf("error creating default config file: %w", err)
			}
		} else if os.IsNotExist(err) {
			// Config file doesn't exist, create it with defaults
			if err := createDefaultConfigFile(); err != nil {
				return fmt.Errorf("error creating default config file: %w", err)
			}
		} else {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal into config struct
	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("error parsing config: %w", err)
	}

	// Determine auth file path (after config is loaded)
	// Priority: config setting > same directory as config.yaml
	if authPath := viper.GetString("auth.path"); authPath != "" {
		authFilePath = authPath
	} else {
		// Default: same directory as config.yaml
		authFilePath = filepath.Join(filepath.Dir(configFilePath), "auth.json")
	}

	return nil
}

// setDefaults configures default values
func setDefaults() {
	viper.SetDefault("oauth2.provider", DefaultOAuth2Provider)
	viper.SetDefault("oauth2.domain", DefaultOAuth2Domain)
	viper.SetDefault("oauth2.client_id", DefaultOAuth2ClientID)
	viper.SetDefault("oauth2.audience", DefaultOAuth2Audience)
	viper.SetDefault("oauth2.scopes", DefaultOAuth2Scopes)
	viper.SetDefault("controlplane.host", DefaultAPIHost)
	viper.SetDefault("controlplane.ssl_verify", DefaultSSLVerify)
	viper.SetDefault("pathfinder.host", DefaultPathfinderHost)
	viper.SetDefault("pathfinder.ssl_verify", DefaultPathfinderSSLVerify)
	viper.SetDefault("organization.name", "")
	viper.SetDefault("output.format", DefaultOutputFormat)
	viper.SetDefault("output.timezone", DefaultTimezone)
	viper.SetDefault("update.check_enabled", DefaultUpdateCheckEnabled)
	viper.SetDefault("debug.enabled", DefaultDebugEnabled)
	viper.SetDefault("debug.log_file", DefaultDebugLogFile)

	// Explicit env var bindings for nested config keys
	// Note: OAuth2 settings are fetched from NDManager at login time

	// Controlplane settings
	viper.BindEnv("controlplane.host", "NDCLI_CONTROLPLANE_HOST")
	viper.BindEnv("controlplane.ssl_verify", "NDCLI_CONTROLPLANE_SSL_VERIFY")

	// Pathfinder settings
	viper.BindEnv("pathfinder.host", "NDCLI_PATHFINDER_HOST")
	viper.BindEnv("pathfinder.ssl_verify", "NDCLI_PATHFINDER_SSL_VERIFY")

	// Organization settings
	viper.BindEnv("organization.name", "NDCLI_ORGANIZATION_NAME")

	// Output settings
	viper.BindEnv("output.format", "NDCLI_OUTPUT_FORMAT")
	viper.BindEnv("output.timezone", "NDCLI_OUTPUT_TIMEZONE")

	// Debug settings
	viper.BindEnv("debug.enabled", "NDCLI_DEBUG_ENABLED")
	viper.BindEnv("debug.log_file", "NDCLI_DEBUG_LOG_FILE")
}

// Get returns the current configuration
func Get() *Config {
	if cfg == nil {
		cfg = &Config{
			OAuth2: OAuth2Config{
				Provider: DefaultOAuth2Provider,
				Domain:   DefaultOAuth2Domain,
				ClientID: DefaultOAuth2ClientID,
				Audience: DefaultOAuth2Audience,
				Scopes:   DefaultOAuth2Scopes,
			},
			Controlplane: ControlplaneConfig{
				Host:      DefaultAPIHost,
				SSLVerify: DefaultSSLVerify,
			},
			Pathfinder: PathfinderConfig{
				Host:      DefaultPathfinderHost,
				SSLVerify: DefaultPathfinderSSLVerify,
			},
		}
	}
	return cfg
}

// GetConfigFilePath returns the path to the config file
func GetConfigFilePath() string {
	return configFilePath
}

// GetAuthFilePath returns the path to the auth token file
func GetAuthFilePath() string {
	return authFilePath
}

// Save writes configuration to file, preserving any manual settings
func Save() error {
	if configFilePath == "" {
		return fmt.Errorf("config file path not set")
	}

	// Ensure directory exists
	dir := filepath.Dir(configFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Read existing config file to preserve manual settings
	existingConfig := map[string]interface{}{}
	if data, err := os.ReadFile(configFilePath); err == nil {
		if err := yaml.Unmarshal(data, &existingConfig); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	// Update organization settings
	if orgName := viper.GetString("organization.name"); orgName != "" {
		if existingConfig["organization"] == nil {
			existingConfig["organization"] = map[string]interface{}{}
		}
		existingConfig["organization"].(map[string]interface{})["name"] = orgName
	}

	// Update output settings
	if format := viper.GetString("output.format"); format != "" {
		if existingConfig["output"] == nil {
			existingConfig["output"] = map[string]interface{}{}
		}
		existingConfig["output"].(map[string]interface{})["format"] = format
	}
	if timezone := viper.GetString("output.timezone"); timezone != "" {
		if existingConfig["output"] == nil {
			existingConfig["output"] = map[string]interface{}{}
		}
		existingConfig["output"].(map[string]interface{})["timezone"] = timezone
	}

	// Update auth settings (only if there are values to save)
	authStorage := viper.GetString("auth.storage")
	authPath := viper.GetString("auth.path")
	authAccount := viper.GetString("auth.account")

	if authStorage != "" || authPath != "" || authAccount != "" {
		if existingConfig["auth"] == nil {
			existingConfig["auth"] = map[string]interface{}{}
		}
		authMap := existingConfig["auth"].(map[string]interface{})

		if authStorage != "" {
			authMap["storage"] = authStorage
		}
		if authPath != "" {
			authMap["path"] = authPath
		}
		if authAccount != "" {
			authMap["account"] = authAccount
		}
	} else if viper.IsSet("auth.account") && authAccount == "" {
		// Account was explicitly cleared, remove the auth section if empty
		if existingConfig["auth"] != nil {
			authMap := existingConfig["auth"].(map[string]interface{})
			delete(authMap, "account")
			if len(authMap) == 0 {
				delete(existingConfig, "auth")
			}
		}
	}

	// Write merged config
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(2)
	if err := encoder.Encode(existingConfig); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// UpdateValue updates a specific configuration value
func UpdateValue(key string, value interface{}) error {
	viper.Set(key, value)
	if err := Save(); err != nil {
		return err
	}
	// Refresh in-memory config to reflect the change
	return viper.Unmarshal(cfg)
}

// createDefaultConfigFile creates a new config file with all default values
func createDefaultConfigFile() error {
	// Ensure directory exists
	dir := filepath.Dir(configFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Build default config structure
	// Note: OAuth2 settings (domain, client_id) are fetched from NDManager at login time
	defaultConfig := map[string]interface{}{
		"controlplane": map[string]interface{}{
			"host":       DefaultAPIHost,
			"ssl_verify": DefaultSSLVerify,
		},
		"pathfinder": map[string]interface{}{
			"host":       DefaultPathfinderHost,
			"ssl_verify": DefaultPathfinderSSLVerify,
		},
		"output": map[string]interface{}{
			"format":   DefaultOutputFormat,
			"timezone": DefaultTimezone,
		},
	}

	// Write config file
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(2)
	if err := encoder.Encode(defaultConfig); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// CreateDefaultConfig creates a default configuration file
func CreateDefaultConfig() error {
	// Ensure configFilePath is set
	if configFilePath == "" {
		configFilePath = filepath.Join(xdg.ConfigHome, "ndcli", "config.yaml")
	}
	setDefaults()
	return Save()
}

// ConfigExists checks if the config file exists
func ConfigExists() bool {
	path := filepath.Join(xdg.ConfigHome, "ndcli", "config.yaml")
	_, err := os.Stat(path)
	return err == nil
}

// GetDefaultConfigPath returns the default config file path without loading
func GetDefaultConfigPath() string {
	return filepath.Join(xdg.ConfigHome, "ndcli", "config.yaml")
}

// EnsureConfigDir creates the config directory if it doesn't exist
func EnsureConfigDir() (string, error) {
	dir := filepath.Join(xdg.ConfigHome, "ndcli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return dir, nil
}
