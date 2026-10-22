package output

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The sync apply headline says what happened: ✗ when every device was
// refused (the command then fails), ✓ when any device was triggered or the
// devices were only skipped.
func TestSyncApplyHeadlineFollowsTheOutcome(t *testing.T) {
	origOut, origNoColor := color.Output, color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.Output, color.NoColor = origOut, origNoColor })

	refused := []models.SyncError{{DeviceName: "fw-a", Error: "device offline"}, {DeviceName: "fw-b", Error: "device offline"}}
	for _, tc := range []struct {
		name string
		resp *models.SyncApplyResponse
		want string
	}{
		{"every device refused", &models.SyncApplyResponse{
			Message: "Sync triggered for 0 device(s); 2 error(s)", Errors: refused,
		}, "✗ Sync triggered for 0 device(s); 2 error(s)"},
		{"some triggered", &models.SyncApplyResponse{
			Message: "Sync triggered for 1 device(s); 2 error(s)", DevicesAffected: 1,
			Tasks: []models.SyncTaskResult{{Task: "T1", DeviceName: "fw-c"}}, Errors: refused,
		}, "✓ Sync triggered for 1 device(s); 2 error(s)"},
		{"only skipped", &models.SyncApplyResponse{
			Message: "Sync triggered for 0 device(s); 3 skipped (already synced)", Skipped: 3,
		}, "✓ Sync triggered for 0 device(s); 3 skipped (already synced)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outputs := map[string]string{
				"table": captureStdout(t, func() {
					color.Output = os.Stdout
					if err := (&TableFormatter{}).FormatSyncApply(tc.resp); err != nil {
						t.Errorf("table: %v", err)
					}
				}),
			}
			var buf bytes.Buffer
			if err := (&DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}).FormatSyncApply(tc.resp); err != nil {
				t.Fatalf("detailed: %v", err)
			}
			outputs["detailed"] = buf.String()

			for name, out := range outputs {
				if headline := strings.SplitN(out, "\n", 2)[0]; headline != tc.want {
					t.Errorf("%s headline = %q, want %q", name, headline, tc.want)
				}
			}
		})
	}
}
