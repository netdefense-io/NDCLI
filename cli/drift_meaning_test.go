package cli

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// DRIFT means NetDefense has changes not yet pushed; a change made on the
// device itself is not detected (operator decision, 2026-11-12). Every
// command that shows or filters drift says so.
func TestDriftHelpSaysWhatDriftMeans(t *testing.T) {
	for _, want := range []string{"IN_SYNC", "DRIFT", "NEVER_SYNCED", "UNKNOWN", "ERROR", "not yet pushed", "not detected"} {
		if !strings.Contains(models.DriftStatusMeaning, want) {
			t.Errorf("drift meaning does not say %q: %s", want, models.DriftStatusMeaning)
		}
	}
	for _, path := range [][]string{{"sync", "status"}, {"sync", "apply"}, {"device", "list"}} {
		if cmd := findCommand(t, path...); !strings.Contains(cmd.Long, models.DriftStatusMeaning) {
			t.Errorf("%s help does not say what drift means:\n%s", strings.Join(path, " "), cmd.Long)
		}
	}
}
