package cli

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
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
	if _, err := svc.NetworkPrefixAdd(context.Background(), org, vpnName, deviceName, variableName, publish); err != nil {
		return err
	}
	color.Green("✓ Prefix added: %s on %s in %s", variableName, deviceName, vpnName)
	return nil
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
