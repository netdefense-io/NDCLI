package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/vpn"
)

var networkCmd = &cobra.Command{
	Use:     "network",
	Aliases: []string{"net"},
	Short:   "VPN network management commands",
}

var networkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VPN networks",
	RunE:  runNetworkList,
}

var networkCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a VPN network",
	Args:  cobra.ExactArgs(1),
	RunE:  runNetworkCreate,
}

var networkDescribeCmd = &cobra.Command{
	Use:               "describe [network]",
	Short:             "Show VPN network details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeVpnNetworks,
	RunE:              runNetworkDescribe,
}

var networkUpdateCmd = &cobra.Command{
	Use:               "update [network]",
	Short:             "Update a VPN network",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeVpnNetworks,
	RunE:              runNetworkUpdate,
}

var networkDeleteCmd = &cobra.Command{
	Use:               "delete [network]",
	Short:             "Delete a VPN network",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeVpnNetworks,
	RunE:              runNetworkDelete,
}

func init() {
	// Network CRUD
	networkCmd.AddCommand(networkListCmd)
	networkCmd.AddCommand(networkCreateCmd)
	networkCmd.AddCommand(networkDescribeCmd)
	networkCmd.AddCommand(networkUpdateCmd)
	networkCmd.AddCommand(networkDeleteCmd)

	// Member subcommands
	networkCmd.AddCommand(networkMemberCmd)
	networkMemberCmd.AddCommand(networkMemberListCmd)
	networkMemberCmd.AddCommand(networkMemberAddCmd)
	networkMemberCmd.AddCommand(networkMemberDescribeCmd)
	networkMemberCmd.AddCommand(networkMemberUpdateCmd)
	networkMemberCmd.AddCommand(networkMemberRemoveCmd)

	// Link subcommands
	networkCmd.AddCommand(networkLinkCmd)
	networkLinkCmd.AddCommand(networkLinkListCmd)
	networkLinkCmd.AddCommand(networkLinkCreateCmd)
	networkLinkCmd.AddCommand(networkLinkDescribeCmd)
	networkLinkCmd.AddCommand(networkLinkUpdateCmd)
	networkLinkCmd.AddCommand(networkLinkDeleteCmd)

	// Prefix subcommands
	networkCmd.AddCommand(networkPrefixCmd)
	networkPrefixCmd.AddCommand(networkPrefixListCmd)
	networkPrefixCmd.AddCommand(networkPrefixAddCmd)
	networkPrefixCmd.AddCommand(networkPrefixUpdateCmd)
	networkPrefixCmd.AddCommand(networkPrefixRemoveCmd)

	// List flags
	networkListCmd.Flags().Int("page", 1, "Page number")
	networkListCmd.Flags().Int("per-page", 30, "Items per page")

	// Create flags
	networkCreateCmd.Flags().String("cidr", "", "Overlay CIDR v4 (required, e.g. 10.100.0.0/24)")
	networkCreateCmd.MarkFlagRequired("cidr")
	networkCreateCmd.Flags().Bool("auto-connect-hubs", false, "Auto-create links between HUB members")
	networkCreateCmd.Flags().Int("listen-port", 0, "Default WireGuard listen port (default: 51820)")
	networkCreateCmd.Flags().Int("mtu", 0, "Default MTU (1280-9000)")
	networkCreateCmd.Flags().Int("keepalive", 0, "Default keepalive interval (1-65535)")

	// Update flags
	networkUpdateCmd.Flags().String("name", "", "New network name")
	networkUpdateCmd.Flags().Bool("auto-connect-hubs", false, "Auto-create links between HUB members")
	networkUpdateCmd.Flags().Int("listen-port", 0, "Default WireGuard listen port")
	networkUpdateCmd.Flags().Int("mtu", 0, "Default MTU (0 to clear)")
	networkUpdateCmd.Flags().Int("keepalive", 0, "Default keepalive interval (0 to clear)")
	networkUpdateCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")

	// Delete flags
	networkDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks", url.PathEscape(org)), params)
	if err != nil {
		return err
	}

	var result models.VpnNetworkListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatVpnNetworks(result.Items, result.Total, result.Quota); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runNetworkCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	cidr, _ := cmd.Flags().GetString("cidr")

	payload := map[string]interface{}{
		"name":            name,
		"overlay_cidr_v4": cidr,
	}

	if cmd.Flags().Changed("auto-connect-hubs") {
		v, _ := cmd.Flags().GetBool("auto-connect-hubs")
		payload["auto_connect_hubs"] = v
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		payload["listen_port_default"] = v
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		payload["mtu_default"] = v
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		payload["keepalive_default"] = v
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks", url.PathEscape(org)), payload)
	if err != nil {
		return err
	}

	var network models.VpnNetwork
	if err := api.ParseResponse(resp, &network); err != nil {
		return err
	}

	color.Green("✓ VPN network created: %s", name)
	return nil
}

func runNetworkDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(vpnName)), nil)
	if err != nil {
		return err
	}

	var network models.VpnNetwork
	if err := api.ParseResponse(resp, &network); err != nil {
		return err
	}

	return formatter.FormatVpnNetwork(&network)
}

func runNetworkUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	payload := make(map[string]interface{})

	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		payload["name"] = v
	}
	if cmd.Flags().Changed("auto-connect-hubs") {
		v, _ := cmd.Flags().GetBool("auto-connect-hubs")
		payload["auto_connect_hubs"] = v
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		payload["listen_port_default"] = v
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		if v == 0 {
			payload["mtu_default"] = nil
		} else {
			payload["mtu_default"] = v
		}
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		if v == 0 {
			payload["keepalive_default"] = nil
		} else {
			payload["keepalive_default"] = v
		}
	}

	if len(payload) == 0 {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()

	// Confirm auto-connect-hubs toggle if it's being changed
	if cmd.Flags().Changed("auto-connect-hubs") {
		skipConfirm, _ := cmd.Flags().GetBool("yes")
		if !skipConfirm {
			newValue, _ := cmd.Flags().GetBool("auto-connect-hubs")

			// Fetch current network to compare
			currentNetwork, err := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
			if err != nil {
				return err
			}

			if newValue != currentNetwork.AutoConnectHubs {
				// Count hubs
				members, err := vpn.FetchAllMembers(ctx, apiClient, org, vpnName)
				if err != nil {
					return err
				}
				hubCount := 0
				for _, m := range members {
					if strings.EqualFold(m.Role, "HUB") {
						hubCount++
					}
				}

				if hubCount > 1 {
					var prompt string
					if newValue {
						prompt = fmt.Sprintf("Enable auto-connect-hubs? All %d hub(s) will be automatically connected.", hubCount)
					} else {
						prompt = "Disable auto-connect-hubs? Hub-to-hub connections without manual links will be lost."
					}
					if !helpers.Confirm(prompt) {
						fmt.Println("Cancelled")
						return nil
					}
				}
			}
		}
	}

	resp, err := apiClient.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(vpnName)), payload)
	if err != nil {
		return err
	}

	var network models.VpnNetwork
	if err := api.ParseResponse(resp, &network); err != nil {
		return err
	}

	color.Green("✓ VPN network updated: %s", network.Name)
	return nil
}

func runNetworkDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		if !helpers.Confirm(fmt.Sprintf("Delete VPN network '%s'?", vpnName)) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(vpnName)))
	if err != nil {
		return err
	}

	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	color.Green("✓ VPN network deleted: %s", vpnName)
	return nil
}
