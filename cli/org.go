package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
)

var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "Organization management commands",
}

var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations",
	RunE:  runOrgList,
}

var orgCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new organization",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgCreate,
}

var orgDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgDelete,
}

var orgDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show organization details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgDescribe,
}

var orgSetDefaultOUCmd = &cobra.Command{
	Use:               "set-default-ou [ou-name]",
	Short:             "Set the default OU for an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOrgSetDefaultOU,
}

// Invite subcommands
var orgInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "Manage organization invitations",
}

var orgInviteSendCmd = &cobra.Command{
	Use:   "send [email]",
	Short: "Send an organization invitation",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgInviteSend,
}

var orgInviteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending invitations",
	RunE:  runOrgInviteList,
}

var orgInviteAcceptCmd = &cobra.Command{
	Use:               "accept [org-name]",
	Short:             "Accept an invitation to join an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgInviteAccept,
}

var orgInviteDeclineCmd = &cobra.Command{
	Use:               "decline [org-name]",
	Short:             "Decline an invitation to join an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgInviteDecline,
}

var orgInviteRevokeCmd = &cobra.Command{
	Use:   "revoke [email]",
	Short: "Revoke a pending invitation (requires superuser)",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgInviteRevoke,
}

var orgQuotaCmd = &cobra.Command{
	Use:               "quota [org]",
	Short:             "Show organization quota and plan limits",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgQuota,
}

// Account subcommands
var orgAccountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage organization accounts",
}

var orgAccountListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organization accounts",
	RunE:  runOrgAccountList,
}

var orgAccountDisableCmd = &cobra.Command{
	Use:   "disable [email]",
	Short: "Disable an account",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgAccountDisable,
}

var orgAccountEnableCmd = &cobra.Command{
	Use:   "enable [email]",
	Short: "Enable an account",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgAccountEnable,
}

var orgAccountRoleCmd = &cobra.Command{
	Use:   "role [email] [role]",
	Short: "Set account role",
	Long: `Set account role for a user in the organization.

Valid roles:
  SU, superuser  - Full administrative access
  RW, readwrite  - Can view and modify resources
  RO, readonly   - Can only view resources`,
	Args: cobra.ExactArgs(2),
	RunE: runOrgAccountRole,
}


func init() {
	orgCmd.AddCommand(orgListCmd)
	orgCmd.AddCommand(orgCreateCmd)
	orgCmd.AddCommand(orgDeleteCmd)
	orgCmd.AddCommand(orgDescribeCmd)
	orgCmd.AddCommand(orgSetDefaultOUCmd)
	orgCmd.AddCommand(orgQuotaCmd)
	orgCmd.AddCommand(orgInviteCmd)
	orgCmd.AddCommand(orgAccountCmd)

	// Invite subcommands
	orgInviteCmd.AddCommand(orgInviteSendCmd)
	orgInviteCmd.AddCommand(orgInviteListCmd)
	orgInviteCmd.AddCommand(orgInviteAcceptCmd)
	orgInviteCmd.AddCommand(orgInviteDeclineCmd)
	orgInviteCmd.AddCommand(orgInviteRevokeCmd)

	// Account subcommands
	orgAccountCmd.AddCommand(orgAccountListCmd)
	orgAccountCmd.AddCommand(orgAccountDisableCmd)
	orgAccountCmd.AddCommand(orgAccountEnableCmd)
	orgAccountCmd.AddCommand(orgAccountRoleCmd)

	// Disable flags
	orgAccountDisableCmd.Flags().Bool("remove", false, "Permanently remove the account from the organization (cannot be re-enabled)")

	// List flags
	orgListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction")
	orgListCmd.Flags().Int("page", 1, "Page number")
	orgListCmd.Flags().Int("per-page", 30, "Items per page")
	orgListCmd.Flags().String("name", "", "Filter by name (regex)")
	orgListCmd.Flags().String("role", "", "Filter by role (RO, RW, SU)")
	orgListCmd.Flags().String("status", "", "Filter by status (ENABLED, DISABLED, INVITED, DECLINED)")

	// Create flags
	orgCreateCmd.Flags().String("display-name", "", "Display name for the organization")
	orgCreateCmd.Flags().String("description", "", "Organization description")

	// Invite send flags
	orgInviteSendCmd.Flags().String("role", "RO", "Role for the invitee (SU, RW, RO)")
}

func runOrgList(cmd *cobra.Command, args []string) error {
	requireAuth()

	sortBy, _ := cmd.Flags().GetString("sort-by")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	name, _ := cmd.Flags().GetString("name")
	role, _ := cmd.Flags().GetString("role")
	status, _ := cmd.Flags().GetString("status")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if sortBy != "" {
		params["sort_by"] = sortBy
	}
	if name != "" {
		params["name"] = name
	}
	if role != "" {
		params["role"] = strings.ToUpper(role)
	}
	if status != "" {
		params["status"] = strings.ToUpper(status)
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/organizations", params)
	if err != nil {
		return err
	}

	var result models.OrganizationListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	items := result.GetItems()
	if err := formatter.FormatOrganizations(items); err != nil {
		return err
	}

	output.PrintPagination(page, result.GetTotal(), perPage)
	return nil
}

func runOrgCreate(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]
	displayName, _ := cmd.Flags().GetString("display-name")
	description, _ := cmd.Flags().GetString("description")

	ctx := context.Background()

	// Check if we should auto-set this org as default after creation
	// Conditions: no default org set AND user has no existing orgs
	currentDefault := config.Get().Organization.Name
	shouldAutoSetDefault := false

	if currentDefault == "" {
		// Check if user has any existing organizations
		listResp, err := apiClient.Get(ctx, "/api/v1/organizations", nil)
		if err == nil {
			var listResult models.OrganizationListResponse
			if err := api.ParseResponse(listResp, &listResult); err == nil {
				shouldAutoSetDefault = len(listResult.GetItems()) == 0
			}
		}
	}

	payload := map[string]string{
		"name": name,
	}
	if displayName != "" {
		payload["display_name"] = displayName
	}
	if description != "" {
		payload["description"] = description
	}

	resp, err := apiClient.Post(ctx, "/api/v1/organizations", payload)
	if err != nil {
		return err
	}

	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		if api.IsRegistrationRestrictedError(err) {
			color.Red("✗ Registration restricted")
			fmt.Println()
			fmt.Println("Registration is currently restricted to invited users.")
			fmt.Println("Please contact an administrator to request access.")
			return nil
		}
		return err
	}

	// Auto-set as default if this is the user's first organization
	if shouldAutoSetDefault {
		if err := config.UpdateValue("organization.name", name); err != nil {
			// Log warning but don't fail the command
			color.Yellow("⚠ Could not set as default organization: %v\n", err)
		} else {
			color.Green("✓ Organization created and set as default: %s\n", name)
			fmt.Printf("  Token: %s\n", org.Token)
			fmt.Printf("\nUse this token to register devices with the NetDefense agent.\n")
			fmt.Printf("To retrieve the token later: ndcli org describe %s\n", name)
			return nil
		}
	}

	color.Green("✓ Organization created: %s\n", name)
	fmt.Printf("  Token: %s\n", org.Token)
	fmt.Printf("\nUse this token to register devices with the NetDefense agent.\n")
	fmt.Printf("To retrieve the token later: ndcli org describe %s\n", name)
	fmt.Printf("\nTo set this organization as your default:\n")
	fmt.Printf("  ndcli config set org %s\n", name)
	return nil
}

func runOrgDelete(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]

	if !helpers.Confirm(fmt.Sprintf("Delete organization '%s'? This action cannot be undone.", name)) {
		fmt.Println("Cancelled")
		return nil
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s", name))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Organization deleted: %s", name)
	return nil
}

func runOrgDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s", name), nil)
	if err != nil {
		return err
	}

	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		return err
	}

	return formatter.FormatOrganization(&org)
}

func runOrgQuota(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/quota", name), nil)
	if err != nil {
		return err
	}

	var result models.OrgQuota
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatOrgQuota(&result)
}

func runOrgSetDefaultOU(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]

	payload := map[string]string{"ou_name": ouName}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/default-ou", org), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Default OU set to: %s", ouName)
	return nil
}

func runOrgInviteSend(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]
	role, _ := cmd.Flags().GetString("role")

	payload := map[string]string{
		"email": email,
		"role":  role,
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites", org), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Invitation sent to: %s", email)
	return nil
}

func runOrgInviteList(cmd *cobra.Command, args []string) error {
	requireAuth()

	params := map[string]string{
		"direction": "all",
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/invites", params)
	if err != nil {
		return err
	}

	var result models.InvitesResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatInvites(&result)
}

func runOrgInviteAccept(cmd *cobra.Command, args []string) error {
	requireAuth()

	orgName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/accept", orgName), nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Invitation to %s accepted", orgName)
	return nil
}

func runOrgInviteDecline(cmd *cobra.Command, args []string) error {
	requireAuth()

	orgName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/decline", orgName), nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Invitation to %s declined", orgName)
	return nil
}

func runOrgInviteRevoke(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/%s", org, email))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Invitation revoked for: %s", email)
	return nil
}

func runOrgAccountList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts", org), nil)
	if err != nil {
		return err
	}

	var result models.AccountListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatAccounts(result.Accounts, result.Quota)
}

func runOrgAccountDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]
	remove, _ := cmd.Flags().GetBool("remove")

	// Different confirmation message based on remove flag
	var confirmMsg string
	if remove {
		confirmMsg = fmt.Sprintf("Permanently remove account '%s' from organization '%s'? This cannot be undone.", email, org)
	} else {
		confirmMsg = fmt.Sprintf("Disable account '%s'?", email)
	}

	if !helpers.Confirm(confirmMsg) {
		fmt.Println("Cancelled")
		return nil
	}

	// Build endpoint with optional remove parameter
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/disable", org, email)
	if remove {
		endpoint += "?remove=true"
	}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, endpoint, nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		// Make error more user-friendly
		if api.IsNotFoundError(err) {
			return fmt.Errorf("account '%s' not found in organization '%s'", email, org)
		}
		if strings.Contains(err.Error(), "not ENABLED") {
			return fmt.Errorf("account '%s' is already disabled", email)
		}
		return err
	}

	if remove {
		color.Green("✓ Account removed from organization: %s", email)
	} else {
		color.Green("✓ Account disabled: %s", email)
	}
	return nil
}

func runOrgAccountEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/enable", org, email), nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		// Make error more user-friendly
		if api.IsNotFoundError(err) {
			return fmt.Errorf("account '%s' not found in organization '%s'", email, org)
		}
		if strings.Contains(err.Error(), "not DISABLED") {
			return fmt.Errorf("account '%s' is already enabled", email)
		}
		return err
	}

	color.Green("✓ Account enabled: %s", email)
	return nil
}

func runOrgAccountRole(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]
	roleInput := strings.ToLower(args[1])

	// Normalize role to short form
	var role string
	switch roleInput {
	case "su", "superuser":
		role = "SU"
	case "rw", "readwrite":
		role = "RW"
	case "ro", "readonly":
		role = "RO"
	default:
		return fmt.Errorf("invalid role: %s. Valid roles: SU/superuser, RW/readwrite, RO/readonly", args[1])
	}

	payload := map[string]string{"role": role}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/accounts/%s/role", org, email), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Role set to %s for: %s", role, email)
	return nil
}
