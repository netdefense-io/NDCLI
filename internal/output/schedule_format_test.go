package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func trackName(s string) *string { return &s }

// scheduleSpecsSample is the three kinds of spec a schedule can carry that
// matter here: two FIRMWARE windows (one disabled, the widest line the detailed
// box has to hold) and a BACKUP spec, whose firmware_track is null.
func scheduleSpecsSample() []models.ScheduledTask {
	return []models.ScheduledTask{
		{Code: "Fw111111", ScheduleName: "night", Kind: models.ScheduledTaskKindFirmware, FirmwareTrack: trackName("fast"),
			Request: json.RawMessage(`{}`), Enabled: true, CreatedBy: "admin@acme.com"},
		{Code: "Fw222222", ScheduleName: "night", Kind: models.ScheduledTaskKindFirmware, FirmwareTrack: trackName("regular"),
			Request: json.RawMessage(`{}`), Enabled: false, CreatedBy: "admin@acme.com"},
		{Code: "Bk333333", ScheduleName: "night", Kind: models.ScheduledTaskKindBackup,
			Request: json.RawMessage(`{"type":"BACKUP"}`), Enabled: true, CreatedBy: "admin@acme.com"},
	}
}

func scheduleWithSpecsSample() *models.Schedule {
	return &models.Schedule{
		OrganizationName: "acme", Name: "night", Enabled: true, Schedule: "0 2 * * 6", Timezone: "UTC",
		CreatedBy: "admin@acme.com", ScheduledTasks: scheduleSpecsSample(),
	}
}

// A FIRMWARE spec reads as the window of its track wherever specs are listed:
// the org-wide list and the specs nested in a schedule, in every human format.
func TestScheduleSpecsNameTheirTrack(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format+"/list", func(t *testing.T) {
			out := render(t, format, "FormatScheduledTasks", scheduleSpecsSample(), 3)
			mustContain(t, format, out, "Fw111111", "fast track window", "Fw222222", "regular track window", "Bk333333")
			mustNotContain(t, format, out, "{}", "firmware track window")
		})
		t.Run(format+"/schedule", func(t *testing.T) {
			out := render(t, format, "FormatSchedule", scheduleWithSpecsSample())
			mustContain(t, format, out, "Fw111111", "fast track window", "Fw222222", "regular track window", "Bk333333")
			mustNotContain(t, format, out, "{}", "firmware track window")
		})
	}
}

// A control plane that predates the field sends a FIRMWARE spec with no track:
// it keeps the plain wording, and never "<empty> track window".
func TestFirmwareSpecWithoutATrackKeepsThePlainWording(t *testing.T) {
	specs := scheduleSpecsSample()[:1]
	specs[0].FirmwareTrack = nil
	for _, format := range []string{"table", "simple", "detailed"} {
		out := render(t, format, "FormatScheduledTasks", specs, 1)
		mustContain(t, format, out, "firmware track window")
		mustNotContain(t, format, out, "{}")
	}
	if got := scheduledTaskSummary(models.ScheduledTaskKindFirmware, "", nil); got != "firmware track window" {
		t.Errorf("summary = %q", got)
	}
	// The track belongs to FIRMWARE specs only; any other kind shows its request.
	if got := scheduledTaskSummary(models.ScheduledTaskKindBackup, "fast", json.RawMessage(`{"type":"BACKUP"}`)); !strings.Contains(got, "type=BACKUP") {
		t.Errorf("a BACKUP spec's summary = %q, want its request", got)
	}
}

// -f json carries firmware_track on every spec: the track for a FIRMWARE spec,
// null for any other kind, never missing and never an empty string.
func TestScheduleSpecsCarryFirmwareTrackInJSON(t *testing.T) {
	check := func(t *testing.T, specs []interface{}) {
		t.Helper()
		if len(specs) != 3 {
			t.Fatalf("specs = %d, want 3", len(specs))
		}
		for i, want := range []interface{}{"fast", "regular", nil} {
			spec := specs[i].(map[string]interface{})
			got, present := spec["firmware_track"]
			if !present {
				t.Errorf("spec %v has no firmware_track key", spec["code"])
			}
			if got != want {
				t.Errorf("spec %v firmware_track = %v, want %v", spec["code"], got, want)
			}
		}
	}
	t.Run("the org-wide list", func(t *testing.T) {
		var doc struct {
			Tasks []interface{} `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(render(t, "json", "FormatScheduledTasks", scheduleSpecsSample(), 3)), &doc); err != nil {
			t.Fatal(err)
		}
		check(t, doc.Tasks)
	})
	t.Run("a schedule's nested specs", func(t *testing.T) {
		var doc struct {
			Tasks []interface{} `json:"scheduled_tasks"`
		}
		if err := json.Unmarshal([]byte(render(t, "json", "FormatSchedule", scheduleWithSpecsSample())), &doc); err != nil {
			t.Fatal(err)
		}
		check(t, doc.Tasks)
	})
}

// The detailed schedule view draws its specs in a box of fixed width that does
// not wrap, so the longest firmware row has to fit: every row of the box is as
// wide as its top edge.
func TestDetailedScheduleBoxKeepsItsBorderAroundFirmwareSpecs(t *testing.T) {
	out := render(t, "detailed", "FormatSchedule", scheduleWithSpecsSample())
	lines := strings.Split(out, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "Registered Specs (3)") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no specs box in:\n%s", out)
	}
	width := len([]rune(lines[start]))
	rows := 0
	for _, line := range lines[start+1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		rows++
		if got := len([]rune(line)); got != width {
			t.Errorf("box row is %d columns, its top edge %d: %q", got, width, line)
		}
	}
	if rows < 4 { // three specs and the bottom edge
		t.Errorf("only %d rows after the title:\n%s", rows, out)
	}
}
