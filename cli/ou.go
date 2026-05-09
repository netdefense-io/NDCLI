package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
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

	opts := service.OUListOpts{}
	opts.Status, _ = cmd.Flags().GetString("status")
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.SortOrder, _ = cmd.Flags().GetString("sort-order")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PageSize, _ = cmd.Flags().GetInt("per-page")
	opts.CreatedAfter, _ = cmd.Flags().GetString("created-after")
	opts.CreatedBefore, _ = cmd.Flags().GetString("created-before")

	result, err := svc.OUList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatOUs(result.OUs); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PageSize)
	return nil
}

func runOUDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ou, err := svc.OUGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatOU(ou)
}

func runOUCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	description, _ := cmd.Flags().GetString("description")
	if _, err := svc.OUCreate(context.Background(), org, name, description); err != nil {
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
	if err := svc.OUDelete(context.Background(), org, name); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && len(apiErr.BlockingResources) > 0 {
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

	oldName, newName := args[0], args[1]
	if _, err := svc.OURename(context.Background(), org, oldName, newName); err != nil {
		return err
	}
	color.Green("✓ OU renamed: %s → %s", oldName, newName)
	return nil
}

func runOUDeviceList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	result, err := svc.OUDeviceList(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatDevices(result.Devices, result.Total, result.Quota)
}

func runOUAddDevice(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName, deviceName := args[0], args[1]
	if err := svc.OUAddDevice(context.Background(), org, ouName, deviceName); err != nil {
		return err
	}
	color.Green("✓ Device added to %s: %s", ouName, deviceName)
	return nil
}

func runOURemoveDevice(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName, deviceName := args[0], args[1]
	if err := svc.OURemoveDevice(context.Background(), org, ouName, deviceName); err != nil {
		return err
	}
	color.Green("✓ Device removed from %s: %s", ouName, deviceName)
	return nil
}

func runOUTemplateAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName, templateName := args[0], args[1]
	if err := svc.OUTemplateAdd(context.Background(), org, ouName, templateName); err != nil {
		return err
	}
	color.Green("✓ Template added to %s: %s", ouName, templateName)
	return nil
}

func runOUTemplateRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	ouName, templateName := args[0], args[1]
	if err := svc.OUTemplateRemove(context.Background(), org, ouName, templateName); err != nil {
		return err
	}
	color.Green("✓ Template removed from %s: %s", ouName, templateName)
	return nil
}

func runOUTemplateList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	result, err := svc.OUTemplateList(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatTemplates(result.Items)
}
