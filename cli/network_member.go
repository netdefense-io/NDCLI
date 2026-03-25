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

var networkMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage VPN network members",
}

var networkMemberListCmd = &cobra.Command{
	Use:               "list [network]",
	Short:             "List VPN network members",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeVpnNetworks,
	RunE:              runNetworkMemberList,
}

var networkMemberAddCmd = &cobra.Command{
	Use:               "add [network] [device]",
	Short:             "Add a device as VPN member",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeVpnNetworkThenDevice,
	RunE:              runNetworkMemberAdd,
}

var networkMemberDescribeCmd = &cobra.Command{
	Use:               "describe [network] [device]",
	Short:             "Show VPN member details",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeVpnNetworkThenDevice,
	RunE:              runNetworkMemberDescribe,
}

var networkMemberUpdateCmd = &cobra.Command{
	Use:               "update [network] [device]",
	Short:             "Update a VPN member",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeVpnNetworkThenDevice,
	RunE:              runNetworkMemberUpdate,
}

var networkMemberRemoveCmd = &cobra.Command{
	Use:               "remove [network] [device]",
	Short:             "Remove a VPN member",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeVpnNetworkThenDevice,
	RunE:              runNetworkMemberRemove,
}

func init() {
	// List flags
	networkMemberListCmd.Flags().Int("page", 1, "Page number")
	networkMemberListCmd.Flags().Int("per-page", 30, "Items per page")

	// Add flags
	networkMemberAddCmd.Flags().String("role", "SPOKE", "Member role: HUB or SPOKE")
	networkMemberAddCmd.Flags().Bool("enabled", true, "Enable the member")
	networkMemberAddCmd.Flags().String("overlay-ip", "", "Overlay IPv4 address (auto-allocated if empty)")
	networkMemberAddCmd.Flags().String("endpoint-host", "", "Public hostname/IP")
	networkMemberAddCmd.Flags().Int("endpoint-port", 0, "Public endpoint port")
	networkMemberAddCmd.Flags().Int("listen-port", 0, "WireGuard listen port override")
	networkMemberAddCmd.Flags().Int("mtu", 0, "MTU override")
	networkMemberAddCmd.Flags().Int("keepalive", 0, "Keepalive interval override")
	networkMemberAddCmd.Flags().String("transit-via-hub", "", "Device name of HUB to route through")

	// Update flags
	networkMemberUpdateCmd.Flags().String("role", "", "Member role: HUB or SPOKE")
	networkMemberUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the member")
	networkMemberUpdateCmd.Flags().String("endpoint-host", "", "Public hostname/IP (\"none\" to clear)")
	networkMemberUpdateCmd.Flags().Int("endpoint-port", 0, "Public endpoint port (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("listen-port", 0, "WireGuard listen port override (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("mtu", 0, "MTU override (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("keepalive", 0, "Keepalive interval override (0 to clear)")
	networkMemberUpdateCmd.Flags().String("transit-via-hub", "", "Device name of HUB to route through (\"none\" to clear)")
	networkMemberUpdateCmd.Flags().Bool("regenerate-keys", false, "Regenerate WireGuard keypair")

	// Remove flags
	networkMemberRemoveCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkMemberList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members",
		url.PathEscape(org), url.PathEscape(vpnName)), params)
	if err != nil {
		return err
	}

	var result models.VpnMemberListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatVpnMembers(result.Items, result.Total); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runNetworkMemberAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]

	payload := map[string]interface{}{
		"device_name": deviceName,
	}

	if cmd.Flags().Changed("role") {
		v, _ := cmd.Flags().GetString("role")
		payload["role"] = v
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		payload["enabled"] = v
	}
	if cmd.Flags().Changed("overlay-ip") {
		v, _ := cmd.Flags().GetString("overlay-ip")
		if v != "" {
			payload["overlay_ip_v4"] = v
		}
	}
	if cmd.Flags().Changed("endpoint-host") {
		v, _ := cmd.Flags().GetString("endpoint-host")
		if v != "" {
			payload["endpoint_host"] = v
		}
	}
	if cmd.Flags().Changed("endpoint-port") {
		v, _ := cmd.Flags().GetInt("endpoint-port")
		if v > 0 {
			payload["endpoint_port"] = v
		}
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		if v > 0 {
			payload["listen_port"] = v
		}
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		if v > 0 {
			payload["mtu"] = v
		}
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		if v > 0 {
			payload["keepalive"] = v
		}
	}
	if cmd.Flags().Changed("transit-via-hub") {
		v, _ := cmd.Flags().GetString("transit-via-hub")
		if v != "" {
			payload["transit_via_hub"] = v
		}
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members",
		url.PathEscape(org), url.PathEscape(vpnName)), payload)
	if err != nil {
		return err
	}

	var member models.VpnMember
	if err := api.ParseResponse(resp, &member); err != nil {
		return err
	}

	color.Green("✓ Member added to %s: %s (%s, %s)", vpnName, deviceName, member.Role, member.OverlayIPv4)

	// Show connectivity info
	network, errN := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
	members, errM := vpn.FetchAllMembers(ctx, apiClient, org, vpnName)
	if errN == nil && errM == nil {
		hubCount, spokeCount := countMemberRoles(members)
		role := strings.ToUpper(member.Role)
		if role == "SPOKE" {
			fmt.Printf("  Auto-connected to %d hub(s)\n", hubCount)
		} else if role == "HUB" {
			if network.AutoConnectHubs {
				otherHubs := hubCount - 1
				if otherHubs < 0 {
					otherHubs = 0
				}
				fmt.Printf("  Auto-connected to %d spoke(s) and %d hub(s)\n", spokeCount, otherHubs)
			} else {
				fmt.Printf("  Auto-connected to %d spoke(s)\n", spokeCount)
			}
		}
	}

	return nil
}

func runNetworkMemberDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), nil)
	if err != nil {
		return err
	}

	var member models.VpnMember
	if err := api.ParseResponse(resp, &member); err != nil {
		return err
	}

	return formatter.FormatVpnMember(&member)
}

func runNetworkMemberUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	payload := make(map[string]interface{})

	if cmd.Flags().Changed("role") {
		v, _ := cmd.Flags().GetString("role")
		payload["role"] = v
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		payload["enabled"] = v
	}
	if cmd.Flags().Changed("endpoint-host") {
		v, _ := cmd.Flags().GetString("endpoint-host")
		if v == "none" || v == "" {
			payload["endpoint_host"] = nil
		} else {
			payload["endpoint_host"] = v
		}
	}
	if cmd.Flags().Changed("endpoint-port") {
		v, _ := cmd.Flags().GetInt("endpoint-port")
		if v == 0 {
			payload["endpoint_port"] = nil
		} else {
			payload["endpoint_port"] = v
		}
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		if v == 0 {
			payload["listen_port"] = nil
		} else {
			payload["listen_port"] = v
		}
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		if v == 0 {
			payload["mtu"] = nil
		} else {
			payload["mtu"] = v
		}
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		if v == 0 {
			payload["keepalive"] = nil
		} else {
			payload["keepalive"] = v
		}
	}
	if cmd.Flags().Changed("transit-via-hub") {
		v, _ := cmd.Flags().GetString("transit-via-hub")
		if v == "none" || v == "" {
			payload["transit_via_hub"] = nil
		} else {
			payload["transit_via_hub"] = v
		}
	}
	if cmd.Flags().Changed("regenerate-keys") {
		v, _ := cmd.Flags().GetBool("regenerate-keys")
		payload["regenerate_keys"] = v
	}

	if len(payload) == 0 {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()
	resp, err := apiClient.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), payload)
	if err != nil {
		return err
	}

	var member models.VpnMember
	if err := api.ParseResponse(resp, &member); err != nil {
		return err
	}

	color.Green("✓ Member updated: %s in %s", deviceName, vpnName)
	return nil
}

func runNetworkMemberRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]

	ctx := context.Background()

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		// Fetch member to get role for impact warning
		member, errM := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceName)
		if errM == nil && strings.EqualFold(member.Role, "HUB") {
			members, errAll := vpn.FetchAllMembers(ctx, apiClient, org, vpnName)
			if errAll == nil {
				_, spokeCount := countMemberRoles(members)
				if spokeCount > 0 {
					prompt := fmt.Sprintf("Remove hub '%s' from VPN '%s'?\n  %d spoke(s) will lose their connection to this hub.",
						deviceName, vpnName, spokeCount)
					if !helpers.Confirm(prompt) {
						fmt.Println("Cancelled")
						return nil
					}
				} else {
					if !helpers.Confirm(fmt.Sprintf("Remove hub '%s' from VPN '%s'?", deviceName, vpnName)) {
						fmt.Println("Cancelled")
						return nil
					}
				}
			} else {
				if !helpers.Confirm(fmt.Sprintf("Remove '%s' from VPN '%s'?", deviceName, vpnName)) {
					fmt.Println("Cancelled")
					return nil
				}
			}
		} else {
			if !helpers.Confirm(fmt.Sprintf("Remove '%s' from VPN '%s'?", deviceName, vpnName)) {
				fmt.Println("Cancelled")
				return nil
			}
		}
	}

	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)))
	if err != nil {
		return err
	}

	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	color.Green("✓ Member removed from %s: %s", vpnName, deviceName)
	return nil
}

func countMemberRoles(members []models.VpnMember) (hubCount, spokeCount int) {
	for _, m := range members {
		if strings.EqualFold(m.Role, "HUB") {
			hubCount++
		} else {
			spokeCount++
		}
	}
	return
}
