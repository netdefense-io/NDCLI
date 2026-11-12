package output

import (
	"math"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Seconds from the control plane are capped before they become a duration, so
// a huge soak or major-upgrade wait reads as a long time, never as "now".
func TestFirmwareSecondsDoNotWrap(t *testing.T) {
	huge := math.MaxInt64
	for name, got := range map[string]string{
		"soak left":    firmwareSoakLeft(&models.FirmwareCandidate{SoakRemainingSeconds: &huge}),
		"seconds left": firmwareSecondsLeft(&huge),
	} {
		if got == "now" || got == "over" || !strings.HasSuffix(got, "d") && !strings.Contains(got, "d ") {
			t.Errorf("%s with %d seconds = %q, want days", name, huge, got)
		}
	}
	two := 7200
	if got := firmwareSecondsLeft(&two); got != "2h" {
		t.Errorf("7200 seconds = %q, want 2h", got)
	}
}
