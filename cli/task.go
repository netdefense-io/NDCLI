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
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task management commands",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runTaskList,
}

var taskDescribeCmd = &cobra.Command{
	Use:   "describe [task-id]",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDescribe,
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel [task-id]",
	Short: "Cancel a pending or scheduled task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCancel,
}

var taskCreateCmd = &cobra.Command{
	Use:   "create [device] [type]",
	Short: "Create a task for a device",
	Long: `Create a task for a device.

Available task types:
  PING      Check device connectivity
  SHUTDOWN  Shutdown the device
  REBOOT    Reboot the device
  RESTART   Restart the device service`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskCreate,
}

func init() {
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskDescribeCmd)
	taskCmd.AddCommand(taskCancelCmd)
	taskCmd.AddCommand(taskCreateCmd)

	// List flags
	taskListCmd.Flags().String("status", "", "Filter by status: PENDING, SCHEDULED, IN_PROGRESS, COMPLETED, FAILED, CANCELLED, EXPIRED")
	taskListCmd.Flags().String("type", "", "Filter by task type: BACKUP, PING, PULL, REBOOT, RESTART, SHUTDOWN, SYNC")
	taskListCmd.Flags().String("device", "", "Filter by device name (regex)")
	taskListCmd.Flags().Bool("expired", false, "Filter by expired status (true=expired only, false=not expired)")
	taskListCmd.Flags().String("created-after", "", "Filter tasks created after (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	taskListCmd.Flags().String("created-before", "", "Filter tasks created before (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	taskListCmd.Flags().String("sort-by", "created_at:desc", "Sort field and direction")
	taskListCmd.Flags().Int("page", 1, "Page number")
	taskListCmd.Flags().Int("per-page", 30, "Items per page (max 100)")
}

func runTaskList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	status, _ := cmd.Flags().GetString("status")
	taskType, _ := cmd.Flags().GetString("type")
	device, _ := cmd.Flags().GetString("device")
	expired, _ := cmd.Flags().GetBool("expired")
	createdAfter, _ := cmd.Flags().GetString("created-after")
	createdBefore, _ := cmd.Flags().GetString("created-before")
	sortBy, _ := cmd.Flags().GetString("sort-by")
	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")

	params := map[string]string{
		"organization": org,
		"page":         strconv.Itoa(page),
		"per_page":     strconv.Itoa(perPage),
	}

	if status != "" {
		params["status"] = status
	}
	if taskType != "" {
		params["type"] = taskType
	}
	if device != "" {
		params["device_name"] = device
	}
	if cmd.Flags().Changed("expired") {
		params["expired"] = strconv.FormatBool(expired)
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
	if sortBy != "" {
		params["sort_by"] = sortBy
	}

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, "/api/v1/tasks", params)
	if err != nil {
		return err
	}

	var result models.TaskListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatTasks(result.Items, result.Total); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}

func runTaskDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()

	taskID := args[0]

	ctx := context.Background()
	resp, err := apiClient.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		return err
	}

	var task models.Task
	if err := api.ParseResponse(resp, &task); err != nil {
		return err
	}

	return formatter.FormatTask(&task)
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	requireAuth()

	taskID := args[0]

	ctx := context.Background()
	resp, err := apiClient.Put(ctx, fmt.Sprintf("/api/v1/tasks/%s/cancel", taskID), nil)
	if err != nil {
		return err
	}

	if err := api.ParseResponse(resp, nil); err != nil {
		return err
	}

	color.Green("✓ Task cancelled: %s", taskID)
	return nil
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	taskType := strings.ToUpper(args[1])

	// Validate task type
	validTypes := map[string]bool{
		"PING":     true,
		"SHUTDOWN": true,
		"REBOOT":   true,
		"RESTART":  true,
	}
	if !validTypes[taskType] {
		return fmt.Errorf("invalid task type: %s. Valid types: PING, SHUTDOWN, REBOOT, RESTART", args[1])
	}

	// Build query parameters
	params := url.Values{}
	params.Set("task_type", taskType)

	ctx := context.Background()
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/task?%s", org, deviceName, params.Encode())
	resp, err := apiClient.Post(ctx, endpoint, nil)
	if err != nil {
		return err
	}

	var task models.Task
	if err := api.ParseResponse(resp, &task); err != nil {
		return err
	}

	color.Green("✓ Task created: %s", task.ID)
	fmt.Printf("Type: %s, Device: %s, Status: %s\n", task.Type, deviceName, task.Status)
	return nil
}
