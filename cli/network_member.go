package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
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
	networkMemberListCmd.Flags().Int("page", 1, "Page number")
	networkMemberListCmd.Flags().Int("per-page", 30, "Items per page")

	networkMemberAddCmd.Flags().String("role", "SPOKE", "Member role: HUB or SPOKE")
	networkMemberAddCmd.Flags().Bool("enabled", true, "Enable the member")
	networkMemberAddCmd.Flags().String("overlay-ip", "", "Overlay IPv4 address (auto-allocated if empty)")
	networkMemberAddCmd.Flags().String("endpoint-host", "", "Public hostname/IP")
	networkMemberAddCmd.Flags().Int("endpoint-port", 0, "Public endpoint port")
	networkMemberAddCmd.Flags().Int("listen-port", 0, "WireGuard listen port override")
	networkMemberAddCmd.Flags().Int("mtu", 0, "MTU override")
	networkMemberAddCmd.Flags().Int("keepalive", 0, "Keepalive interval override")
	networkMemberAddCmd.Flags().String("transit-via-hub", "", "Device name of HUB to route through")

	networkMemberUpdateCmd.Flags().String("role", "", "Member role: HUB or SPOKE")
	networkMemberUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the member")
	networkMemberUpdateCmd.Flags().String("endpoint-host", "", "Public hostname/IP (\"none\" to clear)")
	networkMemberUpdateCmd.Flags().Int("endpoint-port", 0, "Public endpoint port (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("listen-port", 0, "WireGuard listen port override (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("mtu", 0, "MTU override (0 to clear)")
	networkMemberUpdateCmd.Flags().Int("keepalive", 0, "Keepalive interval override (0 to clear)")
	networkMemberUpdateCmd.Flags().String("transit-via-hub", "", "Device name of HUB to route through (\"none\" to clear)")
	networkMemberUpdateCmd.Flags().Bool("regenerate-keys", false, "Regenerate WireGuard keypair")

	networkMemberRemoveCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkMemberList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	result, err := svc.NetworkMemberList(context.Background(), org, vpnName, page, perPage)
	if err != nil {
		return err
	}
	if err := formatter.FormatVpnMembers(result.Members, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runNetworkMemberAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]

	opts := service.NetworkMemberAddOpts{}
	if cmd.Flags().Changed("role") {
		opts.Role, _ = cmd.Flags().GetString("role")
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		opts.Enabled = &v
	}
	if cmd.Flags().Changed("overlay-ip") {
		opts.OverlayIPv4, _ = cmd.Flags().GetString("overlay-ip")
	}
	if cmd.Flags().Changed("endpoint-host") {
		opts.EndpointHost, _ = cmd.Flags().GetString("endpoint-host")
	}
	if cmd.Flags().Changed("endpoint-port") {
		opts.EndpointPort, _ = cmd.Flags().GetInt("endpoint-port")
	}
	if cmd.Flags().Changed("listen-port") {
		opts.ListenPort, _ = cmd.Flags().GetInt("listen-port")
	}
	if cmd.Flags().Changed("mtu") {
		opts.MTU, _ = cmd.Flags().GetInt("mtu")
	}
	if cmd.Flags().Changed("keepalive") {
		opts.Keepalive, _ = cmd.Flags().GetInt("keepalive")
	}
	if cmd.Flags().Changed("transit-via-hub") {
		opts.TransitViaHub, _ = cmd.Flags().GetString("transit-via-hub")
	}

	ctx := context.Background()
	member, err := svc.NetworkMemberAdd(ctx, org, vpnName, deviceName, opts)
	if err != nil {
		return err
	}

	color.Green("✓ Member added to %s: %s (%s, %s)", vpnName, deviceName, member.Role, member.OverlayIPv4)

	// Show connectivity info — requires a follow-up fetch.
	network, errN := svc.NetworkGet(ctx, org, vpnName)
	memberList, errM := svc.NetworkMemberList(ctx, org, vpnName, 1, 500)
	if errN == nil && errM == nil {
		hubCount, spokeCount := countMemberRoles(memberList.Members)
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
	member, err := svc.NetworkMemberGet(context.Background(), org, vpnName, deviceName)
	if err != nil {
		return err
	}
	return formatter.FormatVpnMember(member)
}

func runNetworkMemberUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceName := args[1]
	opts := service.NetworkMemberUpdateOpts{}

	if cmd.Flags().Changed("role") {
		v, _ := cmd.Flags().GetString("role")
		opts.Role = &v
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		opts.Enabled = &v
	}
	if cmd.Flags().Changed("endpoint-host") {
		v, _ := cmd.Flags().GetString("endpoint-host")
		// CLI sentinel "none" → empty string → service interprets as clear.
		if v == "none" {
			v = ""
		}
		opts.EndpointHost = &v
	}
	if cmd.Flags().Changed("endpoint-port") {
		v, _ := cmd.Flags().GetInt("endpoint-port")
		opts.EndpointPort = &v
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		opts.ListenPort = &v
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		opts.MTU = &v
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		opts.Keepalive = &v
	}
	if cmd.Flags().Changed("transit-via-hub") {
		v, _ := cmd.Flags().GetString("transit-via-hub")
		if v == "none" {
			v = ""
		}
		opts.TransitViaHub = &v
	}
	if cmd.Flags().Changed("regenerate-keys") {
		v, _ := cmd.Flags().GetBool("regenerate-keys")
		opts.RegenerateKeys = &v
	}

	if _, err := svc.NetworkMemberUpdate(context.Background(), org, vpnName, deviceName, opts); err != nil {
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
		// Hub removal warns about disconnected spokes.
		member, errM := svc.NetworkMemberGet(ctx, org, vpnName, deviceName)
		if errM == nil && strings.EqualFold(member.Role, "HUB") {
			members, errAll := svc.NetworkMemberList(ctx, org, vpnName, 1, 500)
			if errAll == nil {
				_, spokeCount := countMemberRoles(members.Members)
				if spokeCount > 0 {
					prompt := fmt.Sprintf("Remove hub '%s' from VPN '%s'?\n  %d spoke(s) will lose their connection to this hub.",
						deviceName, vpnName, spokeCount)
					if !helpers.Confirm(prompt) {
						fmt.Println("Cancelled")
						return nil
					}
				} else if !helpers.Confirm(fmt.Sprintf("Remove hub '%s' from VPN '%s'?", deviceName, vpnName)) {
					fmt.Println("Cancelled")
					return nil
				}
			} else if !helpers.Confirm(fmt.Sprintf("Remove '%s' from VPN '%s'?", deviceName, vpnName)) {
				fmt.Println("Cancelled")
				return nil
			}
		} else if !helpers.Confirm(fmt.Sprintf("Remove '%s' from VPN '%s'?", deviceName, vpnName)) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	if err := svc.NetworkMemberRemove(ctx, org, vpnName, deviceName); err != nil {
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
