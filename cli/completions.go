package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
)


// completeOrganizations returns organization names for shell completion
func completeOrganizations(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Check if already have enough args
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Try to initialize auth and API client silently
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/organizations", nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.OrganizationListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, org := range result.GetItems() {
		names = append(names, org.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeDevices returns device names for the current organization
func completeDevices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Check if already have enough args for single-arg commands
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fetchDeviceNames(cmd)
}

// completeDevicesArg2 returns device names when device is the second argument
func completeDevicesArg2(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Only complete if we have exactly 1 arg already
	if len(args) != 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fetchDeviceNames(cmd)
}

// completePendingDevices returns only PENDING device names
func completePendingDevices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return fetchDeviceNamesByStatus(cmd, "PENDING")
}

// fetchDeviceNames fetches device names from the API
func fetchDeviceNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	return fetchDeviceNamesByStatus(cmd, "")
}

// fetchDeviceNamesByStatus fetches device names filtered by status
func fetchDeviceNamesByStatus(cmd *cobra.Command, status string) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	var params map[string]string
	if status != "" {
		params = map[string]string{"status": status}
	}
	resp, err := apiClient.Get(ctx, "/api/v1/organizations/"+org+"/devices", params)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, device := range result.GetItems() {
		names = append(names, device.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeOUs returns OU names for the current organization
func completeOUs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fetchOUNames(cmd)
}

// fetchOUNames fetches OU names from the API
func fetchOUNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/organizations/"+org+"/ous", nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.OUListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, ou := range result.OUs {
		names = append(names, ou.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeOUThenDevice completes OU for first arg, device for second
func completeOUThenDevice(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return fetchOUNames(cmd)
	case 1:
		return fetchDeviceNames(cmd)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeOUThenTemplate completes OU for first arg, template for second
func completeOUThenTemplate(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return fetchOUNames(cmd)
	case 1:
		return fetchTemplateNames(cmd)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// fetchTemplateNames fetches template names from the API
func fetchTemplateNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/organizations/"+org+"/templates", nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.TemplateListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, tmpl := range result.Items {
		names = append(names, tmpl.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeConfigSetKey completes the key for config set, and org names when key is "org"
func completeConfigSetKey(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		// First arg: config keys
		return []string{"org", "format", "host"}, cobra.ShellCompDirectiveNoFileComp
	case 1:
		// Second arg: depends on key
		if args[0] == "org" {
			return completeOrganizations(cmd, nil, toComplete)
		}
		if args[0] == "format" {
			return []string{"table", "simple", "detailed", "json"}, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// initForCompletion initializes auth manager and API client silently
func initForCompletion() error {
	if apiClient != nil {
		return nil
	}

	// Try to find --conf flag from command line if cfgFile is empty
	configPath := cfgFile
	if configPath == "" {
		configPath = findConfigFromArgs()
	}

	// Load configuration silently
	if err := config.Load(configPath); err != nil {
		return err
	}

	// Initialize auth manager
	authManager = auth.GetManager()
	if authManager == nil {
		return fmt.Errorf("failed to get auth manager")
	}

	// Initialize API client
	apiClient = api.NewClientFromConfig(authManager)

	return nil
}

// completeTemplates returns template names for the current organization
func completeTemplates(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fetchTemplateNames(cmd)
}

// completeTemplateThenSnippet completes template for first arg, snippet for second
func completeTemplateThenSnippet(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return fetchTemplateNames(cmd)
	case 1:
		return fetchSnippetNames(cmd)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeSnippets returns snippet names for the current organization
func completeSnippets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fetchSnippetNames(cmd)
}

// fetchSnippetNames fetches snippet names from the API
func fetchSnippetNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/organizations/"+org+"/snippets", nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.SnippetListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, snippet := range result.Items {
		names = append(names, snippet.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// findConfigFromArgs looks for --conf flag in os.Args
func findConfigFromArgs() string {
	args := os.Args
	for i, arg := range args {
		if arg == "--conf" && i+1 < len(args) {
			return args[i+1]
		}
		if len(arg) > 7 && arg[:7] == "--conf=" {
			return arg[7:]
		}
	}
	return ""
}

// getOrgForCompletion gets org from --org flag or config
func getOrgForCompletion(cmd *cobra.Command) string {
	// Check --org flag first
	if org, _ := cmd.Flags().GetString("org"); org != "" {
		return org
	}

	// Fall back to root command flag
	if org, _ := cmd.Root().Flags().GetString("org"); org != "" {
		return org
	}

	// Fall back to config (use config.Get() directly since PersistentPreRunE loads it)
	if cfg := config.Get(); cfg != nil {
		return cfg.Organization.Name
	}

	return ""
}

// fetchVariableNames fetches variable names for a given scope URL
func fetchVariableNames(cmd *cobra.Command, scopeURL string) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, scopeURL, nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.VariableListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, v := range result.Items {
		names = append(names, v.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeOrgVariables completes variable names at org scope
func completeOrgVariables(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	url := fmt.Sprintf("/api/v1/organizations/%s/variables", org)
	return fetchVariableNames(cmd, url)
}

// completeOUThenVariable completes OU for first arg, variable for second
func completeOUThenVariable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	switch len(args) {
	case 0:
		return fetchOUNames(cmd)
	case 1:
		url := fmt.Sprintf("/api/v1/organizations/%s/ous/%s/variables", org, args[0])
		return fetchVariableNames(cmd, url)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeTemplateThenVariable completes template for first arg, variable for second
func completeTemplateThenVariable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	switch len(args) {
	case 0:
		return fetchTemplateNames(cmd)
	case 1:
		url := fmt.Sprintf("/api/v1/organizations/%s/templates/%s/variables", org, args[0])
		return fetchVariableNames(cmd, url)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// fetchVpnNetworkNames fetches VPN network names from the API
func fetchVpnNetworkNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks", org), nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.VpnNetworkListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, n := range result.Items {
		names = append(names, n.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeVpnNetworks completes VPN network names as first arg
func completeVpnNetworks(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return fetchVpnNetworkNames(cmd)
}

// completeVpnNetworkThenDevice completes VPN network for first arg, device for second
func completeVpnNetworkThenDevice(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return fetchVpnNetworkNames(cmd)
	case 1:
		return fetchDeviceNames(cmd)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeVpnNetworkThenTwoDevices completes VPN network for first arg, device for second and third
func completeVpnNetworkThenTwoDevices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return fetchVpnNetworkNames(cmd)
	case 1, 2:
		return fetchDeviceNames(cmd)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeVpnNetworkDeviceVariable completes VPN network, device, then device variable
func completeVpnNetworkDeviceVariable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	switch len(args) {
	case 0:
		return fetchVpnNetworkNames(cmd)
	case 1:
		return fetchDeviceNames(cmd)
	case 2:
		url := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/variables", org, args[1])
		return fetchVariableNames(cmd, url)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeVpnMemberDevices completes device names from VPN network members for --device flag
func completeVpnMemberDevices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if err := initForCompletion(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// The VPN network name should be the first arg
	if len(args) < 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	vpnName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members", org, vpnName), nil)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var result models.VpnMemberListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, m := range result.Items {
		names = append(names, m.DeviceName)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeDeviceThenVariable completes device for first arg, variable for second
func completeDeviceThenVariable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	org := getOrgForCompletion(cmd)
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	switch len(args) {
	case 0:
		return fetchDeviceNames(cmd)
	case 1:
		url := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/variables", org, args[0])
		return fetchVariableNames(cmd, url)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}
