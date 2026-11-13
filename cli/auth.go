package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth/oauth2"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/storage"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication management commands",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with NetDefense",
	Long:  "Authenticate using the OAuth2 device authorization flow",
	Args:  noArgs,
	RunE:  runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and revoke tokens",
	Long: `Log out: revoke the stored refresh token on the server (best effort) and remove the
stored session. This works even when the access token has already expired.`,
	Args: noArgs,
	RunE: runAuthLogout,
}

var authShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current authentication status",
	Long: `Show the stored session: who is signed in, when the access token expires and whether a
refresh token is available. It reads local state only: it never contacts the server or
refreshes the token. An expired access token with a refresh token is a working session
that renews itself on the next command.`,
	Args: noArgs,
	RunE: runAuthShow,
}

var authMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show information about the authenticated user",
	Args:  noArgs,
	RunE:  runAuthMe,
}

var authRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force refresh the access token",
	Long:  "Refresh the access token with the stored refresh token. Works when the access token has already expired.",
	Args:  noArgs,
	RunE:  runAuthRefresh,
}

var authMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate tokens from file storage to keyring",
	Args:  noArgs,
	RunE:  runAuthMigrate,
}

var authDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Permanently delete your account",
	Long:  "Permanently delete your NetDefense account. This action cannot be undone.",
	Args:  noArgs,
	RunE:  runAuthDelete,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authShowCmd)
	authCmd.AddCommand(authMeCmd)
	authCmd.AddCommand(authRefreshCmd)
	authCmd.AddCommand(authMigrateCmd)
	addYesFlag(authMigrateCmd)
	authCmd.AddCommand(authDeleteCmd)
	authCmd.AddCommand(authTokenCmd)

	// Login flags
	authLoginCmd.Flags().Bool("force", false, "Force new login even if already authenticated")
	authLoginCmd.Flags().String("scopes", "", "OAuth2 scopes (default from config)")

	// Delete flags
	authDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

// errLoginWithStaticToken is why `auth login` refuses to run under NDCLI_TOKEN:
// the token wins over any stored session, so a login would change nothing.
var errLoginWithStaticToken = errors.New("cannot log in while NDCLI_TOKEN is set: unset it to authenticate interactively")

func runAuthLogin(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	scopes, _ := cmd.Flags().GetString("scopes")

	// authManager is nil under NDCLI_TOKEN; refuse before anything touches it.
	if svc.AuthStatus().State == models.SessionStatic {
		return errLoginWithStaticToken
	}

	// Check if already authenticated
	if !force && authManager.IsAuthenticated() {
		userInfo, err := authManager.GetUserInfo()
		if err == nil && userInfo != nil {
			color.Green("Already authenticated as: %s", userInfo.Email)
			fmt.Println("Use --force to re-authenticate")
			return nil
		}
	}

	warnIfTLSVerifyDisabled(os.Stderr)

	// Perform login
	ctx := context.Background()
	_, err := authManager.Login(ctx, scopes, force)
	if err != nil {
		// If error was already displayed interactively, just exit silently
		if errors.Is(err, oauth2.ErrAuthDisplayed) {
			return nil
		}
		return fmt.Errorf("login failed: %w", err)
	}

	// Show success message
	userInfo, _ := authManager.GetUserInfo()
	if userInfo != nil {
		color.Green("\n✓ Successfully authenticated!")
		if userInfo.Name != "" {
			fmt.Printf("  Name: %s\n", userInfo.Name)
		}
		if userInfo.Email != "" {
			fmt.Printf("  Email: %s\n", userInfo.Email)
		}
	} else {
		color.Green("\n✓ Successfully authenticated!")
	}

	// Record login and check for pending invites (pass name from Auth0 token)
	recordLoginAndShowInvites(ctx, userInfo)

	return nil
}

// warnIfTLSVerifyDisabled prints a stderr warning when controlplane TLS
// verification is disabled. This is the point where the OAuth2 issuer
// domain — and thus where credentials subsequently flow — gets determined,
// so an operator running with ssl_verify=false should see the MITM risk
// surfaced right before authenticating.
func warnIfTLSVerifyDisabled(w io.Writer) {
	if config.Get().Controlplane.SSLVerify {
		return
	}
	color.New(color.FgYellow).Fprintln(w, "Warning: TLS certificate verification is disabled (controlplane.ssl_verify=false). "+
		"Authentication traffic to the control plane is not protected against interception.")
}

// recordLoginAndShowInvites calls POST /api/v1/auth/me to record login and display pending invites
func recordLoginAndShowInvites(ctx context.Context, userInfo *models.UserInfo) {
	// Send name from Auth0 token if available
	payload := map[string]interface{}{}
	if userInfo != nil && userInfo.Name != "" {
		payload["name"] = userInfo.Name
	}

	resp, err := apiClient.Post(ctx, "/api/v1/auth/me", payload)
	if err != nil {
		return // Silently ignore errors - this is a best-effort call
	}

	var result models.AuthMeUpdateResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		if api.IsRegistrationRestrictedError(err) {
			fmt.Println()
			color.Red("✗ Registration restricted")
			fmt.Println("  Registration is currently restricted to invited users.")
			fmt.Println("  Please contact an administrator to request access.")
		}
		return
	}

	// Show account creation message for new users
	if result.Message == "Account created" {
		fmt.Println()
		color.Green("✓ Account created successfully")
	}

	// Show pending invites if any
	if len(result.PendingInvites) > 0 {
		fmt.Println()
		color.Yellow("You have %d pending organization invite(s):", len(result.PendingInvites))
		for _, inv := range result.PendingInvites {
			fmt.Printf("  • %s [%s] from %s\n", inv.Organization, inv.Role, inv.InvitedBy)
		}
		fmt.Println()
		color.Cyan("Use 'ndcli org invite accept <organization>' to accept an invite")
	}
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	return logoutSession(color.Output, svc)
}

// logoutSession clears whatever session is stored. It keys off the stored
// credentials, not the access token: an expired access token still leaves a
// refresh token behind, and that is the one worth revoking.
func logoutSession(w io.Writer, s *service.Service) error {
	switch s.AuthStatus().State {
	case models.SessionStatic:
		fmt.Fprintln(w, "NDCLI_TOKEN is set: there is no stored session to log out of. Unset the variable to stop using it.")
		return nil
	case models.SessionNone:
		color.New(color.FgRed).Fprintln(w, "✗ Not authenticated")
		fmt.Fprintln(w, "Run 'ndcli auth login' to authenticate")
		return nil
	}

	if err := s.AuthLogout(); err != nil {
		return err
	}

	color.New(color.FgGreen).Fprintln(w, "✓ Successfully logged out")
	return nil
}

// runAuthShow reports the stored session. It is deliberately passive: no
// network call and no refresh, because the refresh token rotates and a status
// command must not race a running TUI, MCP server or another ndcli for it.
func runAuthShow(cmd *cobra.Command, args []string) error {
	return formatter.FormatAuthShow(models.NewAuthShow(svc.AuthStatus(), config.GetCurrentHost(), svc.AuthStorageName()))
}

// renderAuthStatus is output.WriteAuthShow for a session status, a pure
// function of its inputs so every state can be tested without a keyring.
func renderAuthStatus(w io.Writer, st *models.SessionStatus, host, storageName string, now time.Time) {
	output.WriteAuthShow(w, models.NewAuthShow(st, host, storageName), now)
}

func agoSince(now, t time.Time) string { return output.AgoSince(now, t) }

func runAuthMe(cmd *cobra.Command, args []string) error {
	requireAuth()
	authMe, err := svc.AuthMe(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}
	return formatter.FormatAuthMe(authMe)
}

func runAuthRefresh(cmd *cobra.Command, args []string) error {
	return refreshSession(color.Output, svc)
}

// refreshSession renews the access token. It must not require an unexpired one:
// an expired access token is exactly when a refresh is needed.
func refreshSession(w io.Writer, s *service.Service) error {
	switch s.AuthStatus().State {
	case models.SessionStatic:
		fmt.Fprintln(w, "NDCLI_TOKEN is set: a personal access token has nothing to refresh.")
		return nil
	case models.SessionNone:
		color.New(color.FgRed).Fprintln(w, "✗ Not authenticated")
		fmt.Fprintln(w, "Run 'ndcli auth login' to authenticate")
		return nil
	}

	if err := s.AuthRefresh(); err != nil {
		return err
	}
	color.New(color.FgGreen).Fprintln(w, "✓ Token refreshed successfully")
	return nil
}

// leftoverAuthFile reports whether the auth file is one a migration left
// behind: the keyring holds exactly its bytes, as MigrateToKeyring copied them,
// so deleting it loses nothing. The config alone does not prove that:
// auth.storage can say keyring by hand or through NDCLI_AUTH_STORAGE, and the
// keyring can hold an older session or another account's.
func leftoverAuthFile(loadFile, loadKeyring func() ([]byte, error)) bool {
	file, err := loadFile()
	if err != nil || len(file) == 0 {
		return false
	}
	inKeyring, err := loadKeyring()
	if err != nil || len(inKeyring) == 0 {
		return false
	}
	return bytes.Equal(file, inKeyring)
}

// deleteOldAuthFile asks whether to delete the auth file the keyring has
// replaced, and deletes it on a yes.
func deleteOldAuthFile(cmd *cobra.Command) error {
	filePath := storage.NewFileStorage("").FilePath()
	fmt.Fprintln(os.Stderr)
	deleteFile, err := askUnlessYes(cmd, fmt.Sprintf("Delete old auth file (%s)?", filePath), true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "  Old file kept at:", filePath)
		return err
	}
	if !deleteFile {
		fmt.Println("  Old file kept at:", filePath)
		return nil
	}
	if err := storage.DeleteFileStorage(); err != nil {
		return fmt.Errorf("failed to delete old auth file %s: %w", filePath, err)
	}
	color.Green("✓ Old auth file deleted")
	return nil
}

func runAuthMigrate(cmd *cobra.Command, args []string) error {
	// Check if keyring is available
	if !storage.IsKeyringAvailable() {
		return fmt.Errorf("system keyring is not available on this system")
	}

	// Check current storage type
	cfg := config.Get()
	if cfg.Auth.Storage == "keyring" {
		color.Yellow("Already using keyring storage")
		// A migration whose last question went unanswered leaves the file
		// behind; a rerun finishes it.
		if leftoverAuthFile(storage.NewFileStorage("").Load, storage.NewKeyringStorage().Load) {
			return deleteOldAuthFile(cmd)
		}
		return nil
	}

	// Perform migration
	email, err := storage.MigrateToKeyring()
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	color.Green("✓ Tokens migrated to system keyring successfully")
	fmt.Printf("  Account: %s\n", email)
	fmt.Println()

	updateConfig, err := askUnlessYes(cmd, "Update config to use keyring storage?", true)
	if err != nil {
		return err
	}
	if updateConfig {
		// Each failure stops before the old file is offered for deletion,
		// and exits non-zero: with -y nobody is watching.
		if err := config.UpdateValue("auth.storage", "keyring"); err != nil {
			return fmt.Errorf("failed to update config: %w", err)
		}
		color.Green("✓ Config updated to use keyring storage")

		// Verify keyring is working
		fmt.Println()
		fmt.Println("Verifying keyring storage...")
		data, err := storage.NewKeyringStorage().Load()
		if err != nil {
			return fmt.Errorf("failed to read from keyring: %w (you may need to revert the config change by hand)", err)
		}
		if data == nil {
			return fmt.Errorf("no data found in keyring (you may need to revert the config change by hand)")
		}
		if !leftoverAuthFile(storage.NewFileStorage("").Load, storage.NewKeyringStorage().Load) {
			return fmt.Errorf("the keyring does not hold the session in %s, so it is kept", storage.NewFileStorage("").FilePath())
		}
		color.Green("✓ Keyring storage verified")

		return deleteOldAuthFile(cmd)
	} else {
		fmt.Println()
		fmt.Println("To use keyring storage, add this to your config:")
		fmt.Println("  auth:")
		fmt.Println("    storage: keyring")
	}

	return nil
}

func runAuthDelete(cmd *cobra.Command, args []string) error {
	requireAuth()

	skipConfirm, _ := cmd.Flags().GetBool("yes")

	userInfo, err := svc.AuthLocalUser()
	if err != nil || userInfo == nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}

	if !skipConfirm {
		color.New(color.FgRed).Fprintln(os.Stderr, "WARNING: This will permanently delete your account!")
		fmt.Fprintf(os.Stderr, "  Email: %s\n", userInfo.Email)
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, "Type 'DELETE' to confirm: ")

		response, answered := helpers.ReadAnswer()
		if !answered && !helpers.StdinIsTerminal() {
			fmt.Fprintln(os.Stderr)
			return unansweredPromptError(cmd)
		}
		if response != "DELETE" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	result, err := svc.AuthDelete(context.Background())
	if err != nil {
		// Sole-superuser conflict: list the organizations that block it.
		if blocking := service.AuthDeleteBlockingOrgs(err); blocking != nil {
			return refusalWithList(err, err.Error(), "Organizations where you are the only superuser:", blocking)
		}
		return fmt.Errorf("failed to delete account: %w", err)
	}

	color.Green("✓ %s", result.Message)
	fmt.Printf("  Email: %s\n", result.Email)
	return nil
}
