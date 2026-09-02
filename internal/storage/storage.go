package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/netdefense-io/NDCLI/internal/config"
)

const (
	// KeyringService is the service name used in the system keyring
	// Uses reverse domain notation for cross-platform compatibility
	KeyringService = "io.netdefense.ndcli"
)

// Storage defines the interface for token storage backends
type Storage interface {
	// Save stores the token data for the given credential key (email@host)
	Save(data []byte, credentialKey string) error
	// Load retrieves the token data for the current credential key
	Load() ([]byte, error)
	// Clear removes the stored token data for the current credential key
	Clear() error
	// Name returns the storage backend name
	Name() string
	// GetCurrentCredentialKey returns the current credential key (email@host)
	GetCurrentCredentialKey() string
}

// KeyringStorage stores tokens in the system keyring.
//
// Payloads are written through chunkedKeyring, which splits anything larger
// than the platform's per-secret cap across numbered part entries. See
// keyring_chunk.go for the caps and the on-disk layout.
type KeyringStorage struct {
	keyring chunkedKeyring
	// warn receives non-fatal operator-facing messages. A nil warn means
	// os.Stderr; it is a field so tests can capture instead of printing.
	warn io.Writer
}

func (k *KeyringStorage) warnWriter() io.Writer {
	if k.warn != nil {
		return k.warn
	}
	return os.Stderr
}

// NewKeyringStorage creates a new keyring storage backend
func NewKeyringStorage() *KeyringStorage {
	return &KeyringStorage{
		keyring: chunkedKeyring{
			ops:     systemKeyring{},
			service: KeyringService,
			limit:   maxKeyringSecretBytes,
		},
	}
}

// Save stores data in the system keyring for the given credential key (email@host)
func (k *KeyringStorage) Save(data []byte, credentialKey string) error {
	if credentialKey == "" {
		return fmt.Errorf("credential key is required for keyring storage")
	}

	if err := k.keyring.save(credentialKey, data); err != nil {
		return fmt.Errorf("failed to save to keyring: %w", err)
	}

	// Update config file with credential key (for multi-config isolation)
	if err := config.UpdateValue("auth.account", credentialKey); err != nil {
		return fmt.Errorf("failed to update config with account: %w", err)
	}
	return nil
}

// Load retrieves data from the system keyring for the current credential key
func (k *KeyringStorage) Load() ([]byte, error) {
	credentialKey := k.GetCurrentCredentialKey()
	if credentialKey == "" {
		return nil, nil // No account configured
	}
	return k.keyring.load(credentialKey)
}

// Clear removes data from the system keyring for the current credential key
func (k *KeyringStorage) Clear() error {
	credentialKey := k.GetCurrentCredentialKey()
	if credentialKey == "" {
		return nil // Nothing to clear
	}

	// clear removes the primary entry -- the one that actually holds the
	// session -- even when best-effort cleanup of leftover part entries fails,
	// and reports that separately. Reporting it as a failed logout would print
	// "logout failed" for a session that is genuinely gone and leave
	// auth.account pointing at it.
	err := k.keyring.clear(credentialKey)
	var cleanup partCleanupError
	if err != nil && !errors.As(err, &cleanup) {
		// The session secret is still in the keyring. Leave auth.account in
		// place so a retry can still find it.
		return err
	}

	// Clear the account from config
	if cfgErr := config.UpdateValue("auth.account", ""); cfgErr != nil {
		return fmt.Errorf("failed to clear config account: %w", cfgErr)
	}

	if err != nil {
		fmt.Fprintf(k.warnWriter(), "Warning: the stored session was removed, but leftover keyring entries could not be deleted: %v\n", cleanup.err)
		fmt.Fprintf(k.warnWriter(), "  They hold fragments of the old session. Remove entries named %q with a %q suffix from your keyring once it is available.\n", credentialKey, partSuffix)
	}
	return nil
}

// Name returns the storage backend name
func (k *KeyringStorage) Name() string {
	return "keyring"
}

// GetCurrentCredentialKey returns the current credential key (email@host) from config
func (k *KeyringStorage) GetCurrentCredentialKey() string {
	return config.Get().Auth.Account
}

// isCredentialBlobTooBig recognizes a keyring rejecting a secret for being too
// large. zalando/go-keyring pre-checks the size on macOS and Windows and
// returns ErrSetDataTooBig ("data passed to Set was too big"); if that check is
// bypassed the underlying wincred call surfaces "The stub received bad data"
// (Win32 1783) instead.
func isCredentialBlobTooBig(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrSetDataTooBig) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too big") ||
		strings.Contains(msg, "stub received bad data")
}

// FileStorage stores tokens in a host-scoped file
type FileStorage struct {
	basePath string
}

// NewFileStorage creates a new file storage backend
func NewFileStorage(path string) *FileStorage {
	if path == "" {
		path = config.GetAuthFilePath()
	}
	return &FileStorage{basePath: path}
}

// getHostScopedPath returns the auth file path scoped by host
// Example: auth.json -> auth-<host>.json
func (f *FileStorage) getHostScopedPath() string {
	host := config.GetCurrentHost()
	if host == "" {
		return f.basePath
	}

	// Replace special chars in host for valid filename
	safeHost := strings.ReplaceAll(host, ":", "_")
	safeHost = strings.ReplaceAll(safeHost, "/", "_")

	dir := filepath.Dir(f.basePath)
	ext := filepath.Ext(f.basePath)
	base := strings.TrimSuffix(filepath.Base(f.basePath), ext)

	// auth.json -> auth-<host>.json
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, safeHost, ext))
}

// Save stores data in a host-scoped file with secure file handling
func (f *FileStorage) Save(data []byte, credentialKey string) error {
	filePath := f.getHostScopedPath()

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create auth directory: %w", err)
	}

	// Reject symlinks at the target path to prevent symlink attacks
	if info, err := os.Lstat(filePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write auth file: %s is a symlink", filePath)
		}
	}

	// Enforce restrictive umask for file creation (no-op on Windows)
	restoreUmask := setRestrictiveUmask()
	defer restoreUmask()

	// Write file with secure permissions
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}

	// Verify actual file permissions match intended permissions
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to verify auth file permissions: %w", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		// Attempt to fix permissions
		if err := os.Chmod(filePath, 0600); err != nil {
			return fmt.Errorf("auth file has insecure permissions %o and could not be fixed: %w", perm, err)
		}
	}

	return nil
}

// Load retrieves data from the host-scoped file
func (f *FileStorage) Load() ([]byte, error) {
	filePath := f.getHostScopedPath()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read auth file: %w", err)
	}
	return data, nil
}

// Clear removes the host-scoped storage file
func (f *FileStorage) Clear() error {
	filePath := f.getHostScopedPath()
	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Name returns the storage backend name
func (f *FileStorage) Name() string {
	return "file"
}

// FilePath returns the host-scoped file path (for display purposes)
func (f *FileStorage) FilePath() string {
	return f.getHostScopedPath()
}

// GetCurrentCredentialKey returns the current credential key from the stored tokens
func (f *FileStorage) GetCurrentCredentialKey() string {
	data, err := f.Load()
	if err != nil || data == nil {
		return ""
	}

	// Parse JSON to extract email
	var tokens struct {
		UserInfo *struct {
			Email string `json:"email"`
		} `json:"user_info"`
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		return ""
	}
	if tokens.UserInfo != nil && tokens.UserInfo.Email != "" {
		// Build composite key from email + current host
		return config.BuildCredentialKey(tokens.UserInfo.Email, config.Get().Controlplane.Host)
	}
	return ""
}

// IsKeyringAvailable checks if the system keyring is available
func IsKeyringAvailable() bool {
	// Try to access the keyring
	_, err := keyring.Get(KeyringService, "__availability_test__")
	// ErrNotFound means keyring is available but key doesn't exist
	// Any other error means keyring is not available
	return err == nil || err == keyring.ErrNotFound
}

// GetStorage returns the appropriate storage backend based on config and availability
func GetStorage() Storage {
	cfg := config.Get()
	return pickStorage(cfg.Auth.Storage, cfg.Auth.Path, IsKeyringAvailable(), os.Stderr)
}

// pickStorage selects a storage backend and emits any operator-facing
// warnings on warn. Pulled out of GetStorage so the decision logic is
// testable without touching the real keyring or process stderr.
//
// Cases:
//   - "" (unset)         -> treated as "keyring"
//   - "keyring", avail   -> keyring (no warning)
//   - "keyring", !avail  -> file, with warning (refresh tokens land on disk)
//   - "file"             -> file (user opted in; no warning)
//   - anything else      -> file, with warning (unknown type, treated as "file")
func pickStorage(storageType, path string, keyringAvailable bool, warn io.Writer) Storage {
	if storageType == "" {
		storageType = "keyring"
	}

	switch storageType {
	case "file":
		// User explicitly opted into file storage; no warning.
		return NewFileStorage(path)
	case "keyring":
		if keyringAvailable {
			return NewKeyringStorage()
		}
		fmt.Fprintln(warn, "Warning: system keyring is not available; credentials will be stored in a plaintext file.")
		fmt.Fprintln(warn, "  This is risky on headless servers and CI runners. Either:")
		fmt.Fprintln(warn, "    - install/unlock the system keyring (libsecret on Linux, Keychain on macOS), or")
		fmt.Fprintln(warn, "    - set 'auth.storage: file' in config to acknowledge plaintext storage and suppress this warning.")
		fmt.Fprintln(warn, "  Once a keyring is available, run 'ndcli auth migrate' to move stored tokens out of the plaintext file.")
		return NewFileStorage(path)
	default:
		fmt.Fprintf(warn, "Warning: unknown auth.storage value %q; falling back to plaintext file storage.\n", storageType)
		fmt.Fprintln(warn, "  Set 'auth.storage' to 'keyring' or 'file' to silence this warning.")
		return NewFileStorage(path)
	}
}

// MigrateToKeyring migrates tokens from file storage to keyring
// Returns the credential key of the migrated account
func MigrateToKeyring() (string, error) {
	if !IsKeyringAvailable() {
		return "", fmt.Errorf("keyring is not available")
	}

	fileStorage := NewFileStorage("")
	keyringStorage := NewKeyringStorage()

	// Load from file
	data, err := fileStorage.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load from file: %w", err)
	}
	if data == nil {
		return "", fmt.Errorf("no tokens found in file storage")
	}

	// Parse to extract email
	var tokens struct {
		UserInfo *struct {
			Email string `json:"email"`
		} `json:"user_info"`
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		return "", fmt.Errorf("invalid token data: %w", err)
	}
	if tokens.UserInfo == nil || tokens.UserInfo.Email == "" {
		return "", fmt.Errorf("no email found in token data")
	}

	email := tokens.UserInfo.Email
	host := config.Get().Controlplane.Host
	credentialKey := config.BuildCredentialKey(email, host)

	// Save to keyring with credential key (email@host). Save prefixes its own
	// errors; wrapping again here printed "failed to save to keyring:" twice.
	if err := keyringStorage.Save(data, credentialKey); err != nil {
		return "", err
	}

	return credentialKey, nil
}

// GetFileStoragePath returns the path to the file storage
func GetFileStoragePath() string {
	return config.GetAuthFilePath()
}

// DeleteFileStorage removes the file storage file
func DeleteFileStorage() error {
	fs := NewFileStorage("")
	return fs.Clear()
}
