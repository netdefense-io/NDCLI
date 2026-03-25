package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
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
	// List flags
	networkPrefixListCmd.Flags().Int("page", 1, "Page number")
	networkPrefixListCmd.Flags().Int("per-page", 30, "Items per page")

	// Add flags
	networkPrefixAddCmd.Flags().Bool("publish", true, "Whether to advertise the prefix to peers")

	// Update flags
	networkPrefixUpdateCmd.Flags().Bool("publish", true, "Whether to advertise the prefix to peers")
	networkPrefixUpdateCmd.MarkFlagRequired("publish")

	// Remove flags
	networkPrefixRemoveCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkPrefixList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), params)
	if err != nil {
		return err
	}

	var result models.VpnMemberPrefixListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatVpnPrefixes(result.Items, result.Total); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runNetworkPrefixAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	variableName := args[2]

	payload := map[string]interface{}{
		"variable_name": variableName,
	}

	if cmd.Flags().Changed("publish") {
		v, _ := cmd.Flags().GetBool("publish")
		payload["publish"] = v
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), payload)
	if err != nil {
		return err
	}

	var prefix models.VpnMemberPrefix
	if err := api.ParseResponse(resp, &prefix); err != nil {
		return err
	}

	color.Green("✓ Prefix added: %s on %s in %s", variableName, deviceName, vpnName)
	return nil
}

func runNetworkPrefixUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	variableName := args[2]

	payload := make(map[string]interface{})

	if cmd.Flags().Changed("publish") {
		v, _ := cmd.Flags().GetBool("publish")
		payload["publish"] = v
	}

	if len(payload) == 0 {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()
	resp, err := apiClient.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName), url.PathEscape(variableName)), payload)
	if err != nil {
		return err
	}

	var prefix models.VpnMemberPrefix
	if err := api.ParseResponse(resp, &prefix); err != nil {
		return err
	}

	color.Green("✓ Prefix updated: %s on %s in %s", variableName, deviceName, vpnName)
	return nil
}

func runNetworkPrefixRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	variableName := args[2]

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		if !helpers.Confirm(fmt.Sprintf("Remove prefix '%s' from '%s' in VPN '%s'?", variableName, deviceName, vpnName)) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName), url.PathEscape(variableName)))
	if err != nil {
		return err
	}

	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	color.Green("✓ Prefix removed: %s from %s in %s", variableName, deviceName, vpnName)
	return nil
}
