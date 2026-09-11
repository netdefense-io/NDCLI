package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var networkPrefixCmd = &cobra.Command{
	Use:   "prefix",
	Short: "Manage VPN member prefixes",
}

var networkPrefixListCmd = &cobra.Command{
	Use:               "list [network] [device]",
	Short:             "List published prefixes for a VPN member",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeVpnNetworkThenDevice,
	RunE:              runNetworkPrefixList,
}

var networkPrefixAddCmd = &cobra.Command{
	Use:               "add [network] [device] [variable]",
	Short:             "Publish a prefix on a VPN member",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkDeviceVariable,
	RunE:              runNetworkPrefixAdd,
}

var networkPrefixUpdateCmd = &cobra.Command{
	Use:               "update [network] [device] [variable]",
	Short:             "Update a VPN member prefix",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkDeviceVariable,
	RunE:              runNetworkPrefixUpdate,
}

var networkPrefixRemoveCmd = &cobra.Command{
	Use:               "remove [network] [device] [variable]",
	Short:             "Remove a prefix from a VPN member",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkDeviceVariable,
	RunE:              runNetworkPrefixRemove,
}

func init() {
	networkPrefixListCmd.Flags().Int("page", 1, "Page number")
	networkPrefixListCmd.Flags().Int("per-page", 30, "Items per page")

	networkPrefixAddCmd.Flags().Bool("publish", true, "Whether to advertise the prefix to peers")
	// StringSlice rather than StringArray: the stored value is itself
	// comma-separated, so `--value a,b` has to read as two CIDRs, and
	// StringSlice accepts that as well as a repeated flag.
	networkPrefixAddCmd.Flags().StringSlice("value", nil,
		"CIDR block to publish; creates the variable and its device override (repeatable, or comma-separated)")

	networkPrefixUpdateCmd.Flags().Bool("publish", true, "Whether to advertise the prefix to peers")
	networkPrefixUpdateCmd.MarkFlagRequired("publish")

	networkPrefixRemoveCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkPrefixList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName, deviceName := args[0], args[1]
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	result, err := svc.NetworkPrefixList(context.Background(), org, vpnName, deviceName, page, perPage)
	if err != nil {
		return err
	}
	if err := formatter.FormatVpnPrefixes(result.Prefixes, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runNetworkPrefixAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName, deviceName, variableName := args[0], args[1], args[2]
	var publish *bool
	if cmd.Flags().Changed("publish") {
		v, _ := cmd.Flags().GetBool("publish")
		publish = &v
	}

	// With --value, create the variable chain first. Without it, behaviour is
	// exactly as before: the variable must already exist, and NDManager's own
	// errors name the commands to create it.
	if cidrs, _ := cmd.Flags().GetStringSlice("value"); len(cidrs) > 0 {
		result, err := svc.NetworkPrefixProvision(context.Background(), org, vpnName, deviceName, variableName, cidrs, publish)
		if err != nil {
			return namedVariableSetCommand(err)
		}
		// Only the prefix we actually read back can tell us the publish flag.
		// A nil Prefix means the prefix already existed and reading it back
		// failed, so the state stays unknown rather than being inferred from
		// what this invocation asked for.
		var published *bool
		if result.Prefix != nil {
			p := result.Prefix.Publish
			published = &p
		}
		return formatter.FormatVpnPrefixProvisioned(output.VpnPrefixProvision{
			Network:               vpnName,
			Device:                deviceName,
			Variable:              variableName,
			Value:                 result.Value,
			OrgVariableCreated:    result.OrgVariableCreated,
			DeviceVariableCreated: result.DeviceVariableCreated,
			PrefixCreated:         result.PrefixCreated,
			Published:             published,
		})
	}

	if _, err := svc.NetworkPrefixAdd(context.Background(), org, vpnName, deviceName, variableName, publish); err != nil {
		return err
	}
	color.Green("✓ Prefix added: %s on %s in %s", variableName, deviceName, vpnName)
	return nil
}

// namedVariableSetCommand fills in the CLI's own way forward on a
// variable-value mismatch. The service message is surface-neutral because MCP
// callers have no commands to run; here, naming the command is the useful
// advice.
func namedVariableSetCommand(err error) error {
	// errors.As walks the whole chain, so this reaches the cause whether the
	// mismatch is returned directly or wrapped in a provisioning error.
	var mismatch *service.VariableMismatchError
	if !errors.As(err, &mismatch) {
		return err
	}

	// The scope comes off the error as a field. Reading it back out of the
	// rendered sentence would break the moment anyone rewords the sentence,
	// with nothing failing at compile time.
	hint := fmt.Sprintf("change it with 'ndcli variable org set %s --value ...'", mismatch.Name)
	if mismatch.Scope == service.VarScopeDevice {
		hint = fmt.Sprintf("change it with 'ndcli variable device set %s %s --value ...'",
			mismatch.Entity, mismatch.Name)
	}
	return fmt.Errorf("%s", strings.Replace(err.Error(), service.VariableMismatchHint, hint, 1))
}

func runNetworkPrefixUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName, deviceName, variableName := args[0], args[1], args[2]
	if !cmd.Flags().Changed("publish") {
		return fmt.Errorf("no update flags specified")
	}
	v, _ := cmd.Flags().GetBool("publish")
	if _, err := svc.NetworkPrefixUpdate(context.Background(), org, vpnName, deviceName, variableName, &v); err != nil {
		return err
	}
	color.Green("✓ Prefix updated: %s on %s in %s", variableName, deviceName, vpnName)
	return nil
}

func runNetworkPrefixRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName, deviceName, variableName := args[0], args[1], args[2]

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		if !helpers.Confirm(fmt.Sprintf("Remove prefix '%s' from '%s' in VPN '%s'?", variableName, deviceName, vpnName)) {
			fmt.Println("Cancelled")
			return nil
		}
	}
	if err := svc.NetworkPrefixRemove(context.Background(), org, vpnName, deviceName, variableName); err != nil {
		return err
	}
	color.Green("✓ Prefix removed: %s from %s in %s", variableName, deviceName, vpnName)
	return nil
}
