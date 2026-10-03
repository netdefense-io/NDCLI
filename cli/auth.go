package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth/oauth2"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
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
	RunE:  runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and revoke tokens",
	Long: `Log out: revoke the stored refresh token on the server (best effort) and remove the
stored session. This works even when the access token has already expired.`,
	RunE: runAuthLogout,
}

var authShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current authentication status",
	Long: `Show the stored session: who is signed in, when the access token expires and whether a
refresh token is available. It reads local state only: it never contacts the server or
refreshes the token. An expired access token with a refresh token is a working session
that renews itself on the next command.`,
	RunE: runAuthShow,
}

var authMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show information about the authenticated user",
	RunE:  runAuthMe,
}

var authRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force refresh the access token",
	Long:  "Refresh the access token with the stored refresh token. Works when the access token has already expired.",
	RunE:  runAuthRefresh,
}

var authMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate tokens from file storage to keyring",
	RunE:  runAuthMigrate,
}

var authDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Permanently delete your account",
	Long:  "Permanently delete your NetDefense account. This action cannot be undone.",
	RunE:  runAuthDelete,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authShowCmd)
	authCmd.AddCommand(authMeCmd)
	authCmd.AddCommand(authRefreshCmd)
	authCmd.AddCommand(authMigrateCmd)
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
	renderAuthStatus(color.Output, svc.AuthStatus(), config.GetCurrentHost(), svc.AuthStorageName(), time.Now())
	return nil
}

// renderAuthStatus prints one of the five session states. It is a pure function
// of its inputs so every state can be tested without a keyring.
func renderAuthStatus(w io.Writer, st *models.SessionStatus, host, storageName string, now time.Time) {
	footer := func() {
		fmt.Fprintf(w, "  Host:      %s\n", host)
		fmt.Fprintf(w, "  Storage:   %s\n", storageName)
	}
	expired := func() {
		fmt.Fprintf(w, "  Expired:   %s (%s)\n", st.ExpiresAt.Format(time.RFC3339), agoSince(now, st.ExpiresAt))
	}

	switch st.State {
	case models.SessionStatic:
		color.New(color.FgCyan).Fprintln(w, "Authentication Status:")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Method:    Personal access token (NDCLI_TOKEN)")
		footer()

	case models.SessionValid, models.SessionRefreshable:
		color.New(color.FgCyan).Fprintln(w, "Authentication Status:")
		fmt.Fprintln(w)
		if st.Email != "" {
			fmt.Fprintf(w, "  Email:     %s\n", st.Email)
		}
		if st.Name != "" {
			fmt.Fprintf(w, "  Name:      %s\n", st.Name)
		}
		if st.Subject != "" {
			fmt.Fprintf(w, "  Subject:   %s\n", st.Subject)
		}
		if st.State == models.SessionValid {
			fmt.Fprintf(w, "  Expires:   %s\n", st.ExpiresAt.Format(time.RFC3339))
			color.New(color.FgGreen).Fprintln(w, "  Status:    Valid")
		} else {
			// Yellow, not red: nothing is wrong, the next command renews it.
			expired()
			color.New(color.FgYellow).Fprintln(w, "  Status:    Access token expired; it refreshes automatically on the next command")
		}
		if st.HasRefresh {
			fmt.Fprintln(w, "  Refresh:   Available")
		}
		footer()

	case models.SessionExpired:
		color.New(color.FgRed).Fprintln(w, "✗ Session expired")
		if st.Email != "" {
			fmt.Fprintf(w, "  Email:     %s\n", st.Email)
		}
		expired()
		footer()
		fmt.Fprintln(w, "\nRun 'ndcli auth login' to authenticate")

	default:
		color.New(color.FgRed).Fprintln(w, "✗ Not authenticated")
		footer()
		fmt.Fprintln(w, "\nRun 'ndcli auth login' to authenticate")
	}
}

// agoSince renders how long before now t was. A token inside the 60 s refresh
// buffer has not reached its expiry yet and reads as "just now".
func agoSince(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

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

func runAuthMigrate(cmd *cobra.Command, args []string) error {
	// Check if keyring is available
	if !storage.IsKeyringAvailable() {
		return fmt.Errorf("system keyring is not available on this system")
	}

	// Check current storage type
	cfg := config.Get()
	if cfg.Auth.Storage == "keyring" {
		color.Yellow("Already using keyring storage")
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

	// Prompt to update config
	fmt.Print("Update config to use keyring storage? [Y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "" || response == "y" || response == "yes" {
		if err := config.UpdateValue("auth.storage", "keyring"); err != nil {
			color.Red("Failed to update config: %v", err)
		} else {
			color.Green("✓ Config updated to use keyring storage")
		}

		// Verify keyring is working
		fmt.Println()
		fmt.Println("Verifying keyring storage...")
		keyringStorage := storage.NewKeyringStorage()
		data, err := keyringStorage.Load()
		if err != nil {
			color.Red("✗ Failed to read from keyring: %v", err)
			fmt.Println("  You may need to manually revert the config change")
			return nil
		}
		if data == nil {
			color.Red("✗ No data found in keyring")
			fmt.Println("  You may need to manually revert the config change")
			return nil
		}
		color.Green("✓ Keyring storage verified")

		// Prompt to delete old file
		filePath := storage.GetFileStoragePath()
		fmt.Println()
		fmt.Printf("Delete old auth file (%s)? [Y/n]: ", filePath)
		response, _ = reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "" || response == "y" || response == "yes" {
			if err := storage.DeleteFileStorage(); err != nil {
				color.Red("Failed to delete old file: %v", err)
			} else {
				color.Green("✓ Old auth file deleted")
			}
		} else {
			fmt.Println("  Old file kept at:", filePath)
		}
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

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(response)

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
