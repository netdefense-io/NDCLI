package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
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
	networkCreateCmd.Flags().Bool("auto-firewall-rules", false, "Auto-generate OPNsense pass rules on the wireguard interface group so peers can reach each other's published subnets")
	networkCreateCmd.Flags().Int("listen-port", 0, "Default WireGuard listen port (default: 51820)")
	networkCreateCmd.Flags().Int("mtu", 0, "Default MTU (1280-9000)")
	networkCreateCmd.Flags().Int("keepalive", 0, "Default keepalive interval (1-65535)")

	// Update flags
	networkUpdateCmd.Flags().String("name", "", "New network name")
	networkUpdateCmd.Flags().Bool("auto-connect-hubs", false, "Auto-create links between HUB members")
	networkUpdateCmd.Flags().Bool("auto-firewall-rules", false, "Toggle auto-generated pass rules on the wireguard interface group")
	networkUpdateCmd.Flags().Int("listen-port", 0, "Default WireGuard listen port")
	networkUpdateCmd.Flags().Int("mtu", 0, "Default MTU (0 to clear)")
	networkUpdateCmd.Flags().Int("keepalive", 0, "Default keepalive interval (0 to clear)")
	networkUpdateCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")

	// Delete flags
	networkDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

// boolPtr / intPtr / stringPtr helpers — local to cli/ for building service
// option structs from cobra flags.
func boolPtr(v bool) *bool       { return &v }
func intPtr(v int) *int          { return &v }
func stringPtr(v string) *string { return &v }

func runNetworkList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	result, err := svc.NetworkList(context.Background(), org, page, perPage)
	if err != nil {
		return err
	}
	if err := formatter.FormatVpnNetworks(result.Networks, result.Total, result.Quota); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runNetworkCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	cidr, _ := cmd.Flags().GetString("cidr")

	opts := service.NetworkCreateOpts{Name: name, OverlayCIDRv4: cidr}
	if cmd.Flags().Changed("auto-connect-hubs") {
		v, _ := cmd.Flags().GetBool("auto-connect-hubs")
		opts.AutoConnectHubs = &v
	}
	if cmd.Flags().Changed("auto-firewall-rules") {
		v, _ := cmd.Flags().GetBool("auto-firewall-rules")
		opts.AutoFirewallRules = &v
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		opts.ListenPortDefault = &v
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		opts.MTUDefault = &v
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		opts.KeepaliveDefault = &v
	}

	if _, err := svc.NetworkCreate(context.Background(), org, opts); err != nil {
		return err
	}
	color.Green("✓ VPN network created: %s", name)
	return nil
}

func runNetworkDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	n, err := svc.NetworkGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatVpnNetwork(n)
}

func runNetworkUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	opts := service.NetworkUpdateOpts{}

	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		opts.Name = &v
	}
	if cmd.Flags().Changed("auto-connect-hubs") {
		v, _ := cmd.Flags().GetBool("auto-connect-hubs")
		opts.AutoConnectHubs = &v
	}
	if cmd.Flags().Changed("auto-firewall-rules") {
		v, _ := cmd.Flags().GetBool("auto-firewall-rules")
		opts.AutoFirewallRules = &v
	}
	if cmd.Flags().Changed("listen-port") {
		v, _ := cmd.Flags().GetInt("listen-port")
		opts.ListenPortDefault = &v
	}
	if cmd.Flags().Changed("mtu") {
		v, _ := cmd.Flags().GetInt("mtu")
		opts.MTUDefault = &v
	}
	if cmd.Flags().Changed("keepalive") {
		v, _ := cmd.Flags().GetInt("keepalive")
		opts.KeepaliveDefault = &v
	}

	if opts == (service.NetworkUpdateOpts{}) {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()

	// Confirm auto-connect-hubs toggle if it actually changes the state.
	if opts.AutoConnectHubs != nil {
		skipConfirm, _ := cmd.Flags().GetBool("yes")
		if !skipConfirm {
			current, err := svc.NetworkGet(ctx, org, vpnName)
			if err != nil {
				return err
			}
			if *opts.AutoConnectHubs != current.AutoConnectHubs {
				members, err := svc.NetworkMemberList(ctx, org, vpnName, 1, 500)
				if err != nil {
					return err
				}
				hubCount := 0
				for _, m := range members.Members {
					if strings.EqualFold(m.Role, "HUB") {
						hubCount++
					}
				}
				if hubCount > 1 {
					var prompt string
					if *opts.AutoConnectHubs {
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

	n, err := svc.NetworkUpdate(ctx, org, vpnName, opts)
	if err != nil {
		return err
	}
	color.Green("✓ VPN network updated: %s", n.Name)
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
	if err := svc.NetworkDelete(context.Background(), org, vpnName); err != nil {
		return err
	}
	color.Green("✓ VPN network deleted: %s", vpnName)
	return nil
}
