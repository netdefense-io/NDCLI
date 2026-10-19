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

--type, --status, --exclude-type and --origin each take one value, several
values (repeat the flag or separate them with commas) or, except
--exclude-type, "all" for no filter. A task is listed when it has any of the
given types, statuses and origins and none of the excluded types, and matches
every other filter. Case is ignored; a value that is not a known type, status
or origin is refused before anything is sent.

Source is where a task came from: who asked for it and from which client
(WEB, CLI, MCP or API, a personal access token), the schedule that fired it,
or the firmware track. A run groups the tasks of one request, or of one fire
of a schedule spec; filter on it with --run.

Time filters take 30m, 2h, 7d or 2w (that long ago) or an ISO 8601 time.
--scheduled-after and --scheduled-before also take +30m or +2h (that long
from now); a time without an offset is UTC.

The filters from --exclude-type on need a recent control plane. One that
does not have a filter yet ignores it; ndcli then refuses with
SERVER_TOO_OLD rather than list tasks the filter did not select.`,
	Example: `  ndcli task list --status FAILED,EXPIRED
  ndcli task list --type SYNC --type BACKUP --created-after 24h
  ndcli task list --created-after 24h --include-active --exclude-type CONNECT
  ndcli task list --origin SCHEDULE --schedule nightly-backup
  ndcli task list --run R7x2Kp9Q
  ndcli task list --requested-by ana@acme.com --device-exact e2e-a
  ndcli task list --status SCHEDULED --scheduled-before +24h`,
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
	taskListCmd.Flags().String("device", "", "Filter by device name (regex); --device-exact takes an exact name")
	taskListCmd.Flags().Bool("expired", false, "Filter by expired status (true=expired only, false=not expired)")
	taskListCmd.Flags().String("created-after", "", "Filter tasks created after (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	taskListCmd.Flags().String("created-before", "", "Filter tasks created before (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	taskListCmd.Flags().StringSlice("exclude-type", nil, "Leave out these task types, repeatable or comma-separated")
	taskListCmd.Flags().StringSlice("origin", nil, "Filter by source, repeatable or comma-separated: "+models.TaskValuesHelp(models.TaskOrigins))
	taskListCmd.Flags().String("schedule", "", "Only tasks the specs of this schedule created (exact name)")
	taskListCmd.Flags().StringSlice("device-exact", nil, "Only tasks of the devices with exactly these names, repeatable or comma-separated")
	taskListCmd.Flags().String("search", "", "Only tasks whose code or device name contains this text (plain text, not a pattern)")
	taskListCmd.Flags().String("scheduled-after", "", "Only tasks scheduled to run at or after (e.g., 2h, +2h, ISO 8601)")
	taskListCmd.Flags().String("scheduled-before", "", "Only tasks scheduled to run at or before (e.g., +24h, ISO 8601)")
	taskListCmd.Flags().Bool("include-active", false, "With --created-after/--created-before, also list every PENDING, SCHEDULED and IN_PROGRESS task, whenever it was created")
	taskListCmd.Flags().String("run", "", "Only the tasks of this run (a task's run_id)")
	taskListCmd.Flags().String("requested-by", "", "Only the tasks the account with this email asked for")
	_ = taskListCmd.RegisterFlagCompletionFunc("exclude-type", completeTaskTypes)
	_ = taskListCmd.RegisterFlagCompletionFunc("origin", completeTaskOrigins)
	_ = taskListCmd.RegisterFlagCompletionFunc("schedule", completeScheduleNames)
	_ = taskListCmd.RegisterFlagCompletionFunc("device-exact", completeDevices)
	_ = taskListCmd.RegisterFlagCompletionFunc("requested-by", completeAccountEmails)
	taskListCmd.Flags().String("sort-by", "created_at:desc", "Sort as field:direction; fields created_at, updated_at, expires_at, scheduled_at, status, type, device")
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
	opts.ExcludeTypes, _ = cmd.Flags().GetStringSlice("exclude-type")
	opts.Origins, _ = cmd.Flags().GetStringSlice("origin")
	opts.Schedule, _ = cmd.Flags().GetString("schedule")
	opts.Devices, _ = cmd.Flags().GetStringSlice("device-exact")
	opts.Search, _ = cmd.Flags().GetString("search")
	opts.ScheduledAfter, _ = cmd.Flags().GetString("scheduled-after")
	opts.ScheduledBefore, _ = cmd.Flags().GetString("scheduled-before")
	opts.IncludeActive, _ = cmd.Flags().GetBool("include-active")
	opts.RunID, _ = cmd.Flags().GetString("run")
	opts.RequestedBy, _ = cmd.Flags().GetString("requested-by")
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
