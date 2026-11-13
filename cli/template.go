package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/models"
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
	Args:  noArgs,
	RunE:  runTemplateList,
}

var templateCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateCreate,
}

var templateDescribeCmd = &cobra.Command{
	Use:               "describe <name>",
	Short:             "Show template details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateDescribe,
}

var templateUpdateCmd = &cobra.Command{
	Use:               "update <name>",
	Short:             "Update a template",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateUpdate,
}

var templateDeleteCmd = &cobra.Command{
	Use:               "delete <name>",
	Short:             "Delete a template",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateDelete,
}

var templateAddSnippetCmd = &cobra.Command{
	Use:               "add-snippet <template> <snippet>",
	Short:             "Add a snippet to a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplateThenSnippet,
	RunE:              runTemplateAddSnippet,
}

var templateRemoveSnippetCmd = &cobra.Command{
	Use:               "remove-snippet <template> <snippet>",
	Short:             "Remove a snippet from a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplateThenSnippet,
	RunE:              runTemplateRemoveSnippet,
}

var templateAddSoftwareCmd = &cobra.Command{
	Use:               "add-software <template> <software-policy>",
	Short:             "Attach a software policy to a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateAddSoftware,
}

var templateRemoveSoftwareCmd = &cobra.Command{
	Use:               "remove-software <template> <software-policy>",
	Short:             "Detach a software policy from a template",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTemplates,
	RunE:              runTemplateRemoveSoftware,
}

func init() {
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateCreateCmd)
	templateCmd.AddCommand(templateDescribeCmd)
	templateCmd.AddCommand(templateUpdateCmd)
	templateCmd.AddCommand(templateDeleteCmd)
	addYesFlag(templateDeleteCmd)
	templateCmd.AddCommand(templateAddSnippetCmd)
	templateCmd.AddCommand(templateRemoveSnippetCmd)
	addYesFlag(templateRemoveSnippetCmd)
	templateCmd.AddCommand(templateAddSoftwareCmd)
	templateCmd.AddCommand(templateRemoveSoftwareCmd)
	addYesFlag(templateRemoveSoftwareCmd)

	templateListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction")
	templateListCmd.Flags().Int("page", 1, "Page number")
	templateListCmd.Flags().Int("per-page", 30, "Items per page")
	templateListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-after", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-before", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	templateCreateCmd.Flags().String("description", "", "Template description")
	templateCreateCmd.Flags().String("position", "", "Snippet position: PREPEND (default) or APPEND, in any case")

	templateUpdateCmd.Flags().String("description", "", "New description")
	templateUpdateCmd.Flags().String("name", "", "New name")
	templateUpdateCmd.Flags().String("position", "", "New position: PREPEND or APPEND, in any case. Takes effect at each affected device's next sync; none is triggered")
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
	if err := formatter.FormatTemplates(result.Templates, result.Total); err != nil {
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

	t, err := svc.TemplateCreate(context.Background(), org, service.TemplateCreateOpts{
		Name:        name,
		Description: description,
		Position:    position,
	})
	if err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: name, Value: t, Action: "created",
		Message: fmt.Sprintf("✓ Template created: %s", name)})
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

	var lines []string
	if opts.NewName != "" {
		lines = append(lines, fmt.Sprintf("✓ Template renamed: %s -> %s", name, opts.NewName))
	}
	if opts.Description != "" && opts.Position != "" {
		lines = append(lines, fmt.Sprintf("✓ Template updated: %s", finalName))
	} else if opts.Description != "" {
		lines = append(lines, fmt.Sprintf("✓ Template description updated: %s", finalName))
	} else if opts.Position != "" {
		lines = append(lines, fmt.Sprintf("✓ Template position updated: %s", finalName))
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: name, Action: "updated",
		Details: map[string]interface{}{"name": finalName, "renamed": opts.NewName != "", "old_name": name},
		Message: strings.Join(lines, "\n")})
}

func runTemplateDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Delete template '%s'?", name)); !ok {
		return err
	}
	if err := svc.TemplateDelete(context.Background(), org, name); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: name, Action: "deleted",
		Details: map[string]interface{}{"name": name}, Message: fmt.Sprintf("✓ Template deleted: %s", name)})
}

func runTemplateAddSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, snippetName := args[0], args[1]
	if err := svc.TemplateAddSnippet(context.Background(), org, templateName, snippetName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: templateName, Action: "added",
		Details: map[string]interface{}{"snippet": snippetName}, Message: fmt.Sprintf("✓ Snippet added to %s: %s", templateName, snippetName)})
}

func runTemplateRemoveSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, snippetName := args[0], args[1]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Remove snippet '%s' from template '%s'?", snippetName, templateName)); !ok {
		return err
	}
	if err := svc.TemplateRemoveSnippet(context.Background(), org, templateName, snippetName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: templateName, Action: "removed",
		Details: map[string]interface{}{"snippet": snippetName}, Message: fmt.Sprintf("✓ Snippet removed from %s: %s", templateName, snippetName)})
}

func runTemplateAddSoftware(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, policyName := args[0], args[1]
	if err := svc.TemplateAddSoftwarePolicy(context.Background(), org, templateName, policyName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: templateName, Action: "attached",
		Details: map[string]interface{}{"software_policy_name": policyName}, Message: fmt.Sprintf("✓ Software policy added to %s: %s", templateName, policyName)})
}

func runTemplateRemoveSoftware(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName, policyName := args[0], args[1]
	if ok, err := confirmUnlessYes(cmd, fmt.Sprintf("Remove software policy '%s' from template '%s'?", policyName, templateName)); !ok {
		return err
	}
	if err := svc.TemplateRemoveSoftwarePolicy(context.Background(), org, templateName, policyName); err != nil {
		return err
	}
	return formatter.FormatActionResult(&models.ActionResult{Resource: "template", Name: templateName, Action: "detached",
		Details: map[string]interface{}{"software_policy_name": policyName}, Message: fmt.Sprintf("✓ Software policy removed from %s: %s", templateName, policyName)})
}
