package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/vpn"
)

var networkLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage VPN connections and link overrides",
}

var networkLinkListCmd = &cobra.Command{
	Use:   "list [network]",
	Short: "List VPN connections",
	Long: `Show all effective connections in a VPN network.

Connections are derived from member roles:
  hub↔spoke:    always connected automatically
  hub↔hub:      automatic if auto-connect-hubs is enabled
  spoke↔spoke:  requires a manual link

Use --raw to see VPN link database rows only.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeVpnNetworks,
	RunE:              runNetworkLinkList,
}

var networkLinkCreateCmd = &cobra.Command{
	Use:               "create [network] [device-a] [device-b]",
	Short:             "Create a VPN link between two members",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkThenTwoDevices,
	RunE:              runNetworkLinkCreate,
}

var networkLinkDescribeCmd = &cobra.Command{
	Use:               "describe [network] [device-a] [device-b]",
	Short:             "Show VPN connection details",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkThenTwoDevices,
	RunE:              runNetworkLinkDescribe,
}

var networkLinkUpdateCmd = &cobra.Command{
	Use:               "update [network] [device-a] [device-b]",
	Short:             "Update a VPN link",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkThenTwoDevices,
	RunE:              runNetworkLinkUpdate,
}

var networkLinkDeleteCmd = &cobra.Command{
	Use:               "delete [network] [device-a] [device-b]",
	Short:             "Delete a VPN link",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: completeVpnNetworkThenTwoDevices,
	RunE:              runNetworkLinkDelete,
}

func init() {
	networkLinkListCmd.Flags().Bool("raw", false, "Show raw VPN link database rows instead of effective connections")
	networkLinkListCmd.Flags().String("device", "", "Filter connections to those involving this device")
	networkLinkListCmd.Flags().Int("page", 1, "Page number (only with --raw)")
	networkLinkListCmd.Flags().Int("per-page", 30, "Items per page (only with --raw)")
	networkLinkListCmd.RegisterFlagCompletionFunc("device", completeVpnMemberDevices)

	networkLinkCreateCmd.Flags().Bool("enabled", true, "Enable the link")
	networkLinkCreateCmd.Flags().Bool("generate-psk", false, "Generate a WireGuard pre-shared key")

	networkLinkUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the link")
	networkLinkUpdateCmd.Flags().Bool("regenerate-psk", false, "Regenerate the pre-shared key")

	networkLinkDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkLinkList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	raw, _ := cmd.Flags().GetBool("raw")

	if raw {
		page, _ := cmd.Flags().GetInt("page")
		perPage, _ := cmd.Flags().GetInt("per-page")
		result, err := svc.NetworkLinkListRaw(context.Background(), org, vpnName, page, perPage)
		if err != nil {
			return err
		}
		if err := formatter.FormatVpnLinks(result.Links, result.Total); err != nil {
			return err
		}
		output.PrintPagination(result.Page, result.Total, result.PerPage)
		return nil
	}

	deviceFilter, _ := cmd.Flags().GetString("device")
	connections, err := svc.NetworkLinkListEffective(context.Background(), org, vpnName, deviceFilter)
	if err != nil {
		return err
	}

	if err := formatter.FormatVpnConnections(connections, len(connections)); err != nil {
		return err
	}

	// Print summary
	automatic, manualLinks, overrides := 0, 0, 0
	for _, c := range connections {
		if c.Source == "implicit" {
			automatic++
			if c.HasOverride {
				overrides++
			}
		} else {
			manualLinks++
		}
	}
	connWord := "connections"
	if len(connections) == 1 {
		connWord = "connection"
	}
	var parts []string
	if automatic > 0 {
		parts = append(parts, fmt.Sprintf("%d automatic", automatic))
	}
	if manualLinks > 0 {
		word := "manual links"
		if manualLinks == 1 {
			word = "manual link"
		}
		parts = append(parts, fmt.Sprintf("%d %s", manualLinks, word))
	}
	if overrides > 0 {
		word := "overrides"
		if overrides == 1 {
			word = "override"
		}
		parts = append(parts, fmt.Sprintf("%d %s", overrides, word))
	}
	output.ColorDim.Printf("\n%d %s (%s)\n", len(connections), connWord, strings.Join(parts, ", "))
	return nil
}

func runNetworkLinkCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceA := args[1]
	deviceB := args[2]

	ctx := context.Background()

	// Pre-fetch members + network to classify the pair for the success message.
	memberA, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}
	network, err := svc.NetworkGet(ctx, org, vpnName)
	if err != nil {
		return err
	}
	pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	opts := service.NetworkLinkCreateOpts{}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		opts.Enabled = &v
	}
	if cmd.Flags().Changed("generate-psk") {
		v, _ := cmd.Flags().GetBool("generate-psk")
		opts.GeneratePSK = &v
	}

	if _, err := svc.NetworkLinkCreate(ctx, org, vpnName, deviceA, deviceB, opts); err != nil {
		return err
	}

	if source == "implicit" {
		color.Green("✓ Link override created: %s ↔ %s in %s (%s)", deviceA, deviceB, vpnName, output.VpnPairTypeDisplay(pairType))
		if cmd.Flags().Changed("enabled") {
			enabled, _ := cmd.Flags().GetBool("enabled")
			if !enabled {
				fmt.Println("  This override disables the automatic connection between these devices")
			}
		}
	} else {
		color.Green("✓ Link created: %s ↔ %s in %s", deviceA, deviceB, vpnName)
	}
	return nil
}

func runNetworkLinkDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceA := args[1]
	deviceB := args[2]

	ctx := context.Background()

	link, getErr := svc.NetworkLinkGet(ctx, org, vpnName, deviceA, deviceB)
	linkFound := getErr == nil
	if getErr != nil {
		// Only swallow the error when it is a 404 (link doesn't exist for an
		// implicit pair); surface anything else.
		var apiErr *api.APIError
		if !errors.As(getErr, &apiErr) || apiErr.StatusCode != 404 {
			return getErr
		}
	}

	memberA, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}
	network, err := svc.NetworkGet(ctx, org, vpnName)
	if err != nil {
		return err
	}
	pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	if linkFound {
		conn := &models.EffectiveConnection{
			DeviceA:     link.DeviceAName,
			DeviceB:     link.DeviceBName,
			RoleA:       strings.ToUpper(memberA.Role),
			RoleB:       strings.ToUpper(memberB.Role),
			PairType:    pairType,
			Source:      source,
			Active:      link.Enabled,
			HasOverride: source == "implicit",
			HasPSK:      link.HasPSK,
			VpnNetwork:  vpnName,
		}
		return formatter.FormatVpnConnection(conn)
	}

	if source == "implicit" {
		conn := &models.EffectiveConnection{
			DeviceA:    deviceA,
			DeviceB:    deviceB,
			RoleA:      strings.ToUpper(memberA.Role),
			RoleB:      strings.ToUpper(memberB.Role),
			PairType:   pairType,
			Source:     source,
			Active:     true,
			VpnNetwork: vpnName,
		}
		return formatter.FormatVpnConnection(conn)
	}

	return fmt.Errorf("no connection between %s and %s. Create a link with 'ndcli network link create'", deviceA, deviceB)
}

func runNetworkLinkUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceA := args[1]
	deviceB := args[2]

	opts := service.NetworkLinkUpdateOpts{}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		opts.Enabled = &v
	}
	if cmd.Flags().Changed("regenerate-psk") {
		v, _ := cmd.Flags().GetBool("regenerate-psk")
		opts.RegeneratePSK = &v
	}
	if opts.Enabled == nil && opts.RegeneratePSK == nil {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()
	_, err := svc.NetworkLinkUpdate(ctx, org, vpnName, deviceA, deviceB, opts)
	if err != nil {
		// 404 — auto-create override for implicit pairs.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			memberA, errA := svc.NetworkMemberGet(ctx, org, vpnName, deviceA)
			memberB, errB := svc.NetworkMemberGet(ctx, org, vpnName, deviceB)
			network, errN := svc.NetworkGet(ctx, org, vpnName)
			if errA == nil && errB == nil && errN == nil {
				pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)
				if source == "implicit" {
					createOpts := service.NetworkLinkCreateOpts{Enabled: opts.Enabled}
					if opts.RegeneratePSK != nil {
						createOpts.GeneratePSK = opts.RegeneratePSK
					}
					if _, cErr := svc.NetworkLinkCreate(ctx, org, vpnName, deviceA, deviceB, createOpts); cErr != nil {
						return cErr
					}
					color.Green("✓ Link override created: %s ↔ %s in %s (%s)", deviceA, deviceB, vpnName, output.VpnPairTypeDisplay(pairType))
					if opts.Enabled != nil && !*opts.Enabled {
						fmt.Println("  This override disables the automatic connection between these devices")
					}
					return nil
				}
			}
		}
		return err
	}

	// Success — classify the pair to phrase the success line.
	memberA, errA := svc.NetworkMemberGet(ctx, org, vpnName, deviceA)
	memberB, errB := svc.NetworkMemberGet(ctx, org, vpnName, deviceB)
	network, errN := svc.NetworkGet(ctx, org, vpnName)
	if errA != nil || errB != nil || errN != nil {
		color.Green("✓ Link updated: %s ↔ %s in %s", deviceA, deviceB, vpnName)
		return nil
	}
	_, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)
	if source == "implicit" {
		if opts.Enabled != nil && !*opts.Enabled {
			color.Green("✓ Link override updated: %s ↔ %s in %s (automatic connection disabled)", deviceA, deviceB, vpnName)
			return nil
		}
		color.Green("✓ Link override updated: %s ↔ %s in %s", deviceA, deviceB, vpnName)
	} else {
		color.Green("✓ Link updated: %s ↔ %s in %s", deviceA, deviceB, vpnName)
	}
	return nil
}

func runNetworkLinkDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceA := args[1]
	deviceB := args[2]

	ctx := context.Background()

	memberA, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := svc.NetworkMemberGet(ctx, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}
	network, err := svc.NetworkGet(ctx, org, vpnName)
	if err != nil {
		return err
	}
	pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		var prompt string
		if source == "implicit" {
			prompt = fmt.Sprintf("Remove link override for '%s ↔ %s' in '%s'? The automatic %s connection will remain active.",
				deviceA, deviceB, vpnName, output.VpnPairTypeDisplay(pairType))
		} else {
			prompt = fmt.Sprintf("Delete link '%s ↔ %s' in '%s'? This will disconnect these devices.",
				deviceA, deviceB, vpnName)
		}
		if !helpers.Confirm(prompt) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	if err := svc.NetworkLinkDelete(ctx, org, vpnName, deviceA, deviceB); err != nil {
		return err
	}
	if source == "implicit" {
		color.Green("✓ Link override removed: %s ↔ %s in %s (automatic connection remains active)", deviceA, deviceB, vpnName)
	} else {
		color.Green("✓ Link deleted: %s ↔ %s in %s (devices disconnected)", deviceA, deviceB, vpnName)
	}
	return nil
}
