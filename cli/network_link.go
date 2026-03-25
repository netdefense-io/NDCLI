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
	// List flags — effective connections mode (default)
	networkLinkListCmd.Flags().Bool("raw", false, "Show raw VPN link database rows instead of effective connections")
	networkLinkListCmd.Flags().String("device", "", "Filter connections to those involving this device")
	networkLinkListCmd.Flags().Int("page", 1, "Page number (only with --raw)")
	networkLinkListCmd.Flags().Int("per-page", 30, "Items per page (only with --raw)")
	networkLinkListCmd.RegisterFlagCompletionFunc("device", completeVpnMemberDevices)

	// Create flags
	networkLinkCreateCmd.Flags().Bool("enabled", true, "Enable the link")
	networkLinkCreateCmd.Flags().Bool("generate-psk", false, "Generate a WireGuard pre-shared key")

	// Update flags
	networkLinkUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the link")
	networkLinkUpdateCmd.Flags().Bool("regenerate-psk", false, "Regenerate the pre-shared key")

	// Delete flags
	networkLinkDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runNetworkLinkList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	raw, _ := cmd.Flags().GetBool("raw")

	if raw {
		return runNetworkLinkListOverrides(cmd, org, vpnName)
	}

	return runNetworkLinkListEffective(cmd, org, vpnName)
}

func runNetworkLinkListOverrides(cmd *cobra.Command, org, vpnName string) error {
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
		url.PathEscape(org), url.PathEscape(vpnName)), params)
	if err != nil {
		return err
	}

	var result models.VpnLinkListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatVpnLinks(result.Items, result.Total); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runNetworkLinkListEffective(cmd *cobra.Command, org, vpnName string) error {
	ctx := context.Background()

	network, err := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
	if err != nil {
		return err
	}

	members, err := vpn.FetchAllMembers(ctx, apiClient, org, vpnName)
	if err != nil {
		return err
	}

	links, err := vpn.FetchAllLinks(ctx, apiClient, org, vpnName)
	if err != nil {
		return err
	}

	connections := vpn.ComputeEffectiveConnections(network, members, links)

	if deviceFilter, _ := cmd.Flags().GetString("device"); deviceFilter != "" {
		connections = vpn.FilterByDevice(connections, deviceFilter)
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

	// Fetch both members to validate and classify
	memberA, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}

	// Fetch network for auto_connect_hubs
	network, err := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
	if err != nil {
		return err
	}

	pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	payload := map[string]interface{}{
		"device_a_name": deviceA,
		"device_b_name": deviceB,
	}

	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		payload["enabled"] = v
	}
	if cmd.Flags().Changed("generate-psk") {
		v, _ := cmd.Flags().GetBool("generate-psk")
		payload["generate_psk"] = v
	}

	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
		url.PathEscape(org), url.PathEscape(vpnName)), payload)
	if err != nil {
		return err
	}

	var link models.VpnLink
	if err := api.ParseResponse(resp, &link); err != nil {
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

	// Try fetching the link from the API
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)), nil)
	if err != nil {
		return err
	}

	linkFound := resp.StatusCode < 400
	if !linkFound {
		resp.Body.Close()
	}

	// Fetch members to classify
	memberA, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}

	network, err := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
	if err != nil {
		return err
	}

	pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	if linkFound {
		// Link exists — build effective connection from it
		var link models.VpnLink
		if err := api.ParseResponse(resp, &link); err != nil {
			return err
		}

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

	// Link not found (404)
	if source == "implicit" {
		// Automatic pair without override — show default active connection
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

	// Explicit pair with no link — not connected
	return fmt.Errorf("no connection between %s and %s. Create a link with 'ndcli network link create'", deviceA, deviceB)
}

func runNetworkLinkUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	vpnName := args[0]
	deviceA := args[1]
	deviceB := args[2]
	payload := make(map[string]interface{})

	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		payload["enabled"] = v
	}
	if cmd.Flags().Changed("regenerate-psk") {
		v, _ := cmd.Flags().GetBool("regenerate-psk")
		payload["regenerate_psk"] = v
	}

	if len(payload) == 0 {
		return fmt.Errorf("no update flags specified")
	}

	ctx := context.Background()
	resp, err := apiClient.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)), payload)
	if err != nil {
		return err
	}

	if resp.StatusCode == 404 {
		resp.Body.Close()

		// 404 — check if this is an implicit pair that needs an auto-created override
		memberA, errA := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceA)
		memberB, errB := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceB)
		if errA != nil || errB != nil {
			return fmt.Errorf("link between '%s' and '%s' not found", deviceA, deviceB)
		}

		network, errN := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
		if errN != nil {
			return fmt.Errorf("link between '%s' and '%s' not found", deviceA, deviceB)
		}

		pairType, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)
		if source != "implicit" {
			return fmt.Errorf("link between '%s' and '%s' not found", deviceA, deviceB)
		}

		// Auto-create override for implicit pair
		createPayload := map[string]interface{}{
			"device_a_name": deviceA,
			"device_b_name": deviceB,
		}
		if cmd.Flags().Changed("enabled") {
			v, _ := cmd.Flags().GetBool("enabled")
			createPayload["enabled"] = v
		}
		if cmd.Flags().Changed("regenerate-psk") {
			v, _ := cmd.Flags().GetBool("regenerate-psk")
			createPayload["generate_psk"] = v
		}

		createResp, createErr := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
			url.PathEscape(org), url.PathEscape(vpnName)), createPayload)
		if createErr != nil {
			return createErr
		}

		var created models.VpnLink
		if err := api.ParseResponse(createResp, &created); err != nil {
			return err
		}

		color.Green("✓ Link override created: %s ↔ %s in %s (%s)", deviceA, deviceB, vpnName, output.VpnPairTypeDisplay(pairType))
		if cmd.Flags().Changed("enabled") {
			enabled, _ := cmd.Flags().GetBool("enabled")
			if !enabled {
				fmt.Println("  This override disables the automatic connection between these devices")
			}
		}
		return nil
	}

	var link models.VpnLink
	if err := api.ParseResponse(resp, &link); err != nil {
		return err
	}

	// Fetch members to classify pair
	memberA, errA := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceA)
	memberB, errB := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceB)
	if errA != nil || errB != nil {
		color.Green("✓ Link updated: %s ↔ %s in %s", deviceA, deviceB, vpnName)
		return nil
	}

	network, errN := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
	if errN != nil {
		color.Green("✓ Link updated: %s ↔ %s in %s", deviceA, deviceB, vpnName)
		return nil
	}

	_, source := vpn.ClassifyPair(memberA.Role, memberB.Role, network.AutoConnectHubs)

	if source == "implicit" {
		if cmd.Flags().Changed("enabled") {
			enabled, _ := cmd.Flags().GetBool("enabled")
			if !enabled {
				color.Green("✓ Link override updated: %s ↔ %s in %s (automatic connection disabled)", deviceA, deviceB, vpnName)
				return nil
			}
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

	// Fetch members to classify pair before confirmation
	memberA, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceA)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceA, err)
	}
	memberB, err := vpn.FetchMember(ctx, apiClient, org, vpnName, deviceB)
	if err != nil {
		return fmt.Errorf("member '%s': %w", deviceB, err)
	}

	network, err := vpn.FetchNetwork(ctx, apiClient, org, vpnName)
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

	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)))
	if err != nil {
		return err
	}

	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if source == "implicit" {
		color.Green("✓ Link override removed: %s ↔ %s in %s (automatic connection remains active)", deviceA, deviceB, vpnName)
	} else {
		color.Green("✓ Link deleted: %s ↔ %s in %s (devices disconnected)", deviceA, deviceB, vpnName)
	}

	return nil
}
