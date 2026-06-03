package output

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// scheduleEnabledLabel returns a display string for the enabled field.
func scheduleEnabledLabel(enabled bool) string {
	if enabled {
		return ColorEnabled.Sprint("enabled")
	}
	return ColorDisabled.Sprint("disabled")
}

// scheduleEnabledStatus returns a status string compatible with
// StatusWithIcon/ColoredStatus.
func scheduleEnabledStatus(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "DISABLED"
}

// scheduleNextRun formats next_run_at human-friendly.
func scheduleNextRun(sch *models.Schedule) string {
	if sch.NextRunAt == nil || sch.NextRunAt.IsZero() {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", FormatTimestamp(sch.NextRunAt.Time), RelativeTime(sch.NextRunAt.Time))
}

// scheduleLastFired formats last_fired_at human-friendly.
func scheduleLastFired(sch *models.Schedule) string {
	if sch.LastFiredAt == nil || sch.LastFiredAt.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%s (%s)", FormatTimestamp(sch.LastFiredAt.Time), RelativeTime(sch.LastFiredAt.Time))
}

// scheduledTaskRequestSummary returns a one-line summary of the raw request
// JSON stored in a ScheduledTask. It extracts the most useful fields without
// requiring a full parse of every possible shape.
func scheduledTaskRequestSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, 4)
	// RUN shape: type, targets
	if v, ok := m["type"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			parts = append(parts, "type="+s)
		}
	}
	if v, ok := m["targets"]; ok {
		var t struct {
			Devices []string `json:"devices"`
			OUs     []string `json:"ous"`
			All     bool     `json:"all"`
		}
		if json.Unmarshal(v, &t) == nil {
			if t.All {
				parts = append(parts, "target=all")
			} else {
				if len(t.Devices) > 0 {
					parts = append(parts, fmt.Sprintf("devices=[%s]", strings.Join(t.Devices, ",")))
				}
				if len(t.OUs) > 0 {
					parts = append(parts, fmt.Sprintf("ous=[%s]", strings.Join(t.OUs, ",")))
				}
			}
		}
	}
	// SYNC shape: organization, device, ou
	if v, ok := m["organization"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			parts = append(parts, "org="+s)
		}
	}
	if v, ok := m["device"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			parts = append(parts, "device="+s)
		}
	}
	if v, ok := m["ou"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			parts = append(parts, "ou="+s)
		}
	}
	if len(parts) == 0 {
		return string(raw)
	}
	return strings.Join(parts, " ")
}

// ── Table ────────────────────────────────────────────────────────────────────

// FormatSchedules formats a list of schedules as a table.
func (f *TableFormatter) FormatSchedules(schedules []models.Schedule, total int) error {
	if len(schedules) == 0 {
		f.Info("No schedules found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Status", "Cron", "Timezone", "Next Run", "Specs"})
	for _, s := range schedules {
		nextRun := "-"
		if s.NextRunAt != nil && !s.NextRunAt.IsZero() {
			nextRun = RelativeTimeShort(s.NextRunAt.Time)
		}
		table.Append([]string{
			s.Name,
			StatusWithIcon(scheduleEnabledStatus(s.Enabled)),
			s.Schedule,
			s.Timezone,
			nextRun,
			fmt.Sprintf("%d", len(s.ScheduledTasks)),
		})
	}
	table.Render()
	return nil
}

// FormatSchedule formats a single schedule (table/key-value), including any
// nested ScheduledTask specs.
func (f *TableFormatter) FormatSchedule(sch *models.Schedule) error {
	fmt.Fprintf(f.Writer, "Name:         %s\n", sch.Name)
	if sch.OrganizationName != "" {
		fmt.Fprintf(f.Writer, "Organization: %s\n", sch.OrganizationName)
	}
	fmt.Fprintf(f.Writer, "Status:       %s\n", scheduleEnabledLabel(sch.Enabled))
	fmt.Fprintf(f.Writer, "Cron:         %s\n", sch.Schedule)
	fmt.Fprintf(f.Writer, "Timezone:     %s\n", sch.Timezone)
	fmt.Fprintln(f.Writer)
	fmt.Fprintf(f.Writer, "Next Run:     %s\n", scheduleNextRun(sch))
	fmt.Fprintf(f.Writer, "Last Fired:   %s\n", scheduleLastFired(sch))
	fmt.Fprintf(f.Writer, "Created By:   %s\n", sch.CreatedBy)
	fmt.Fprintf(f.Writer, "Created:      %s\n", FormatTimestamp(sch.CreatedAt.Time))
	if !sch.UpdatedAt.IsZero() {
		fmt.Fprintf(f.Writer, "Updated:      %s\n", FormatTimestamp(sch.UpdatedAt.Time))
	}
	if len(sch.ScheduledTasks) > 0 {
		fmt.Fprintln(f.Writer)
		fmt.Fprintf(f.Writer, "Registered specs (%d):\n", len(sch.ScheduledTasks))
		for _, st := range sch.ScheduledTasks {
			status := scheduleEnabledLabel(st.Enabled)
			summary := scheduledTaskRequestSummary(st.Request)
			fmt.Fprintf(f.Writer, "  %s  [%s] %s  %s\n", st.Code, st.Kind, status, summary)
		}
	}
	return nil
}

// FormatScheduledTasks renders the org-wide list of task specs as a table.
// Each row shows schedule_name so the list is readable without pre-filtering.
func (f *TableFormatter) FormatScheduledTasks(tasks []models.ScheduledTask, total int) error {
	if len(tasks) == 0 {
		f.Info("No task specs found")
		return nil
	}
	table := NewStyledTable([]string{"Code", "Schedule", "Kind", "Status", "Request"})
	for _, t := range tasks {
		table.Append([]string{
			t.Code,
			t.ScheduleName,
			t.Kind,
			StatusWithIcon(scheduleEnabledStatus(t.Enabled)),
			scheduledTaskRequestSummary(t.Request),
		})
	}
	table.Render()
	return nil
}

// FormatScheduledTaskRegisterResultTable formats a spec registration result.
func (f *TableFormatter) FormatScheduledTaskRegisterResult(result *models.ScheduledTaskRegisterResult) error {
	f.Success(fmt.Sprintf("Registered spec %s on schedule %q", result.Code, result.ScheduleName))
	fmt.Fprintf(f.Writer, "Code:     %s\n", result.Code)
	fmt.Fprintf(f.Writer, "Kind:     %s\n", result.Kind)
	fmt.Fprintf(f.Writer, "Schedule: %s\n", result.ScheduleName)
	summary := scheduledTaskRequestSummary(result.Request)
	if summary != "" {
		fmt.Fprintf(f.Writer, "Request:  %s\n", summary)
	}
	return nil
}

// ── Simple ───────────────────────────────────────────────────────────────────

// FormatSchedules formats a list of schedules as simple bullet points.
func (f *SimpleFormatter) FormatSchedules(schedules []models.Schedule, total int) error {
	if len(schedules) == 0 {
		f.Info("No schedules found")
		return nil
	}

	for _, s := range schedules {
		nextRun := "-"
		if s.NextRunAt != nil && !s.NextRunAt.IsZero() {
			nextRun = RelativeTimeShort(s.NextRunAt.Time)
		}
		fmt.Fprintf(f.Writer, "• %s [%s] %s (%s) → next: %s\n",
			s.Name, scheduleEnabledLabel(s.Enabled), s.Schedule, s.Timezone, nextRun)
	}
	return nil
}

// FormatSchedule formats a single schedule (simple).
func (f *SimpleFormatter) FormatSchedule(sch *models.Schedule) error {
	fmt.Fprintf(f.Writer, "Schedule: %s\n", sch.Name)
	if sch.OrganizationName != "" {
		fmt.Fprintf(f.Writer, "  Org:       %s\n", sch.OrganizationName)
	}
	fmt.Fprintf(f.Writer, "  Status:    %s\n", scheduleEnabledLabel(sch.Enabled))
	fmt.Fprintf(f.Writer, "  Cron:      %s  (%s)\n", sch.Schedule, sch.Timezone)
	fmt.Fprintf(f.Writer, "  Next Run:  %s\n", scheduleNextRun(sch))
	fmt.Fprintf(f.Writer, "  Last Fired:%s\n", scheduleLastFired(sch))
	if len(sch.ScheduledTasks) > 0 {
		fmt.Fprintf(f.Writer, "  Specs (%d):\n", len(sch.ScheduledTasks))
		for _, st := range sch.ScheduledTasks {
			fmt.Fprintf(f.Writer, "    • %s [%s] %s  %s\n",
				st.Code, st.Kind, scheduleEnabledLabel(st.Enabled),
				scheduledTaskRequestSummary(st.Request))
		}
	}
	return nil
}

// FormatScheduledTasks renders the org-wide list of task specs (simple).
func (f *SimpleFormatter) FormatScheduledTasks(tasks []models.ScheduledTask, total int) error {
	if len(tasks) == 0 {
		f.Info("No task specs found")
		return nil
	}
	for _, t := range tasks {
		fmt.Fprintf(f.Writer, "• %s [%s/%s] %s  %s\n",
			t.Code, t.ScheduleName, t.Kind,
			scheduleEnabledLabel(t.Enabled),
			scheduledTaskRequestSummary(t.Request))
	}
	return nil
}

// FormatScheduledTaskRegisterResultSimple formats a spec registration result (simple).
func (f *SimpleFormatter) FormatScheduledTaskRegisterResult(result *models.ScheduledTaskRegisterResult) error {
	f.Success(fmt.Sprintf("Registered spec %s on schedule %q (%s)", result.Code, result.ScheduleName, result.Kind))
	return nil
}

// ── Detailed ─────────────────────────────────────────────────────────────────

// FormatSchedules formats a list of schedules with rich box drawing.
func (f *DetailedFormatter) FormatSchedules(schedules []models.Schedule, total int) error {
	if len(schedules) == 0 {
		f.Info("No schedules found")
		return nil
	}

	for i := range schedules {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		f.formatScheduleRich(&schedules[i])
	}
	return nil
}

// FormatSchedule formats a single schedule with rich box drawing.
func (f *DetailedFormatter) FormatSchedule(sch *models.Schedule) error {
	f.formatScheduleRich(sch)
	return nil
}

func (f *DetailedFormatter) formatScheduleRich(sch *models.Schedule) {
	const width = 60

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Schedule"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, sch.Name)
	padding := width - 5 - len(sch.Name)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	if sch.OrganizationName != "" {
		ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, sch.OrganizationName)
		orgPad := width - 5 - len(sch.OrganizationName)
		if orgPad < 0 {
			orgPad = 0
		}
		fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", orgPad), BoxVertical)
	}
	cronLine := sch.Schedule + "  " + sch.Timezone
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, cronLine)
	padding = width - 5 - len(cronLine)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s %s\n", "Status",
		StatusIndicator(scheduleEnabledStatus(sch.Enabled)),
		scheduleEnabledLabel(sch.Enabled))
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Next Run", scheduleNextRun(sch))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Last Fired", scheduleLastFired(sch))
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created By", sch.CreatedBy)
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(sch.CreatedAt.Time))
	if !sch.UpdatedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(sch.UpdatedAt.Time))
	}

	if len(sch.ScheduledTasks) > 0 {
		fmt.Fprintln(f.Writer)
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle(fmt.Sprintf("Registered Specs (%d)", len(sch.ScheduledTasks))))
		for _, st := range sch.ScheduledTasks {
			status := scheduleEnabledLabel(st.Enabled)
			line := fmt.Sprintf("%s  [%s]  %s  %s", st.Code, st.Kind, status, scheduledTaskRequestSummary(st.Request))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
		}
		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}
}

// FormatScheduledTasks renders the org-wide list of task specs (detailed).
func (f *DetailedFormatter) FormatScheduledTasks(tasks []models.ScheduledTask, total int) error {
	if len(tasks) == 0 {
		f.Info("No task specs found")
		return nil
	}
	for i, t := range tasks {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── Spec %s ───\n", t.Code)
		f.printLabelValue("Schedule", t.ScheduleName)
		f.printLabelValue("Kind", t.Kind)
		f.printLabel("Status")
		fmt.Fprintln(f.Writer, scheduleEnabledLabel(t.Enabled))
		summary := scheduledTaskRequestSummary(t.Request)
		if summary != "" {
			f.printLabelValue("Request", summary)
		}
		f.printLabelValue("Created By", t.CreatedBy)
		f.printLabelValue("Created", FormatTimestamp(t.CreatedAt.Time))
	}
	return nil
}

// FormatScheduledTaskRegisterResultDetailed formats a spec registration result (detailed).
func (f *DetailedFormatter) FormatScheduledTaskRegisterResult(result *models.ScheduledTaskRegisterResult) error {
	const width = 60
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Spec Registered"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, result.Code)
	padding := width - 5 - len(result.Code)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	subtitle := result.Kind + " on " + result.ScheduleName
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, subtitle)
	padding = width - 5 - len(subtitle)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Code", result.Code)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Kind", result.Kind)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Schedule", result.ScheduleName)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created By", result.CreatedBy)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(result.CreatedAt.Time))
	summary := scheduledTaskRequestSummary(result.Request)
	if summary != "" {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Request", summary)
	}
	return nil
}

// ── JSON ─────────────────────────────────────────────────────────────────────

// FormatSchedules formats a list of schedules as JSON.
func (f *JSONFormatter) FormatSchedules(schedules []models.Schedule, total int) error {
	return f.output(map[string]interface{}{
		"schedules": schedules,
		"total":     total,
	})
}

// FormatSchedule formats a single schedule as JSON.
func (f *JSONFormatter) FormatSchedule(sch *models.Schedule) error {
	return f.output(sch)
}

// FormatScheduledTasks formats the org-wide task spec list as JSON.
func (f *JSONFormatter) FormatScheduledTasks(tasks []models.ScheduledTask, total int) error {
	return f.output(map[string]interface{}{
		"tasks": tasks,
		"total": total,
	})
}

// FormatScheduledTaskRegisterResult formats a spec registration result as JSON.
func (f *JSONFormatter) FormatScheduledTaskRegisterResult(result *models.ScheduledTaskRegisterResult) error {
	return f.output(result)
}
