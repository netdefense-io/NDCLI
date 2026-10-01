package cli

import (
	"context"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task maintenance commands (list, describe, cancel)",
	Long: `Task maintenance commands.

Tasks are produced by many flows (sync apply, VPN config, backup schedules,
device commands issued via ` + "`ndcli run`" + `). This command surface is for
inspecting and cancelling them — it does NOT create tasks. To issue a
command to one or more devices, use ` + "`ndcli run`" + `.`,
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	Long: `List tasks in the organization.

--type and --status each take one value, several values (repeat the flag or
separate them with commas) or "all" for no filter. A task is listed when it has
any of the given types and any of the given statuses. Case is ignored; a value
that is not a known type or status is refused before anything is sent.`,
	Example: `  ndcli task list --status FAILED,EXPIRED
  ndcli task list --type SYNC --type BACKUP --created-after 24h`,
	RunE: runTaskList,
}

var taskDescribeCmd = &cobra.Command{
	Use:               "describe [task-id]",
	Short:             "Show task details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTasks,
	RunE:              runTaskDescribe,
}

var taskCancelCmd = &cobra.Command{
	Use:               "cancel [task-id]",
	Short:             "Cancel a pending or scheduled task",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeTasks,
	RunE:              runTaskCancel,
}

func init() {
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskDescribeCmd)
	taskCmd.AddCommand(taskCancelCmd)

	// List flags
	taskListCmd.Flags().StringSlice("status", nil, "Filter by status, repeatable or comma-separated: "+models.TaskValuesHelp(models.TaskStatuses))
	taskListCmd.Flags().StringSlice("type", nil, "Filter by task type, repeatable or comma-separated: "+models.TaskValuesHelp(models.TaskTypes))
	_ = taskListCmd.RegisterFlagCompletionFunc("status", completeTaskStatuses)
	_ = taskListCmd.RegisterFlagCompletionFunc("type", completeTaskTypes)
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

	opts := service.TaskListOpts{}
	opts.Statuses, _ = cmd.Flags().GetStringSlice("status")
	opts.Types, _ = cmd.Flags().GetStringSlice("type")
	opts.Device, _ = cmd.Flags().GetString("device")
	if cmd.Flags().Changed("expired") {
		opts.Expired, _ = cmd.Flags().GetBool("expired")
		opts.ExpiredSet = true
	}
	opts.CreatedAfter, _ = cmd.Flags().GetString("created-after")
	opts.CreatedBefore, _ = cmd.Flags().GetString("created-before")
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")

	result, err := svc.TaskList(context.Background(), org, opts)
	if err != nil {
		return err
	}

	if err := formatter.FormatTasks(result.Tasks, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runTaskDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	task, err := svc.TaskGet(context.Background(), args[0])
	if err != nil {
		return err
	}
	return formatter.FormatTask(task)
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	requireAuth()
	taskID := args[0]
	if err := svc.TaskCancel(context.Background(), taskID); err != nil {
		return err
	}
	color.Green("✓ Task cancelled: %s", taskID)
	return nil
}
