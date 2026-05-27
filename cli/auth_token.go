package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// authTokenCmd is the parent command — `ndcli auth token`
var authTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Personal access token management",
}

var authTokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a personal access token",
	Long: `Create a personal access token (PAT) for use with NDCLI_TOKEN.

The raw token value is displayed once and cannot be retrieved again.
Token create always requires interactive authentication — NDCLI_TOKEN
cannot be used for this command.`,
	RunE: runAuthTokenCreate,
}

var authTokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List personal access tokens",
	RunE:  runAuthTokenList,
}

var authTokenRevokeCmd = &cobra.Command{
	Use:   "revoke <name>",
	Short: "Revoke a personal access token",
	Long: `Revoke a personal access token by name.

Token revoke always requires interactive authentication — NDCLI_TOKEN
cannot be used for this command.`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthTokenRevoke,
}

func init() {
	authTokenCmd.AddCommand(authTokenCreateCmd)
	authTokenCmd.AddCommand(authTokenListCmd)
	authTokenCmd.AddCommand(authTokenRevokeCmd)

	// Create flags
	authTokenCreateCmd.Flags().String("name", "", "Token name (required)")
	authTokenCreateCmd.Flags().String("scope", "", "Token scope: RW or RO (required)")
	authTokenCreateCmd.Flags().String("org", "", "Restrict token to a specific organization (optional)")
	authTokenCreateCmd.Flags().String("expiry", "90d", "Token expiry: 30d, 60d, 90d, 180d, 365d, or never")
	authTokenCreateCmd.MarkFlagRequired("name")
	authTokenCreateCmd.MarkFlagRequired("scope")

	// Revoke flags
	authTokenRevokeCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runAuthTokenCreate(cmd *cobra.Command, args []string) error {
	requireAuth()

	name, _ := cmd.Flags().GetString("name")
	scope, _ := cmd.Flags().GetString("scope")
	org, _ := cmd.Flags().GetString("org")
	expiry, _ := cmd.Flags().GetString("expiry")

	validExpiries := map[string]bool{
		"30d": true, "60d": true, "90d": true,
		"180d": true, "365d": true, "never": true,
	}
	if !validExpiries[expiry] {
		return fmt.Errorf("invalid expiry %q — must be one of: 30d, 60d, 90d, 180d, 365d, never", expiry)
	}

	opts := service.TokenCreateOpts{
		Name:      name,
		Scope:     strings.ToUpper(scope),
		Org:       org,
		ExpiresIn: expiry,
	}

	result, err := svc.TokenCreate(context.Background(), opts)
	if err != nil {
		return err
	}

	return formatter.FormatTokenCreated(result.Token)
}

func runAuthTokenList(cmd *cobra.Command, args []string) error {
	requireAuth()

	tokens, err := svc.TokenList(context.Background())
	if err != nil {
		return err
	}

	return formatter.FormatPersonalAccessTokens(tokens)
}

func runAuthTokenRevoke(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	if !skipConfirm {
		fmt.Printf("Revoke token %q? This cannot be undone. [y/N]: ", name)
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := svc.TokenRevoke(context.Background(), name); err != nil {
		return err
	}

	return formatter.FormatTokenRevoked(name)
}
