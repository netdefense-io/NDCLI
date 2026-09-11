package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

	// explicitlySet records the keys this process assigned through
	// UpdateValue. viper.IsSet cannot stand in for it: for a key that has a
	// default, IsSet is true whether or not anyone ever set the key, and Save
	// needs to tell "cleared this session" apart from "never configured".
	explicitMu    sync.Mutex
	explicitlySet = map[string]bool{}
)

// markExplicitlySet records that key was assigned by this process.
func markExplicitlySet(key string) {
	explicitMu.Lock()
	defer explicitMu.Unlock()
	explicitlySet[key] = true
}

// wasExplicitlySet reports whether this process assigned key, including
// assigning it the empty string.
func wasExplicitlySet(key string) bool {
	explicitMu.Lock()
	defer explicitMu.Unlock()
	return explicitlySet[key]
}

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

	// auth.* has no compiled default, but viper.Unmarshal only picks up an
	// environment value for a key it already knows about — without these the
	// NDCLI_AUTH_* bindings would exist and still do nothing (issue #202).
	viper.SetDefault("auth.storage", "")
	viper.SetDefault("auth.path", "")
	viper.SetDefault("auth.account", "")

	// Environment bindings for nested config keys. envBindings (env.go) is the
	// single source of truth for which keys have one, and drives both this and
	// the warning for variables that have none. oauth2.* is deliberately not
	// bound — see the comment there.
	bindEnvVars()
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

	// Update auth settings.
	//
	// Only fields this process actually assigned through UpdateValue are
	// written. Reading viper.GetString here instead would pick up an
	// environment-resolved value and bake it into the file, turning a session
	// override into a permanent setting: `NDCLI_AUTH_STORAGE=file ndcli config
	// set output.timezone ...` would pin credentials to plaintext on disk for
	// good, long after the variable is gone, from a command that has nothing
	// to do with authentication. Whatever is already in the file is left
	// exactly as it is — this never rewrites a value it did not set.
	//
	// Assigning the empty string is how a field is cleared (logout does it for
	// auth.account through KeyringStorage.Clear), so that removes the key.
	// viper.IsSet cannot stand in for wasExplicitlySet: these keys carry
	// defaults now, so IsSet is true whether or not anyone ever set them.
	authWrites := map[string]string{}
	for _, key := range []string{"auth.storage", "auth.path", "auth.account"} {
		if !wasExplicitlySet(key) {
			continue
		}
		authWrites[strings.TrimPrefix(key, "auth.")] = viper.GetString(key)
	}

	if len(authWrites) > 0 {
		authMap, _ := existingConfig["auth"].(map[string]interface{})
		if authMap == nil {
			authMap = map[string]interface{}{}
		}
		for field, value := range authWrites {
			if value == "" {
				delete(authMap, field)
				continue
			}
			authMap[field] = value
		}
		if len(authMap) == 0 {
			delete(existingConfig, "auth")
		} else {
			existingConfig["auth"] = authMap
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
	markExplicitlySet(key)
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

// CreateDefaultConfig resets the configuration file to defaults, overwriting any
// existing values. Save() deliberately preserves manual settings, so reset must
// bypass it and write a fresh defaults file.
func CreateDefaultConfig() error {
	if configFilePath == "" {
		configFilePath = filepath.Join(xdg.ConfigHome, "ndcli", "config.yaml")
	}
	if err := createDefaultConfigFile(); err != nil {
		return err
	}
	// Re-read the fresh file so in-memory viper and cfg reflect defaults.
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("error reading reset config: %w", err)
	}
	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("error parsing reset config: %w", err)
	}
	return nil
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
