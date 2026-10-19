package output

import (
	"fmt"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The task fields NDManager 1.99 and 1.100 added, laid out once for every human
// format: where a task came from, who asked for it and from which client, its
// run, how its device stands while it waits, and when it was sent, finished
// and answered. A helper returns "" (or no fields) for what the server did not
// send, so a task from an older control plane renders as it always did.

// taskField is one "Label: value" line of a task's details.
type taskField struct {
	label, value string
}

func stringOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// taskDeviceCell is the device as a list shows it: its name, marked when the
// device was deleted or, for a waiting task, when it is offline.
func taskDeviceCell(t *models.Task) string {
	switch {
	case t.DeviceDeleted:
		return t.DeviceName + " (deleted)"
	case t.DeviceOnline != nil && !*t.DeviceOnline:
		return t.DeviceName + " (offline)"
	}
	return t.DeviceName
}

// taskDeviceLine is the device as a describe shows it: for a waiting task also
// whether its device is connected, and when it was last seen.
func taskDeviceLine(t *models.Task) string {
	name := t.DeviceName
	if t.DeviceDeleted {
		return name + " (deleted)"
	}
	seen := ""
	if t.DeviceLastSeenAt != nil {
		seen = "last seen " + FormatTimestamp(*t.DeviceLastSeenAt)
	}
	switch {
	case t.DeviceOnline != nil && *t.DeviceOnline:
		return name + " (online)"
	case t.DeviceOnline != nil && seen != "":
		return name + " (offline, " + seen + ")"
	case t.DeviceOnline != nil:
		return name + " (offline)"
	case seen != "":
		return name + " (" + seen + ")"
	}
	return name
}

// taskRequesterEmail is the requester as a list names it: the email, which the
// requested_by filter takes, else the name, or "Deleted account" once the
// account is gone (both null).
func taskRequesterEmail(t *models.Task) string {
	if t.RequestedBy == nil {
		return ""
	}
	if email := stringOf(t.RequestedBy.Email); email != "" {
		return email
	}
	if name := stringOf(t.RequestedBy.Name); name != "" {
		return name
	}
	return "Deleted account"
}

// taskRequesterLine is the requester as a describe names it, with the
// account's name when it has one: "Ana Lima <ana@acme.com>".
func taskRequesterLine(t *models.Task) string {
	if t.RequestedBy == nil {
		return ""
	}
	email, name := stringOf(t.RequestedBy.Email), stringOf(t.RequestedBy.Name)
	switch {
	case email == "" && name == "":
		return "Deleted account"
	case email == "":
		return name
	case name == "":
		return email
	}
	return name + " <" + email + ">"
}

func taskTrack(t *models.Task) string {
	if t.Schedule == nil {
		return ""
	}
	return stringOf(t.Schedule.FirmwareTrack)
}

// taskSourceCell is where a task came from, in a few words for a list: who
// asked and from which client, the schedule, or the firmware track. "" when
// the control plane does not say.
func taskSourceCell(t *models.Task) string {
	switch t.Origin {
	case "":
		return ""
	case models.TaskOriginManual:
		who, client := taskRequesterEmail(t), stringOf(t.Client)
		switch {
		case who != "" && client != "":
			return who + " · " + client
		case who != "":
			return who
		case client != "":
			return "Manual · " + client
		}
		return "Manual"
	case models.TaskOriginSchedule:
		if t.Schedule != nil && t.Schedule.Name != "" {
			return "Schedule " + t.Schedule.Name
		}
		return "Schedule"
	case models.TaskOriginFirmware:
		if track := taskTrack(t); track != "" {
			return track + " track"
		}
		return "Firmware track"
	}
	return t.Origin
}

// taskSourceLine is the Source line of a describe: the kind of source, with the
// track of a firmware step; the schedule and the requester get lines of their
// own (taskProvenanceFields).
func taskSourceLine(t *models.Task) string {
	switch t.Origin {
	case models.TaskOriginManual:
		if t.RequestedBy == nil {
			return "Manual (who started it is not recorded)"
		}
		return "Manual"
	case models.TaskOriginSchedule:
		return "Schedule"
	case models.TaskOriginFirmware:
		if track := taskTrack(t); track != "" {
			return "Firmware track " + track
		}
		return "Firmware track"
	}
	return t.Origin
}

// taskProvenanceFields are the lines a describe adds after the organization:
// the source, the schedule spec behind it, who asked and from where, and the
// run.
func taskProvenanceFields(t *models.Task) []taskField {
	var fields []taskField
	add := func(label, value string) {
		if value != "" {
			fields = append(fields, taskField{label, value})
		}
	}
	add("Source", taskSourceLine(t))
	if s := t.Schedule; s != nil {
		add("Schedule", s.Name)
		spec := stringOf(s.SpecCode)
		switch {
		case spec != "" && s.Kind != "":
			spec += " (" + s.Kind + ")"
		case spec == "":
			spec = s.Kind
		}
		add("Spec", spec)
		next := "disabled"
		if s.NextRunAt != nil {
			next = FormatTimestamp(*s.NextRunAt)
		}
		add("Next run", next)
	}
	add("Requested by", taskRequesterLine(t))
	add("Client", stringOf(t.Client))
	add("Run", stringOf(t.RunID))
	return fields
}

// taskRunFields are the times a describe adds after Created, Scheduled and
// Expires: when the task was sent and finished, how long that took, and when
// the device's answer was recorded.
func taskRunFields(t *models.Task) []taskField {
	var fields []taskField
	if t.DispatchedAt != nil {
		fields = append(fields, taskField{"Dispatched", FormatTimestamp(*t.DispatchedAt)})
	}
	if t.CompletedAt != nil {
		fields = append(fields, taskField{"Completed", FormatTimestamp(*t.CompletedAt)})
	}
	if d := taskDurationText(t); d != "" {
		fields = append(fields, taskField{"Duration", d})
	}
	if t.LastResponseAt != nil {
		fields = append(fields, taskField{"Responded", FormatTimestamp(*t.LastResponseAt)})
	}
	return fields
}

// taskDurationText is how long a task took, from its dispatch to its
// completion ("3s", "2m 05s", "1h 02m", "3d 04h"), or "" when that is not
// known.
func taskDurationText(t *models.Task) string {
	d, ok := t.Duration()
	if !ok {
		return ""
	}
	return formatTaskDuration(d)
}

func formatTaskDuration(d time.Duration) string {
	s := int64(d.Round(time.Second) / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	case s < 86400:
		return fmt.Sprintf("%dh %02dm", s/3600, (s%3600)/60)
	}
	return fmt.Sprintf("%dd %02dh", s/86400, (s%86400)/3600)
}

func anyTaskSource(tasks []models.Task) bool {
	for i := range tasks {
		if tasks[i].Origin != "" {
			return true
		}
	}
	return false
}

func anyTaskDuration(tasks []models.Task) bool {
	for i := range tasks {
		if _, ok := tasks[i].Duration(); ok {
			return true
		}
	}
	return false
}

// jsonTask is a task as `-f json` prints it: the task as the control plane sent
// it, plus three keys of NDCLI's own:
//
//   - duration_seconds, from dispatched_at to completed_at, null when either
//     is unknown;
//   - started_at, the same instant as dispatched_at. NDManager never sent a
//     started_at, so this key used to be null on every task; it now carries
//     the dispatch for the scripts that read it, and dispatched_at is the name
//     to use;
//   - firmware_track_result, for a completed FIRMWARE_TRACK step: its parsed
//     result beside the raw message, the way the MCP task tools return it.
//     Absent, never null, on any other task.
type jsonTask struct {
	models.Task
	StartedAt           *time.Time                  `json:"started_at"`
	DurationSeconds     *int64                      `json:"duration_seconds"`
	FirmwareTrackResult *models.FirmwareTrackResult `json:"firmware_track_result,omitempty"`
}

func newJSONTask(task *models.Task) jsonTask {
	return jsonTask{
		Task:                *task,
		StartedAt:           task.DispatchedAt,
		DurationSeconds:     task.DurationSeconds(),
		FirmwareTrackResult: firmwareTrackResultFor(task),
	}
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// shortTaskID is the code as a list shows it; task codes are 8 characters, and
// a longer one is cut there.
func shortTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
