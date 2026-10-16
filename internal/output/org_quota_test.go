package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func orgQuotaSample(backup, remoteAdmin, firmware bool) *models.OrgQuota {
	return &models.OrgQuota{
		Organization: "acme-corp",
		Plan:         &models.OrgPlan{Name: "team", DisplayName: "Team"},
		Devices:      models.Quota{Limit: 100, Used: 12, Available: 88},
		Users:        models.Quota{Limit: 10, Used: 3, Available: 7},
		VpnNetworks:  models.Quota{Limit: 5, Used: 1, Available: 4},
		Snippets:     models.Quota{Unlimited: true, Used: 20},

		BackupEnabled:         backup,
		RemoteAdminEnabled:    remoteAdmin,
		FirmwareTracksEnabled: firmware,
	}
}

// The plan features are listed together: firmware tracks sit right after
// Backup and Remote Admin, enabled or not, since the commands are there either way.
func TestOrgQuotaListsFirmwareTracksNextToTheOtherPlanFeatures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		firmware bool
		want     string
	}{
		{"included", true, "Enabled"},
		{"not included", false, "Disabled"},
	} {
		for _, format := range []string{"table", "simple", "detailed"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				out := render(t, format, "FormatOrgQuota", orgQuotaSample(true, false, tc.firmware))

				var features []string
				for _, line := range strings.Split(out, "\n") {
					for _, label := range []string{"Backup", "Remote Admin", "Firmware Tracks"} {
						if strings.Contains(line, label) {
							features = append(features, strings.Join(strings.Fields(line), " "))
						}
					}
				}
				if len(features) != 3 {
					t.Fatalf("want one line each for Backup, Remote Admin and Firmware Tracks, got %q in:\n%s", features, out)
				}
				for i, label := range []string{"Backup", "Remote Admin", "Firmware Tracks"} {
					if !strings.Contains(features[i], label) {
						t.Errorf("feature %d = %q, want %s (Backup, Remote Admin, Firmware Tracks in that order)", i, features[i], label)
					}
				}
				if firmware := features[2]; !strings.HasSuffix(firmware, tc.want) {
					t.Errorf("%q should end with %q", firmware, tc.want)
				}
				// The three features are consecutive lines of one block.
				lines := strings.Split(out, "\n")
				for i, line := range lines {
					if strings.Contains(line, "Remote Admin") && !strings.Contains(lines[i+1], "Firmware Tracks") {
						t.Errorf("Firmware Tracks should follow Remote Admin, got %q", lines[i+1])
					}
				}
			})
		}
	}
}

func TestOrgQuotaJSONCarriesTheFirmwareTracksFlag(t *testing.T) {
	for _, firmware := range []bool{true, false} {
		out := render(t, "json", "FormatOrgQuota", orgQuotaSample(true, false, firmware))
		var doc map[string]interface{}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("not one JSON document: %v\n%s", err, out)
		}
		for key, want := range map[string]bool{"backup_enabled": true, "remoteadmin_enabled": false, "firmware_tracks_enabled": firmware} {
			if got, ok := doc[key]; !ok || got != want {
				t.Errorf("%s = %v (present: %v), want %v", key, got, ok, want)
			}
		}
	}
}
