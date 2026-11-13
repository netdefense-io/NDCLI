package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
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
	Args:  noArgs,
	RunE:  runOrgList,
}

var orgCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new organization",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgCreate,
}

var orgDeleteCmd = &cobra.Command{
	Use:               "delete <name>",
	Short:             "Delete an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgDelete,
}

var orgDescribeCmd = &cobra.Command{
	Use:               "describe <name>",
	Short:             "Show organization details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgDescribe,
}

var orgSetDefaultOUCmd = &cobra.Command{
	Use:               "set-default-ou <ou-name>",
	Short:             "Set the default OU for an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOrgSetDefaultOU,
}

var orgUnverifiedReposCmd = &cobra.Command{
	Use:   "unverified-repos <allow|forbid>",
	Short: "Allow or forbid unsigned and non-https repositories in software policies",
	Long: `Allow or forbid the organization's software policies (-o) to declare package
repositories with signature type none or a non-https URL.

  allow    policies may declare unsigned or non-https repositories, which can
           install anything on every device the policy reaches
  forbid   every repository must be signed and use https; devices stop
           installing from the others at their next sync

Only a superuser of the organization may change it, from an interactive login:
the server refuses a personal access token. Both ask first; -y skips the
question. 'ndcli org describe <org>' shows the current setting.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"allow", "forbid"},
	RunE:      runOrgUnverifiedRepos,
}

var orgInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "Manage organization invitations",
}

var orgInviteSendCmd = &cobra.Command{
	Use:   "send <email>",
	Short: "Send an organization invitation",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgInviteSend,
}

var orgInviteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending invitations",
	Args:  noArgs,
	RunE:  runOrgInviteList,
}

var orgInviteAcceptCmd = &cobra.Command{
	Use:               "accept <org-name>",
	Short:             "Accept an invitation to join an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgInviteAccept,
}

var orgInviteDeclineCmd = &cobra.Command{
	Use:               "decline <org-name>",
	Short:             "Decline an invitation to join an organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runOrgInviteDecline,
}

var orgInviteRevokeCmd = &cobra.Command{
	Use:   "revoke <email>",
	Short: "Revoke a pending invitation (requires superuser)",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgInviteRevoke,
}

var orgQuotaCmd = &cobra.Command{
	Use:               "quota <org>",
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
	Args:  noArgs,
	RunE:  runOrgAccountList,
}

var orgAccountDisableCmd = &cobra.Command{
	Use:               "disable <email>",
	Short:             "Disable an account",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeAccountEmails,
	RunE:              runOrgAccountDisable,
}

var orgAccountEnableCmd = &cobra.Command{
	Use:               "enable <email>",
	Short:             "Enable an account",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeAccountEmails,
	RunE:              runOrgAccountEnable,
}

var orgAccountRoleCmd = &cobra.Command{
	Use:   "role <email> <role>",
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
	addYesFlag(orgDeleteCmd)
	orgCmd.AddCommand(orgDescribeCmd)
	orgCmd.AddCommand(orgSetDefaultOUCmd)
	orgCmd.AddCommand(orgUnverifiedReposCmd)
	addYesFlag(orgUnverifiedReposCmd)
	orgCmd.AddCommand(orgQuotaCmd)
	orgCmd.AddCommand(orgInviteCmd)
	orgCmd.AddCommand(orgAccountCmd)

	orgInviteCmd.AddCommand(orgInviteSendCmd)
	orgInviteCmd.AddCommand(orgInviteListCmd)
	orgInviteCmd.AddCommand(orgInviteAcceptCmd)
	orgInviteCmd.AddCommand(orgInviteDeclineCmd)
	orgInviteCmd.AddCommand(orgInviteRevokeCmd)
	addYesFlag(orgInviteRevokeCmd)

	orgAccountCmd.AddCommand(orgAccountListCmd)
	orgAccountCmd.AddCommand(orgAccountDisableCmd)
	orgAccountCmd.AddCommand(orgAccountEnableCmd)
	orgAccountCmd.AddCommand(orgAccountRoleCmd)

	addYesFlag(orgAccountDisableCmd)
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
	if err := formatter.FormatOrganizations(result.Orgs, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

// registrationRestrictedMessage replaces NDManager's terse refusal of an org
// create while registration is closed.
const registrationRestrictedMessage = "Registration is currently restricted to invited users. " +
	"Please contact an administrator to request access."

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
			// The organization was not created, so this is an error: it exits
			// non-zero, and printCommandError renders it with the code the
			// wrapped API error still carries.
			return &service.Error{Code: service.CodeAPIError, Message: registrationRestrictedMessage, Err: err}
		}
		return err
	}

	madeDefault := false
	if shouldAutoSetDefault {
		if err := config.UpdateValue("organization.name", name); err != nil {
			_, _ = color.New(color.FgYellow).Fprintf(os.Stderr, "⚠ Could not set as default organization: %v\n", err)
		} else {
			madeDefault = true
		}
	}
	return formatter.FormatActionResult(orgCreatedResult(name, org, madeDefault))
}

// orgCreatedResult is org create's report: the organization and its agent
// token, and the hint to make it the default unless the command just did.
func orgCreatedResult(name string, org *models.Organization, madeDefault bool) *models.ActionResult {
	result := &models.ActionResult{Resource: "organization", Name: name, Value: org, Action: "created",
		Details: map[string]interface{}{"agent_token": org.Token, "token_warning": models.AgentTokenWarning, "default": madeDefault},
		Message: fmt.Sprintf("✓ Organization created: %s", name),
		Notes: []string{
			"  Token: " + org.Token,
			"",
			"Use this token to register devices with the NetDefense agent.",
			"To retrieve the token later: ndcli org describe " + name,
		}}
	if madeDefault {
		result.Message = fmt.Sprintf("✓ Organization created and set as default: %s", name)
		return result
	}
	result.Notes = append(result.Notes, "", "To set this organization as your default:", "  ndcli config set org "+name)
	return result
}

func runOrgDelete(cmd *cobra.Command, args []string) error {
	requireAuth()

	name := args[0]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Delete organization '%s'? This action cannot be undone.", name)); !ok {
		return err
	}
	if err := svc.OrgDelete(context.Background(), name); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: name, Action: "deleted",
		Details: map[string]interface{}{"name": name}, Message: fmt.Sprintf("✓ Organization deleted: %s", name)})
}

func runOrgDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org, err := svc.OrgDescribe(context.Background(), args[0])
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
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "default_ou_set",
		Details: map[string]interface{}{"default_ou": ouName}, Message: fmt.Sprintf("✓ Default OU set to: %s", ouName)})
}

func runOrgUnverifiedRepos(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	var allow bool
	var prompt string
	switch args[0] {
	case "allow":
		allow, prompt = true, fmt.Sprintf("Let software policies in '%s' install from unsigned or non-https repositories?", org)
	case "forbid":
		prompt = fmt.Sprintf("Stop devices in '%s' from installing from unsigned or non-https repositories?", org)
	default:
		return &service.Error{Code: service.CodeInvalidInput, Message: fmt.Sprintf("%q is neither allow nor forbid", args[0])}
	}
	if ok, err := confirmUnlessYes(cmd, prompt); !ok {
		return err
	}
	if err := svc.OrgSetUnverifiedRepos(context.Background(), org, allow); err != nil {
		return err
	}
	action := "forbidden"
	if allow {
		action = "allowed"
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: action,
		Details: map[string]interface{}{"allow_unverified_repos": allow},
		Message: fmt.Sprintf("✓ Unverified repositories %s in %s", action, org)})
}

func runOrgInviteSend(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	email := args[0]
	role, _ := cmd.Flags().GetString("role")
	if err := svc.OrgInviteSend(context.Background(), org, email, role); err != nil {
		return err
	}
	// Echo the role the service sent, not the spelling typed.
	canonical, _ := service.NormalizeRole(role)
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "invited",
		Details: map[string]interface{}{"email": email, "role": canonical}, Message: fmt.Sprintf("✓ Invitation sent to: %s", email)})
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
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: orgName, Action: "accepted",
		Message: fmt.Sprintf("✓ Invitation to %s accepted", orgName)})
}

func runOrgInviteDecline(cmd *cobra.Command, args []string) error {
	requireAuth()
	orgName := args[0]
	if err := svc.OrgInviteDecline(context.Background(), orgName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: orgName, Action: "declined",
		Message: fmt.Sprintf("✓ Invitation to %s declined", orgName)})
}

func runOrgInviteRevoke(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	email := args[0]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Revoke the invitation for '%s'?", email)); !ok {
		return err
	}
	if err := svc.OrgInviteRevoke(context.Background(), org, email); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "revoked",
		Details: map[string]interface{}{"email": email}, Message: fmt.Sprintf("✓ Invitation revoked for: %s", email)})
}

func runOrgAccountList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	result, err := svc.OrgAccountList(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatAccounts(result.Accounts, result.Total, result.Quota)
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
	if ok, err := confirmUnlessYes(cmd, confirmMsg); !ok {
		return err
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
	result := &models.ActionResult{Resource: "organization", Name: org, Action: "disabled",
		Details: map[string]interface{}{"email": email}, Message: fmt.Sprintf("✓ Account disabled: %s", email)}
	if remove {
		result.Action, result.Message = "removed", fmt.Sprintf("✓ Account removed from organization: %s", email)
	}
	return formatter.FormatActionResult(result)
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
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "enabled",
		Details: map[string]interface{}{"email": email}, Message: fmt.Sprintf("✓ Account enabled: %s", email)})
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
	return formatter.FormatActionResult(&models.ActionResult{Resource: "organization", Name: org, Action: "role_set",
		Details: map[string]interface{}{"email": email, "role": canonical}, Message: fmt.Sprintf("✓ Role set to %s for: %s", canonical, email)})
}
