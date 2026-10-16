package resources

import (
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func firmwareTrackRef(s string) *string { return &s }

// A FIRMWARE spec's row says which track's window it is, in a column of its own;
// every other kind shows a dash there. The cells stay aligned with the columns.
func TestScheduledTaskRowNamesTheTrackOfAFirmwareSpec(t *testing.T) {
	columns := ScheduledTaskResource{}.Columns()
	track := -1
	for i, c := range columns {
		if c.Title == "TRACK" {
			track = i
		}
	}
	if track < 0 {
		t.Fatalf("no TRACK column in %v", columns)
	}

	for _, tc := range []struct {
		name string
		spec models.ScheduledTask
		want string
	}{
		{"firmware", models.ScheduledTask{Code: "Fw123456", Kind: models.ScheduledTaskKindFirmware, FirmwareTrack: firmwareTrackRef("fast"), Enabled: true}, "fast"},
		{"backup", models.ScheduledTask{Code: "Bk123456", Kind: models.ScheduledTaskKindBackup, Enabled: true}, "—"},
		{"firmware from a control plane that names no track", models.ScheduledTask{Code: "Fw654321", Kind: models.ScheduledTaskKindFirmware}, "—"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := scheduledTaskRow(tc.spec)
			if len(row.Cells) != len(columns) {
				t.Fatalf("%d cells for %d columns: %v", len(row.Cells), len(columns), row.Cells)
			}
			if row.ID != tc.spec.Code || row.Cells[0] != tc.spec.Code || row.Cells[1] != tc.spec.Kind {
				t.Errorf("row = %v", row)
			}
			if got := row.Cells[track]; got != tc.want {
				t.Errorf("TRACK cell = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScheduledTaskKindLabelNamesTheTrack(t *testing.T) {
	firmware := models.ScheduledTask{Kind: models.ScheduledTaskKindFirmware, FirmwareTrack: firmwareTrackRef("regular")}
	if got := scheduledTaskKindLabel(firmware); got != "FIRMWARE (regular track)" {
		t.Errorf("label = %q", got)
	}
	for _, spec := range []models.ScheduledTask{
		{Kind: models.ScheduledTaskKindRun},
		{Kind: models.ScheduledTaskKindFirmware},
	} {
		if got := scheduledTaskKindLabel(spec); got != spec.Kind {
			t.Errorf("label for %s without a track = %q, want the kind", spec.Kind, got)
		}
	}
}
