package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
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
	Use:               "disable [email]",
	Short:             "Disable an account",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeAccountEmails,
	RunE:              runOrgAccountDisable,
}

var orgAccountEnableCmd = &cobra.Command{
	Use:               "enable [email]",
	Short:             "Enable an account",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeAccountEmails,
	RunE:              runOrgAccountEnable,
}

var orgAccountRoleCmd = &cobra.Command{
	Use:   "role [email] [role]",
	Short: "Set account role",
	Long: `Set account role for a user in the organization.

Valid roles:
  SU, superuser  - Full administrative access
  RW, readwrite  - Can view and modify resources
  RO, readonly   - Can only view resources`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeAccountEmailThenRole,
	RunE:              runOrgAccountRole,
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

	orgInviteCmd.AddCommand(orgInviteSendCmd)
	orgInviteCmd.AddCommand(orgInviteListCmd)
	orgInviteCmd.AddCommand(orgInviteAcceptCmd)
	orgInviteCmd.AddCommand(orgInviteDeclineCmd)
	orgInviteCmd.AddCommand(orgInviteRevokeCmd)

	orgAccountCmd.AddCommand(orgAccountListCmd)
	orgAccountCmd.AddCommand(orgAccountDisableCmd)
	orgAccountCmd.AddCommand(orgAccountEnableCmd)
	orgAccountCmd.AddCommand(orgAccountRoleCmd)

	orgAccountDisableCmd.Flags().Bool("remove", false, "Permanently remove the account from the organization (cannot be re-enabled)")

	orgListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction")
	orgListCmd.Flags().Int("page", 1, "Page number")
	orgListCmd.Flags().Int("per-page", 30, "Items per page")
	orgListCmd.Flags().String("name", "", "Filter by name (regex)")
	orgListCmd.Flags().String("role", "", "Filter by role (RO, RW, SU)")
	orgListCmd.Flags().String("status", "", "Filter by status (ENABLED, DISABLED, INVITED, DECLINED)")

	orgCreateCmd.Flags().String("display-name", "", "Display name for the organization")
	orgCreateCmd.Flags().String("description", "", "Organization description")

	orgInviteSendCmd.Flags().String("role", "RO", "Role for the invitee (SU, RW, RO)")
}

func runOrgList(cmd *cobra.Command, args []string) error {
	requireAuth()

	opts := service.OrgListOpts{}
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.Role, _ = cmd.Flags().GetString("role")
	opts.Status, _ = cmd.Flags().GetString("status")

	result, err := svc.OrgList(context.Background(), opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatOrganizations(result.Orgs); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runOrgCreate(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]
	displayName, _ := cmd.Flags().GetString("display-name")
	description, _ := cmd.Flags().GetString("description")

	ctx := context.Background()

	// If config has no default org and the user has no orgs yet, auto-set
	// the new org as default after a successful create.
	currentDefault := config.Get().Organization.Name
	shouldAutoSetDefault := false
	if currentDefault == "" {
		listing, err := svc.OrgList(ctx, service.OrgListOpts{PerPage: 1})
		if err == nil {
			shouldAutoSetDefault = len(listing.Orgs) == 0
		}
	}

	org, err := svc.OrgCreate(ctx, name, displayName, description)
	if err != nil {
		if api.IsRegistrationRestrictedError(err) {
			color.Red("✗ Registration restricted")
			fmt.Println()
			fmt.Println("Registration is currently restricted to invited users.")
			fmt.Println("Please contact an administrator to request access.")
			return nil
		}
		return err
	}

	if shouldAutoSetDefault {
		if err := config.UpdateValue("organization.name", name); err != nil {
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
	if err := svc.OrgDelete(context.Background(), name); err != nil {
		return err
	}
	color.Green("✓ Organization deleted: %s", name)
	return nil
}

func runOrgDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org, err := svc.OrgGet(context.Background(), args[0])
	if err != nil {
		return err
	}
	return formatter.FormatOrganization(org)
}

func runOrgQuota(cmd *cobra.Command, args []string) error {
	requireAuth()
	q, err := svc.OrgQuota(context.Background(), args[0])
	if err != nil {
		return err
	}
	return formatter.FormatOrgQuota(q)
}

func runOrgSetDefaultOU(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	ouName := args[0]
	if err := svc.OrgSetDefaultOU(context.Background(), org, ouName); err != nil {
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
	if err := svc.OrgInviteSend(context.Background(), org, email, role); err != nil {
		return err
	}
	color.Green("✓ Invitation sent to: %s", email)
	return nil
}

func runOrgInviteList(cmd *cobra.Command, args []string) error {
	requireAuth()
	result, err := svc.OrgInviteList(context.Background())
	if err != nil {
		return err
	}
	return formatter.FormatInvites(result)
}

func runOrgInviteAccept(cmd *cobra.Command, args []string) error {
	requireAuth()
	orgName := args[0]
	if err := svc.OrgInviteAccept(context.Background(), orgName); err != nil {
		return err
	}
	color.Green("✓ Invitation to %s accepted", orgName)
	return nil
}

func runOrgInviteDecline(cmd *cobra.Command, args []string) error {
	requireAuth()
	orgName := args[0]
	if err := svc.OrgInviteDecline(context.Background(), orgName); err != nil {
		return err
	}
	color.Green("✓ Invitation to %s declined", orgName)
	return nil
}

func runOrgInviteRevoke(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	email := args[0]
	if err := svc.OrgInviteRevoke(context.Background(), org, email); err != nil {
		return err
	}
	color.Green("✓ Invitation revoked for: %s", email)
	return nil
}

func runOrgAccountList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	result, err := svc.OrgAccountList(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatAccounts(result.Accounts, result.Quota)
}

func runOrgAccountDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	email := args[0]
	remove, _ := cmd.Flags().GetBool("remove")

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

	if err := svc.OrgAccountDisable(context.Background(), org, email, remove); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			if api.IsNotFoundError(apiErr) {
				return fmt.Errorf("account '%s' not found in organization '%s'", email, org)
			}
			if strings.Contains(apiErr.Error(), "not ENABLED") {
				return fmt.Errorf("account '%s' is already disabled", email)
			}
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
	if err := svc.OrgAccountEnable(context.Background(), org, email); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			if api.IsNotFoundError(apiErr) {
				return fmt.Errorf("account '%s' not found in organization '%s'", email, org)
			}
			if strings.Contains(apiErr.Error(), "not DISABLED") {
				return fmt.Errorf("account '%s' is already enabled", email)
			}
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
	if err := svc.OrgAccountSetRole(context.Background(), org, email, args[1]); err != nil {
		return err
	}
	// Echo the canonical form (service has already normalised it).
	canonical, _ := service.NormalizeRole(args[1])
	color.Green("✓ Role set to %s for: %s", canonical, email)
	return nil
}
