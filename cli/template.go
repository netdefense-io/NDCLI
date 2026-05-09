package cli

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Template management commands",
}

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List templates",
	RunE:  runTemplateList,
}

var templateCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateCreate,
}

var templateDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show template details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateDescribe,
}

var templateUpdateCmd = &cobra.Command{
	Use:               "update [name]",
	Short:             "Update a template",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateUpdate,
}

var templateDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete a template",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateDelete,
}

var templateAddSnippetCmd = &cobra.Command{
	Use:               "add-snippet [template] [snippet]",
	Short:             "Add a snippet to a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplateThenSnippet,
	RunE:              runTemplateAddSnippet,
}

var templateRemoveSnippetCmd = &cobra.Command{
	Use:               "remove-snippet [template] [snippet]",
	Short:             "Remove a snippet from a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplateThenSnippet,
	RunE:              runTemplateRemoveSnippet,
}

func init() {
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateCreateCmd)
	templateCmd.AddCommand(templateDescribeCmd)
	templateCmd.AddCommand(templateUpdateCmd)
	templateCmd.AddCommand(templateDeleteCmd)
	templateCmd.AddCommand(templateAddSnippetCmd)
	templateCmd.AddCommand(templateRemoveSnippetCmd)

	templateListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction")
	templateListCmd.Flags().Int("page", 1, "Page number")
	templateListCmd.Flags().Int("per-page", 30, "Items per page")
	templateListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-after", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-before", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	templateCreateCmd.Flags().String("description", "", "Template description")
	templateCreateCmd.Flags().String("position", "", "Snippet position: PREPEND (default) or APPEND")

	templateUpdateCmd.Flags().String("description", "", "New description")
	templateUpdateCmd.Flags().String("name", "", "New name")
	templateUpdateCmd.Flags().String("position", "", "New position: PREPEND or APPEND")
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.TemplateListOpts{}
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	opts.CreatedAfter, _ = cmd.Flags().GetString("created-after")
	opts.CreatedBefore, _ = cmd.Flags().GetString("created-before")
	opts.UpdatedAfter, _ = cmd.Flags().GetString("updated-after")
	opts.UpdatedBefore, _ = cmd.Flags().GetString("updated-before")

	result, err := svc.TemplateList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatTemplates(result.Templates); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runTemplateCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	description, _ := cmd.Flags().GetString("description")
	position, _ := cmd.Flags().GetString("position")

	if _, err := svc.TemplateCreate(context.Background(), org, service.TemplateCreateOpts{
		Name:        name,
		Description: description,
		Position:    position,
	}); err != nil {
		return err
	}
	color.Green("✓ Template created: %s", name)
	return nil
}

func runTemplateDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	tmpl, err := svc.TemplateGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatTemplate(tmpl)
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	opts := service.TemplateUpdateOpts{}
	opts.Description, _ = cmd.Flags().GetString("description")
	opts.NewName, _ = cmd.Flags().GetString("name")
	opts.Position, _ = cmd.Flags().GetString("position")

	if opts.NewName == "" && opts.Description == "" && opts.Position == "" {
		return fmt.Errorf("no updates specified. Use --description, --name, or --position")
	}

	finalName, err := svc.TemplateUpdate(context.Background(), org, name, opts)
	if err != nil {
		return err
	}

	if opts.NewName != "" {
		color.Green("✓ Template renamed: %s -> %s", name, opts.NewName)
	}
	if opts.Description != "" && opts.Position != "" {
		color.Green("✓ Template updated: %s", finalName)
	} else if opts.Description != "" {
		color.Green("✓ Template description updated: %s", finalName)
	} else if opts.Position != "" {
		color.Green("✓ Template position updated: %s", finalName)
	}
	return nil
}

func runTemplateDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	if !helpers.Confirm(fmt.Sprintf("Delete template '%s'?", name)) {
		fmt.Println("Cancelled")
		return nil
	}
	if err := svc.TemplateDelete(context.Background(), org, name); err != nil {
		return err
	}
	color.Green("✓ Template deleted: %s", name)
	return nil
}

func runTemplateAddSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, snippetName := args[0], args[1]
	if err := svc.TemplateAddSnippet(context.Background(), org, templateName, snippetName); err != nil {
		return err
	}
	color.Green("✓ Snippet added to %s: %s", templateName, snippetName)
	return nil
}

func runTemplateRemoveSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, snippetName := args[0], args[1]
	if err := svc.TemplateRemoveSnippet(context.Background(), org, templateName, snippetName); err != nil {
		return err
	}
	color.Green("✓ Snippet removed from %s: %s", templateName, snippetName)
	return nil
}
