package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
)

var ouCmd = &cobra.Command{
	Use:   "ou",
	Short: "Organizational unit management commands",
}

var ouListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizational units",
	RunE:  runOUList,
}

var ouDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show OU details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOUDescribe,
}

var ouCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new OU",
	Args:  cobra.ExactArgs(1),
	RunE:  runOUCreate,
}

var ouDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete an OU",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOUDelete,
}

var ouRenameCmd = &cobra.Command{
	Use:               "rename [old-name] [new-name]",
	Short:             "Rename an OU",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeOUs,
	RunE:              runOURename,
}

var ouDeviceListCmd = &cobra.Command{
	Use:               "device-list [ou]",
	Short:             "List devices in an OU",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOUDeviceList,
}

var ouAddDeviceCmd = &cobra.Command{
	Use:               "add-device [ou] [device]",
	Short:             "Add a device to an OU",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeOUThenDevice,
	RunE:              runOUAddDevice,
}

var ouRemoveDeviceCmd = &cobra.Command{
	Use:               "remove-device [ou] [device]",
	Short:             "Remove a device from an OU",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeOUThenDevice,
	RunE:              runOURemoveDevice,
}

// Template subcommands for OU
var ouTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage OU templates",
}

var ouTemplateAddCmd = &cobra.Command{
	Use:               "add [ou] [template]",
	Short:             "Add a template to an OU",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeOUThenTemplate,
	RunE:              runOUTemplateAdd,
}

var ouTemplateRemoveCmd = &cobra.Command{
	Use:               "remove [ou] [template]",
	Short:             "Remove a template from an OU",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeOUThenTemplate,
	RunE:              runOUTemplateRemove,
}

var ouTemplateListCmd = &cobra.Command{
	Use:               "list [ou]",
	Short:             "List templates assigned to an OU",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOUs,
	RunE:              runOUTemplateList,
}

func init() {
	ouCmd.AddCommand(ouListCmd)
	ouCmd.AddCommand(ouDescribeCmd)
	ouCmd.AddCommand(ouCreateCmd)
	ouCmd.AddCommand(ouDeleteCmd)
	ouCmd.AddCommand(ouRenameCmd)
	ouCmd.AddCommand(ouDeviceListCmd)
	ouCmd.AddCommand(ouAddDeviceCmd)
	ouCmd.AddCommand(ouRemoveDeviceCmd)
	ouCmd.AddCommand(ouTemplateCmd)

	ouTemplateCmd.AddCommand(ouTemplateAddCmd)
	ouTemplateCmd.AddCommand(ouTemplateRemoveCmd)
	ouTemplateCmd.AddCommand(ouTemplateListCmd)

	// List flags
	ouListCmd.Flags().String("status", "all", "Filter by status: all, enabled")
	ouListCmd.Flags().String("name", "", "Filter by name (regex pattern)")
	ouListCmd.Flags().String("sort-by", "name", "Sort field: name, device_count, created_at, updated_at")
	ouListCmd.Flags().String("sort-order", "asc", "Sort order: asc, desc")
	ouListCmd.Flags().Int("page", 1, "Page number")
	ouListCmd.Flags().Int("per-page", 20, "Items per page (1-100)")
	ouListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	ouListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	// Create flags
	ouCreateCmd.Flags().String("description", "", "OU description")
}

func runOUList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	status, _ := cmd.Flags().GetString("status")
	name, _ := cmd.Flags().GetString("name")
	sortBy, _ := cmd.Flags().GetString("sort-by")
	sortOrder, _ := cmd.Flags().GetString("sort-order")
	page, _ := cmd.Flags().GetInt("page")
	pageSize, _ := cmd.Flags().GetInt("per-page")
	createdAfter, _ := cmd.Flags().GetString("created-after")
	createdBefore, _ := cmd.Flags().GetString("created-before")

	params := map[string]string{
		"status":     status,
		"sort_by":    sortBy,
		"sort_order": sortOrder,
		"page":       strconv.Itoa(page),
		"page_size":  strconv.Itoa(pageSize),
	}
	if name != "" {
		params["name"] = name
	}
	if createdAfter != "" {
		parsed, err := helpers.ParseTimeFilter(createdAfter)
		if err != nil {
			return fmt.Errorf("invalid created-after value: %w", err)
		}
		params["created_after"] = parsed
	}
	if createdBefore != "" {
		parsed, err := helpers.ParseTimeFilter(createdBefore)
		if err != nil {
			return fmt.Errorf("invalid created-before value: %w", err)
		}
		params["created_before"] = parsed
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous", org), params)
	if err != nil {
		return err
	}

	var result models.OUListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatOUs(result.OUs); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, pageSize)
	return nil
}

func runOUDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, ouName), nil)
	if err != nil {
		return err
	}

	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return err
	}

	return formatter.FormatOU(&ou)
}

func runOUCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	description, _ := cmd.Flags().GetString("description")

	payload := map[string]string{
		"name": name,
	}
	if description != "" {
		payload["description"] = description
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous", org), payload)
	if err != nil {
		return err
	}

	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return err
	}

	color.Green("✓ OU created: %s", name)
	return nil
}

func runOUDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]

	if !helpers.Confirm(fmt.Sprintf("Delete OU '%s'?", name)) {
		fmt.Println("Cancelled")
		return nil
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, name))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		// Check for conflict with blocking devices
		if apiErr, ok := err.(*api.APIError); ok && len(apiErr.BlockingResources) > 0 {
			color.Red("Cannot delete OU '%s' - %d active device(s) must be removed first:", name, len(apiErr.BlockingResources))
			for _, device := range apiErr.BlockingResources {
				fmt.Printf("  • %s\n", device)
			}
			return nil
		}
		return err
	}

	color.Green("✓ OU deleted: %s", name)
	return nil
}

func runOURename(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	oldName := args[0]
	newName := args[1]

	payload := map[string]string{"name": newName}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s", org, oldName), payload)
	if err != nil {
		return err
	}

	var ou models.OrganizationalUnit
	if err := api.ParseResponse(resp, &ou); err != nil {
		return err
	}

	color.Green("✓ OU renamed: %s → %s", oldName, newName)
	return nil
}

func runOUDeviceList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]

	params := map[string]string{
		"ou": ouName,
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices", org), params)
	if err != nil {
		return err
	}

	var result models.DeviceListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatDevices(result.GetItems(), result.Total, result.Quota)
}

func runOUAddDevice(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]
	deviceName := args[1]

	payload := map[string]string{"device_name": deviceName}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/devices", org, ouName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Device added to %s: %s", ouName, deviceName)
	return nil
}

func runOURemoveDevice(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]
	deviceName := args[1]

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/devices/%s", org, ouName, deviceName))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Device removed from %s: %s", ouName, deviceName)
	return nil
}

func runOUTemplateAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]
	templateName := args[1]

	payload := map[string]string{"template": templateName}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates", org, ouName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Template added to %s: %s", ouName, templateName)
	return nil
}

func runOUTemplateRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]
	templateName := args[1]

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates/%s", org, ouName, templateName))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Template removed from %s: %s", ouName, templateName)
	return nil
}

func runOUTemplateList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/ous/%s/templates", org, ouName), nil)
	if err != nil {
		return err
	}

	var result struct {
		Items []models.Template `json:"items"`
		Total int               `json:"total"`
	}
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	return formatter.FormatTemplates(result.Items)
}
