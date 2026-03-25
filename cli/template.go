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

	// List flags
	templateListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction")
	templateListCmd.Flags().Int("page", 1, "Page number")
	templateListCmd.Flags().Int("per-page", 30, "Items per page")
	templateListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-after", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	templateListCmd.Flags().String("updated-before", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	// Create flags
	templateCreateCmd.Flags().String("description", "", "Template description")
	templateCreateCmd.Flags().String("position", "", "Snippet position: PREPEND (default) or APPEND")

	// Update flags
	templateUpdateCmd.Flags().String("description", "", "New description")
	templateUpdateCmd.Flags().String("name", "", "New name")
	templateUpdateCmd.Flags().String("position", "", "New position: PREPEND or APPEND")
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	sortBy, _ := cmd.Flags().GetString("sort-by")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	createdAfter, _ := cmd.Flags().GetString("created-after")
	createdBefore, _ := cmd.Flags().GetString("created-before")
	updatedAfter, _ := cmd.Flags().GetString("updated-after")
	updatedBefore, _ := cmd.Flags().GetString("updated-before")

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if sortBy != "" {
		params["sort_by"] = sortBy
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
	if updatedAfter != "" {
		parsed, err := helpers.ParseTimeFilter(updatedAfter)
		if err != nil {
			return fmt.Errorf("invalid updated-after value: %w", err)
		}
		params["updated_after"] = parsed
	}
	if updatedBefore != "" {
		parsed, err := helpers.ParseTimeFilter(updatedBefore)
		if err != nil {
			return fmt.Errorf("invalid updated-before value: %w", err)
		}
		params["updated_before"] = parsed
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates", org), params)
	if err != nil {
		return err
	}

	var result models.TemplateListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatTemplates(result.Items); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runTemplateCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	description, _ := cmd.Flags().GetString("description")
	position, _ := cmd.Flags().GetString("position")

	payload := map[string]string{
		"name": name,
	}
	if description != "" {
		payload["description"] = description
	}
	if position != "" {
		payload["position"] = position
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates", org), payload)
	if err != nil {
		return err
	}

	var template models.Template
	if err := api.ParseResponse(resp, &template); err != nil {
		return err
	}

	color.Green("✓ Template created: %s", name)
	return nil
}

func runTemplateDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, name), nil)
	if err != nil {
		return err
	}

	var template models.Template
	if err := api.ParseResponse(resp, &template); err != nil {
		return err
	}

	return formatter.FormatTemplate(&template)
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	newDescription, _ := cmd.Flags().GetString("description")
	newName, _ := cmd.Flags().GetString("name")
	newPosition, _ := cmd.Flags().GetString("position")

	if newDescription == "" && newName == "" && newPosition == "" {
		return fmt.Errorf("no updates specified. Use --description, --name, or --position")
	}

	ctx := context.Background()

	// Handle rename if --name is provided
	if newName != "" {
		payload := map[string]string{"new_name": newName}
		resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/rename", org, name), payload)
		if err != nil {
			return err
		}
		if err := api.ParseResponse(resp, nil); err != nil {
			return err
		}
		color.Green("✓ Template renamed: %s -> %s", name, newName)
		// Update name for subsequent updates
		name = newName
	}

	// Handle description/position update via PATCH
	if newDescription != "" || newPosition != "" {
		payload := map[string]string{}
		if newDescription != "" {
			payload["description"] = newDescription
		}
		if newPosition != "" {
			payload["position"] = newPosition
		}
		resp, err := apiClient.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, name), payload)
		if err != nil {
			return err
		}
		if err := api.ParseResponse(resp, nil); err != nil {
			return err
		}
		if newDescription != "" && newPosition != "" {
			color.Green("✓ Template updated: %s", name)
		} else if newDescription != "" {
			color.Green("✓ Template description updated: %s", name)
		} else {
			color.Green("✓ Template position updated: %s", name)
		}
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

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s", org, name))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Template deleted: %s", name)
	return nil
}

func runTemplateAddSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName := args[0]
	snippetName := args[1]

	payload := map[string]string{"snippet_name": snippetName}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/snippets", org, templateName), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet added to %s: %s", templateName, snippetName)
	return nil
}

func runTemplateRemoveSnippet(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	templateName := args[0]
	snippetName := args[1]

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/templates/%s/snippets/%s", org, templateName, snippetName))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet removed from %s: %s", templateName, snippetName)
	return nil
}

