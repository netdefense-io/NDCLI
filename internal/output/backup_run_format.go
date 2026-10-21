package output

import (
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// `ndcli run backup` answers with the tasks it created and the devices it left
// out. The skipped devices are half of the answer, not a footnote: a targeted
// device without backup enabled is not backed up, and the user has to see that
// next to the ones that were. The device UUID is omitted from the human
// formats, as in a run; `-f json` has it.

// backupRunHeader is the one-line summary every human format opens with.
func backupRunHeader(result *models.BackupRunResult) string {
	noun := "backup"
	if result.Total != 1 {
		noun = "backups"
	}
	header := fmt.Sprintf("Started %d %s in org %q", result.Total, noun, result.Organization)
	if n := len(result.Skipped); n > 0 {
		header += fmt.Sprintf(" — %d skipped", n)
	}
	return header
}

// backupSkipReason is the control plane's sentence for a skipped device, shown
// as sent. A code this build does not know is covered by the sentence beside
// it; the code stands in only if the sentence is missing.
func backupSkipReason(s models.BackupRunSkipped) string {
	if s.Reason != "" {
		return s.Reason
	}
	if s.Code != "" {
		return s.Code
	}
	return "-"
}

// backupSkippedHeading introduces the skipped list.
func backupSkippedHeading(n int) string {
	if n == 1 {
		return "1 device was not backed up"
	}
	return fmt.Sprintf("%d devices were not backed up", n)
}

// FormatBackupRunResult renders an on-demand backup as a header line, a table
// of the created tasks and a table of the skipped devices with their reasons.
func (f *TableFormatter) FormatBackupRunResult(result *models.BackupRunResult) error {
	f.Success(backupRunHeader(result))
	if len(result.Tasks) > 0 {
		table := NewStyledTable([]string{"Device", "Task", "Expires"})
		for _, t := range result.Tasks {
			table.Append([]string{t.DeviceName, t.Task, FormatTimestamp(parseRunTime(t.ExpiresAt))})
		}
		table.RenderTo(f.Writer)
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintln(f.Writer)
		f.Warning(backupSkippedHeading(len(result.Skipped)))
		table := NewStyledTable([]string{"Device", "Reason"})
		for _, s := range result.Skipped {
			table.Append([]string{s.DeviceName, backupSkipReason(s)})
		}
		table.RenderTo(f.Writer)
	}
	return nil
}

// FormatBackupRunResult renders an on-demand backup as a header line, one
// bullet per created task and one per skipped device.
func (f *SimpleFormatter) FormatBackupRunResult(result *models.BackupRunResult) error {
	f.Success(backupRunHeader(result))
	for _, t := range result.Tasks {
		fmt.Fprintf(f.Writer, "• %s → task %s (expires %s)\n",
			t.DeviceName, t.Task, FormatTimestamp(parseRunTime(t.ExpiresAt)))
	}
	for _, s := range result.Skipped {
		fmt.Fprintf(f.Writer, "• %s → skipped: %s\n", s.DeviceName, backupSkipReason(s))
	}
	return nil
}

// FormatBackupRunResult renders an on-demand backup with one box per backed-up
// device and one box listing the devices that were left out.
func (f *DetailedFormatter) FormatBackupRunResult(result *models.BackupRunResult) error {
	f.Success(backupRunHeader(result))
	for _, t := range result.Tasks {
		fmt.Fprintln(f.Writer)
		box := &fieldBox{title: t.DeviceName}
		box.field("Task", t.Task)
		box.field("Status", StatusIndicator(t.Status)+" "+ColoredStatus(t.Status))
		box.field("Expires", FormatTimestamp(parseRunTime(t.ExpiresAt)))
		box.render(f.BaseFormatter)
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintln(f.Writer)
		box := &fieldBox{title: "Not backed up"}
		for _, s := range result.Skipped {
			box.field(s.DeviceName, backupSkipReason(s))
		}
		box.render(f.BaseFormatter)
	}
	return nil
}

// FormatBackupRunResult formats an on-demand backup as JSON: the answer as
// NDManager sent it, with both lists always present.
func (f *JSONFormatter) FormatBackupRunResult(result *models.BackupRunResult) error {
	return f.output(result)
}
