package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	internalHelpers "github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
)

var snippetCmd = &cobra.Command{
	Use:   "snippet",
	Short: "Snippet management commands",
}

var snippetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List snippets",
	RunE:  runSnippetList,
}

var snippetCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new snippet",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnippetCreate,
}

var snippetDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show snippet details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetDescribe,
}

var snippetEditCmd = &cobra.Command{
	Use:               "edit [name]",
	Short:             "Edit snippet content in external editor",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetEdit,
}

var snippetUpdateContentCmd = &cobra.Command{
	Use:               "update-content [name] [file]",
	Short:             "Update snippet content from file",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetUpdateContent,
}

var snippetRenameCmd = &cobra.Command{
	Use:               "rename [name] [new-name]",
	Short:             "Rename a snippet",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetRename,
}

var snippetSetPriorityCmd = &cobra.Command{
	Use:               "set-priority [name] [priority]",
	Short:             "Set snippet priority",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetSetPriority,
}

var snippetDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete a snippet",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetDelete,
}

var snippetPullCmd = &cobra.Command{
	Use:   "pull [device] [name]",
	Short: "Pull snippet content from a device",
	Long: `Pull snippet content from a device's configuration.

This creates an asynchronous task that retrieves the specified configuration
from the device and optionally stores it as a snippet in the database.

Matching behavior:
- For USER: Exact name match on the username
- For GROUP: Exact name match on the group name
- For ALIAS: Exact name match on the alias name field
- For RULE: Partial description match (case-insensitive)
  - If multiple rules match, the task will fail with an error
  - Use a more specific description to identify a unique rule
- For UNBOUND_HOST_OVERRIDE: Match by hostname.domain (e.g., server1.local)
- For UNBOUND_DOMAIN_FORWARD: Match by domain name (e.g., internal.corp)
- For UNBOUND_HOST_ALIAS: Match by hostname.domain (e.g., www.local)
- For UNBOUND_ACL: Match by ACL name (e.g., lan-clients)

By default, the command returns immediately with the task ID.
Use --wait (-w) to wait for the task to complete and display the result.`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeDevices,
	RunE:              runSnippetPull,
}

func init() {
	snippetCmd.AddCommand(snippetListCmd)
	snippetCmd.AddCommand(snippetCreateCmd)
	snippetCmd.AddCommand(snippetDescribeCmd)
	snippetCmd.AddCommand(snippetEditCmd)
	snippetCmd.AddCommand(snippetUpdateContentCmd)
	snippetCmd.AddCommand(snippetRenameCmd)
	snippetCmd.AddCommand(snippetSetPriorityCmd)
	snippetCmd.AddCommand(snippetDeleteCmd)
	snippetCmd.AddCommand(snippetPullCmd)

	// List flags
	snippetListCmd.Flags().String("type", "", "Filter by type: USER, GROUP, ALIAS, RULE, UNBOUND_HOST_OVERRIDE, UNBOUND_DOMAIN_FORWARD, UNBOUND_HOST_ALIAS, UNBOUND_ACL")
	snippetListCmd.Flags().String("name", "", "Filter by name (regex pattern)")
	snippetListCmd.Flags().String("sort-by", "priority:asc", "Sort field and direction (priority, name, created_at, updated_at)")
	snippetListCmd.Flags().Int("page", 1, "Page number")
	snippetListCmd.Flags().Int("per-page", 50, "Items per page (max 100)")
	snippetListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("updated-after", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("updated-before", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	// Create flags
	snippetCreateCmd.Flags().String("type", "", "Snippet type (required): USER, GROUP, ALIAS, RULE, UNBOUND_HOST_OVERRIDE, UNBOUND_DOMAIN_FORWARD, UNBOUND_HOST_ALIAS, UNBOUND_ACL")
	snippetCreateCmd.Flags().String("content", "", "Snippet content (required)")
	snippetCreateCmd.Flags().String("file", "", "Read content from file instead of --content")
	snippetCreateCmd.Flags().Int("priority", 1000, "Snippet priority 1-60000 (default 1000)")

	// Pull flags
	snippetPullCmd.Flags().String("type", "ALIAS", "Config type to pull: USER, GROUP, ALIAS, RULE, UNBOUND_HOST_OVERRIDE, UNBOUND_DOMAIN_FORWARD, UNBOUND_HOST_ALIAS, UNBOUND_ACL")
	snippetPullCmd.Flags().Bool("auto-create", false, "Create snippet in DB if it doesn't exist")
	snippetPullCmd.Flags().Bool("overwrite", false, "Update snippet in DB if it already exists")
	snippetPullCmd.Flags().BoolP("wait", "w", false, "Wait for task to complete")
}

func runSnippetList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	snippetType, _ := cmd.Flags().GetString("type")
	name, _ := cmd.Flags().GetString("name")
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
	if snippetType != "" {
		params["type"] = snippetType
	}
	if name != "" {
		params["name"] = name
	}
	if sortBy != "" {
		params["sort_by"] = sortBy
	}
	if createdAfter != "" {
		parsed, err := internalHelpers.ParseTimeFilter(createdAfter)
		if err != nil {
			return fmt.Errorf("invalid created-after value: %w", err)
		}
		params["created_after"] = parsed
	}
	if createdBefore != "" {
		parsed, err := internalHelpers.ParseTimeFilter(createdBefore)
		if err != nil {
			return fmt.Errorf("invalid created-before value: %w", err)
		}
		params["created_before"] = parsed
	}
	if updatedAfter != "" {
		parsed, err := internalHelpers.ParseTimeFilter(updatedAfter)
		if err != nil {
			return fmt.Errorf("invalid updated-after value: %w", err)
		}
		params["updated_after"] = parsed
	}
	if updatedBefore != "" {
		parsed, err := internalHelpers.ParseTimeFilter(updatedBefore)
		if err != nil {
			return fmt.Errorf("invalid updated-before value: %w", err)
		}
		params["updated_before"] = parsed
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets", org), params)
	if err != nil {
		return err
	}

	var result models.SnippetListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatSnippets(result.Items); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runSnippetCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	snippetType, _ := cmd.Flags().GetString("type")
	content, _ := cmd.Flags().GetString("content")
	file, _ := cmd.Flags().GetString("file")
	priority, _ := cmd.Flags().GetInt("priority")

	// Validate required fields
	if snippetType == "" {
		return fmt.Errorf("--type is required (USER, GROUP, ALIAS, RULE, UNBOUND_HOST_OVERRIDE, UNBOUND_DOMAIN_FORWARD, UNBOUND_HOST_ALIAS, UNBOUND_ACL)")
	}

	// Get content from file if specified
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	}

	if content == "" {
		return fmt.Errorf("--content or --file is required")
	}

	payload := map[string]interface{}{
		"name":     name,
		"type":     snippetType,
		"content":  content,
		"priority": priority,
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets", org), payload)
	if err != nil {
		return err
	}

	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return err
	}

	color.Green("✓ Snippet created: %s", name)
	return nil
}

func runSnippetDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, name), nil)
	if err != nil {
		return err
	}

	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return err
	}

	if err := formatter.FormatSnippet(&snippet); err != nil {
		return err
	}

	// Also show content
	fmt.Println()
	color.Cyan("Content:")
	fmt.Println(snippet.Content)

	return nil
}

func runSnippetEdit(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]

	// Get current snippet
	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, name), nil)
	if err != nil {
		return err
	}

	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return err
	}

	// Determine file extension based on type
	ext := ".xml"

	// Open editor
	newContent, err := internalHelpers.EditContent(snippet.Content, ext)
	if err != nil {
		return fmt.Errorf("failed to edit content: %w", err)
	}

	// Check if content changed
	if newContent == snippet.Content {
		fmt.Println("No changes made")
		return nil
	}

	// Update snippet content
	payload := map[string]string{
		"content": newContent,
	}

	resp, err = apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/content", org, name), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet updated: %s", name)
	return nil
}

func runSnippetUpdateContent(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	file := args[1]

	// Read content from file
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	payload := map[string]string{
		"content": string(data),
	}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/content", org, name), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet content updated: %s", name)
	return nil
}

func runSnippetRename(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	newName := args[1]

	payload := map[string]string{
		"new_name": newName,
	}

	ctx := context.Background()
	resp, err := apiClient.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/rename", org, name), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet renamed: %s -> %s", name, newName)
	return nil
}

func runSnippetSetPriority(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	priorityStr := args[1]

	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		return fmt.Errorf("invalid priority: %s", priorityStr)
	}

	if priority < 1 || priority > 60000 {
		return fmt.Errorf("priority must be between 1 and 60000")
	}

	payload := map[string]int{
		"priority": priority,
	}

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/priority", org, name), payload)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Priority set to %d for: %s", priority, name)
	return nil
}

func runSnippetDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]

	if !internalHelpers.Confirm(fmt.Sprintf("Delete snippet '%s'?", name)) {
		fmt.Println("Cancelled")
		return nil
	}

	ctx := context.Background()
	resp, err := apiClient.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, name))
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Snippet deleted: %s", name)
	return nil
}

func runSnippetPull(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	name := args[1]

	configType, _ := cmd.Flags().GetString("type")
	autoCreate, _ := cmd.Flags().GetBool("auto-create")
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	wait, _ := cmd.Flags().GetBool("wait")

	// Build query parameters
	params := url.Values{}
	params.Set("name", name)
	if configType != "" {
		params.Set("config_type", configType)
	}
	if autoCreate {
		params.Set("auto_create", "true")
	}
	if overwrite {
		params.Set("overwrite", "true")
	}

	// Create pull task
	ctx := context.Background()
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/pull?%s", org, deviceName, params.Encode())
	resp, err := apiClient.Post(ctx, endpoint, nil)
	if err != nil {
		return err
	}

	var pullResp struct {
		Task       string `json:"task"`
		Name       string `json:"name"`
		ConfigType string `json:"config_type"`
		Status     string `json:"status"`
		Message    string `json:"message"`
	}
	if err := api.ParseResponse(resp, &pullResp); err != nil {
		return err
	}

	color.Green("✓ Pull task created: %s", pullResp.Task)

	if !wait {
		fmt.Printf("Check task status with: ndcli task describe %s\n", pullResp.Task)
		return nil
	}

	fmt.Println("Waiting for task to complete...")

	// Poll for task completion
	taskID := pullResp.Task
	maxAttempts := 60 // 60 seconds max
	for attempt := 0; attempt < maxAttempts; attempt++ {
		time.Sleep(1 * time.Second)

		resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s", taskID), nil)
		if err != nil {
			return fmt.Errorf("failed to check task status: %w", err)
		}

		var task models.Task
		if err := api.ParseResponse(resp, &task); err != nil {
			return err
		}

		switch task.Status {
		case models.TaskStatusCompleted:
			color.Green("✓ Snippet pulled successfully: %s", name)
			if task.Message != "" {
				fmt.Println()
				color.Cyan("Content:")
				fmt.Println(task.Message)
			}
			return nil
		case models.TaskStatusFailed:
			errMsg := task.Message
			if errMsg == "" {
				errMsg = task.ErrorMessage
			}
			return fmt.Errorf("pull task failed: %s", errMsg)
		case models.TaskStatusCancelled:
			return fmt.Errorf("pull task was cancelled")
		}
		// Still pending/in progress, continue polling
	}

	return fmt.Errorf("timeout waiting for pull task to complete. Check task status with: ndcli task describe %s", taskID)
}
