package cli

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Recurring cadence schedule management",
	Long: `Manage recurring cadence schedules for an organization.

A schedule is a pure cadence (cron expression + timezone). The "what to run"
is registered separately: use --schedule <name> on ndcli run or ndcli sync
apply to attach a task spec to this schedule.

Each schedule is identified by its name (unique per organization). Registered
task specs are addressed by their server-generated code (8-char base62).`,
}

// ── list ─────────────────────────────────────────────────────────────────────

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cadence schedules",
	Args:  cobra.NoArgs,
	RunE:  runScheduleList,
}

func runScheduleList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	result, err := svc.ScheduleList(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatSchedules(result.Schedules, result.Total)
}

// ── describe ─────────────────────────────────────────────────────────────────

var scheduleDescribeCmd = &cobra.Command{
	Use:               "describe <name>",
	Short:             "Show schedule details including registered specs",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeScheduleNames,
	RunE:              runScheduleDescribe,
}

func runScheduleDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	sch, err := svc.ScheduleGet(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatSchedule(sch)
}

// ── create ───────────────────────────────────────────────────────────────────

var scheduleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a cadence schedule",
	Long: `Create a new cadence schedule (cron expression + timezone).

Use ndcli run --schedule <name> or ndcli sync apply --schedule <name> to
register task specs against this schedule.`,
	Args: cobra.NoArgs,
	RunE: runScheduleCreate,
}

func runScheduleCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name, _ := cmd.Flags().GetString("name")
	cron, _ := cmd.Flags().GetString("cron")
	timezone, _ := cmd.Flags().GetString("timezone")
	disabled, _ := cmd.Flags().GetBool("disabled")

	isEnabled := true
	if disabled {
		isEnabled = false
	}

	sch, err := svc.ScheduleCreate(context.Background(), org, service.ScheduleCreateOpts{
		Name:     name,
		Cron:     cron,
		Timezone: timezone,
		Enabled:  isEnabled,
	})
	if err != nil {
		return err
	}
	return formatter.FormatSchedule(sch)
}

// ── enable ───────────────────────────────────────────────────────────────────

var scheduleEnableCmd = &cobra.Command{
	Use:               "enable <name>",
	Short:             "Enable a schedule",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeScheduleNames,
	RunE:              runScheduleEnable,
}

func runScheduleEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if _, err := svc.ScheduleSetEnabled(context.Background(), org, args[0], true); err != nil {
		return err
	}
	color.Green("Schedule %q enabled", args[0])
	return nil
}

// ── disable ──────────────────────────────────────────────────────────────────

var scheduleDisableCmd = &cobra.Command{
	Use:               "disable <name>",
	Short:             "Disable a schedule",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeScheduleNames,
	RunE:              runScheduleDisable,
}

func runScheduleDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	if _, err := svc.ScheduleSetEnabled(context.Background(), org, args[0], false); err != nil {
		return err
	}
	color.Green("Schedule %q disabled", args[0])
	return nil
}

// ── delete ───────────────────────────────────────────────────────────────────

var scheduleDeleteCmd = &cobra.Command{
	Use:               "delete <name>",
	Short:             "Delete a schedule and all its registered specs",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeScheduleNames,
	RunE:              runScheduleDelete,
}

func runScheduleDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	name := args[0]

	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(
		fmt.Sprintf("Delete schedule %q and all its registered specs? This cannot be undone.", name),
		force,
	)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled")
		return nil
	}

	if err := svc.ScheduleDelete(context.Background(), org, name); err != nil {
		return err
	}
	color.Green("Schedule %q deleted", name)
	return nil
}

// ── tasks subgroup ────────────────────────────────────────────────────────────

var scheduleTasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Manage registered task specs (org-wide, addressed by code)",
}

var scheduleTasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List task specs across the org (optionally filtered by schedule)",
	Long: `List all registered task specs for the organization.

Without --schedule, every spec in the org is shown; each row includes its
schedule_name column so the list is self-describing.

With --schedule <name>, only specs belonging to that schedule are returned
(server-side filtering; unknown schedule name returns an empty list).`,
	Args: cobra.NoArgs,
	RunE: runScheduleTasksList,
}

func runScheduleTasksList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	scheduleFilter, _ := cmd.Flags().GetString("schedule")
	tasks, err := svc.ScheduleTaskList(context.Background(), org, scheduleFilter)
	if err != nil {
		return err
	}
	return formatter.FormatScheduledTasks(tasks, len(tasks))
}

var scheduleTasksEnableCmd = &cobra.Command{
	Use:   "enable <code>",
	Short: "Enable a task spec by its code",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleTasksEnable,
}

func runScheduleTasksEnable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	code := args[0]
	spec, err := svc.ScheduledTaskSetEnabledByCode(context.Background(), org, code, true)
	if err != nil {
		return err
	}
	color.Green("Spec %s (schedule %q) enabled", code, spec.ScheduleName)
	return nil
}

var scheduleTasksDisableCmd = &cobra.Command{
	Use:   "disable <code>",
	Short: "Disable a task spec by its code",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleTasksDisable,
}

func runScheduleTasksDisable(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	code := args[0]
	spec, err := svc.ScheduledTaskSetEnabledByCode(context.Background(), org, code, false)
	if err != nil {
		return err
	}
	color.Green("Spec %s (schedule %q) disabled", code, spec.ScheduleName)
	return nil
}

var scheduleTasksRemoveCmd = &cobra.Command{
	Use:   "remove <code>",
	Short: "Remove a task spec by its code",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleTasksRemove,
}

func runScheduleTasksRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	code := args[0]

	// Fetch the spec descriptor so we can tailor the confirmation message.
	// If the fetch fails for any reason (e.g. server doesn't support GET-one,
	// or the code is about to 404 on delete anyway) we skip the kind check
	// and proceed with the generic message.
	ctx := context.Background()
	spec, _ := svc.ScheduledTaskGet(ctx, org, code)

	prompt := fmt.Sprintf("Remove spec %s? This cannot be undone.", code)
	if spec != nil && spec.Kind == "BACKUP" {
		color.Yellow("⚠  This is the org's BACKUP spec — removing it stops scheduled backups.")
		color.Yellow("   The backup config stays configured but won't run until re-attached to a schedule.")
		prompt = fmt.Sprintf("Remove BACKUP spec %s?", code)
	}

	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(prompt, force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled")
		return nil
	}

	if err := svc.ScheduledTaskRemoveByCode(ctx, org, code); err != nil {
		return err
	}
	color.Green("Spec %s removed", code)
	return nil
}

// ── wiring ───────────────────────────────────────────────────────────────────

func init() {
	// create flags — cadence only; no task-type/target/payload
	scheduleCreateCmd.Flags().String("name", "", "Unique schedule name (required)")
	scheduleCreateCmd.Flags().String("cron", "", "Cron expression, e.g. \"0 2 * * 0\" (required)")
	// --schedule is a deprecated alias for --cron; hidden after one release cycle
	scheduleCreateCmd.Flags().String("schedule", "", "Cron expression (deprecated alias for --cron)")
	_ = scheduleCreateCmd.Flags().MarkHidden("schedule")
	scheduleCreateCmd.Flags().String("timezone", "UTC", "IANA timezone for the cron expression")
	scheduleCreateCmd.Flags().Bool("disabled", false, "Create the schedule in disabled state (default: enabled)")
	_ = scheduleCreateCmd.MarkFlagRequired("name")
	_ = scheduleCreateCmd.MarkFlagRequired("cron")

	// delete flags
	scheduleDeleteCmd.Flags().Bool("force", false, "Skip the confirmation prompt (required when stdin is not a TTY)")

	// tasks list: optional schedule filter
	scheduleTasksListCmd.Flags().String("schedule", "", "Limit to specs belonging to this schedule name (optional)")
	_ = scheduleTasksListCmd.RegisterFlagCompletionFunc("schedule", completeScheduleNames)

	// tasks remove: force confirm
	scheduleTasksRemoveCmd.Flags().Bool("force", false, "Skip the confirmation prompt (required when stdin is not a TTY)")

	// tasks subgroup
	scheduleTasksCmd.AddCommand(
		scheduleTasksListCmd,
		scheduleTasksEnableCmd,
		scheduleTasksDisableCmd,
		scheduleTasksRemoveCmd,
	)

	scheduleCmd.AddCommand(
		scheduleListCmd,
		scheduleDescribeCmd,
		scheduleCreateCmd,
		scheduleEnableCmd,
		scheduleDisableCmd,
		scheduleDeleteCmd,
		scheduleTasksCmd,
	)
}

// completeScheduleNames provides shell completion for schedule names.
func completeScheduleNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	org := getOrganization()
	if org == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	result, err := svc.ScheduleList(context.Background(), org)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(result.Schedules))
	for _, s := range result.Schedules {
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
