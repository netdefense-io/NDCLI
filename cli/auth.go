package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth/oauth2"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
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
	RunE:  runAuthLogout,
}

var authShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current authentication status",
	RunE:  runAuthShow,
}

var authMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show information about the authenticated user",
	RunE:  runAuthMe,
}

var authRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force refresh the access token",
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

	// Login flags
	authLoginCmd.Flags().Bool("force", false, "Force new login even if already authenticated")
	authLoginCmd.Flags().String("scopes", "", "OAuth2 scopes (default from config)")

	// Delete flags
	authDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	scopes, _ := cmd.Flags().GetString("scopes")

	// Check if already authenticated
	if !force && authManager.IsAuthenticated() {
		userInfo, err := authManager.GetUserInfo()
		if err == nil && userInfo != nil {
			color.Green("Already authenticated as: %s", userInfo.Email)
			fmt.Println("Use --force to re-authenticate")
			return nil
		}
	}

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
	if !authManager.IsAuthenticated() {
		color.Red("✗ Not authenticated")
		fmt.Println("Run 'ndcli auth login' to authenticate")
		return nil
	}

	if err := authManager.Logout(); err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}

	color.Green("✓ Successfully logged out")
	return nil
}

func runAuthShow(cmd *cobra.Command, args []string) error {
	// Always show storage backend
	storageName := authManager.GetStorageName()

	currentHost := config.GetCurrentHost()

	if !authManager.IsAuthenticated() {
		color.Red("✗ Not authenticated")
		fmt.Printf("  Host:      %s\n", currentHost)
		fmt.Printf("  Storage:   %s\n", storageName)
		fmt.Println("\nRun 'ndcli auth login' to authenticate")
		return nil
	}

	summary := authManager.GetTokenSummary()
	if summary == nil {
		color.Red("✗ Not authenticated")
		fmt.Printf("  Host:      %s\n", currentHost)
		fmt.Printf("  Storage:   %s\n", storageName)
		fmt.Println("\nRun 'ndcli auth login' to authenticate")
		return nil
	}

	color.Cyan("Authentication Status:")
	fmt.Println()

	if email, ok := summary["email"].(string); ok {
		fmt.Printf("  Email:     %s\n", email)
	}
	if name, ok := summary["name"].(string); ok {
		fmt.Printf("  Name:      %s\n", name)
	}
	if subject, ok := summary["subject"].(string); ok {
		fmt.Printf("  Subject:   %s\n", subject)
	}
	if expiresAt, ok := summary["expires_at"].(string); ok {
		fmt.Printf("  Expires:   %s\n", expiresAt)
	}
	if isExpired, ok := summary["is_expired"].(bool); ok {
		if isExpired {
			color.Red("  Status:    Expired")
		} else {
			color.Green("  Status:    Valid")
		}
	}
	if hasRefresh, ok := summary["has_refresh"].(bool); ok {
		if hasRefresh {
			fmt.Println("  Refresh:   Available")
		}
	}
	fmt.Printf("  Host:      %s\n", currentHost)
	fmt.Printf("  Storage:   %s\n", storageName)

	return nil
}

func runAuthMe(cmd *cobra.Command, args []string) error {
	requireAuth()

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/auth/me", nil)
	if err != nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}

	var authMe models.AuthMe
	if err := api.ParseResponse(resp, &authMe); err != nil {
		return err
	}

	return formatter.FormatAuthMe(&authMe)
}

func runAuthRefresh(cmd *cobra.Command, args []string) error {
	if !authManager.IsAuthenticated() {
		color.Red("✗ Not authenticated")
		fmt.Println("Run 'ndcli auth login' to authenticate")
		return nil
	}

	if err := authManager.ForceRefresh(); err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}

	color.Green("✓ Token refreshed successfully")
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

	// Get user info for display
	userInfo, err := authManager.GetUserInfo()
	if err != nil || userInfo == nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}

	// Confirm deletion unless --yes flag is provided
	if !skipConfirm {
		color.Red("WARNING: This will permanently delete your account!")
		fmt.Printf("  Email: %s\n", userInfo.Email)
		fmt.Println()
		fmt.Print("Type 'DELETE' to confirm: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(response)

		if response != "DELETE" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Call DELETE /api/v1/auth/me
	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, "/api/v1/auth/me")
	if err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}

	// Check for conflict error (sole superuser)
	if resp.StatusCode == 409 {
		apiErr := api.ParseError(resp)
		color.Red("✗ Cannot delete account")
		fmt.Println()
		fmt.Println(apiErr.Message)
		if len(apiErr.BlockingResources) > 0 {
			fmt.Println()
			fmt.Println("Organizations where you are the only superuser:")
			for _, org := range apiErr.BlockingResources {
				fmt.Printf("  • %s\n", org)
			}
		}
		return nil
	}

	// Parse success response
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Email   string `json:"email"`
	}
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	// Log out locally after successful deletion
	_ = authManager.Logout()

	color.Green("✓ %s", result.Message)
	fmt.Printf("  Email: %s\n", result.Email)
	return nil
}
