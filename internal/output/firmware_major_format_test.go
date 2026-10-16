package output

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The views of the major upgrade, in the three human formats (JSON is the
// model, covered by TestFirmwareJSONIsTheModel). The sentences below are written
// out here, not read from the code that builds them.

func TestFirmwareTrackShowsTheMajorSwitch(t *testing.T) {
	cfg := firmwareConfigSample()
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			on := render(t, format, "FormatFirmwareTrack", &cfg.Tracks[0])
			mustContain(t, format+" on", on, "Major upgrades", "on: the track's windows move its devices to the next OPNsense series")
			off := render(t, format, "FormatFirmwareTrack", &cfg.Tracks[1])
			mustContain(t, format+" off", off, "off: the track's windows ask none of its devices to upgrade")
			mustNotContain(t, format+" off", off, "on: the track's windows")
		})
	}
	// The tables and the lines carry it too.
	mustContain(t, "tracks table", render(t, "table", "FormatFirmwareTracks", cfg.Tracks), "Major Upgrades")
	mustContain(t, "config simple", render(t, "simple", "FormatFirmwareConfig", cfg), "major upgrades on", "major upgrades off")
	mustContain(t, "config table", render(t, "table", "FormatFirmwareConfig", cfg), "Major Upgrades")

	t.Run("a write that switched it off says what it cancelled", func(t *testing.T) {
		track := cfg.Tracks[1]
		track.CancelledTasks = intRef(2)
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareTrack", &track), "2 queued major-upgrade steps cancelled")
		}
		track.CancelledTasks = intRef(1)
		mustContain(t, "one", render(t, "table", "FormatFirmwareTrack", &track), "1 queued major-upgrade step cancelled")
		track.CancelledTasks = intRef(0)
		mustNotContain(t, "none", render(t, "table", "FormatFirmwareTrack", &track), "cancelled")
	})
	t.Run("a control plane that predates it says nothing, not that it is off", func(t *testing.T) {
		legacy := firmwareLegacyConfig()
		for _, format := range []string{"table", "simple", "detailed"} {
			for _, method := range []string{"FormatFirmwareConfig", "FormatFirmwareTrack"} {
				var arg interface{} = legacy
				if method == "FormatFirmwareTrack" {
					arg = &legacy.Tracks[0]
				}
				out := render(t, format, method, arg)
				mustNotContain(t, format+" "+method, out, "Major Upgrades", "Major upgrades", "major upgrades on", "major upgrades off", "next OPNsense series")
			}
			mustNotContain(t, format+" tracks", render(t, format, "FormatFirmwareTracks", legacy.Tracks), "Major Upgrades", "major upgrades")
		}
	})
}

func TestFirmwareAssignmentShowsTheBootEnvironmentSetting(t *testing.T) {
	setting := &models.FirmwareAssignment{OrganizationName: "acme", CancelledTasks: 1, Results: []models.FirmwareAssignmentResult{
		{DeviceName: "fw-a", Result: "updated", Track: "fast", State: "managed", BootEnvironment: boolRef(false), Notes: []string{}},
		{DeviceName: "fw-b", Result: "unchanged", Track: "regular", State: "managed", BootEnvironment: boolRef(false), Notes: []string{}},
	}}
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatFirmwareAssignment", setting)
			// No track was asked for, so none is named; the setting is what changed.
			mustContain(t, format, out, "2 devices: boot-environment setting changed on 1, 1 unchanged", "fw-a", "fw-b", "updated", "off", "1 pending firmware step cancelled")
			mustNotContain(t, format, out, "→", "(,", "()")
		})
	}
	mustContain(t, "table", render(t, "table", "FormatFirmwareAssignment", setting), "Boot Env")
	mustContain(t, "simple", render(t, "simple", "FormatFirmwareAssignment", setting), "• fw-a: updated (fast, managed), boot environment off")
	mustContain(t, "detailed", render(t, "detailed", "FormatFirmwareAssignment", setting), "Boot environment")

	t.Run("beside a track both are said", func(t *testing.T) {
		both := &models.FirmwareAssignment{OrganizationName: "acme", Track: "regular", Results: []models.FirmwareAssignmentResult{
			{DeviceName: "fw-c", Result: "added", Track: "regular", State: "enrolling", BootEnvironment: boolRef(true), Notes: []string{}}}}
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareAssignment", both), "1 device → regular track: 1 added", "fw-c")
		}
	})
	t.Run("a control plane that predates the setting shows no column for it", func(t *testing.T) {
		old := &models.FirmwareAssignment{OrganizationName: "acme", Track: "fast", Results: []models.FirmwareAssignmentResult{
			{DeviceName: "fw-a", Result: "added", Track: "fast", State: "enrolling", Notes: []string{}}}}
		mustNotContain(t, "table", render(t, "table", "FormatFirmwareAssignment", old), "Boot Env")
		mustNotContain(t, "simple", render(t, "simple", "FormatFirmwareAssignment", old), "boot environment")
		mustNotContain(t, "detailed", render(t, "detailed", "FormatFirmwareAssignment", old), "Boot environment")
	})
}

func TestFirmwareDevicesShowTheMajorUpgrade(t *testing.T) {
	list := firmwareDevicesMajorSample()
	t.Run("table", func(t *testing.T) {
		out := render(t, "table", "FormatFirmwareDevices", list)
		for _, header := range []string{"Series", "Major", "Boot Env"} {
			mustContain(t, "table", out, header)
		}
		mustContain(t, "table", out,
			"eligible", "blocked",
			"on (last: made)", "off (last: skipped, no room)",
			"the last major upgrade found a third-party repository and a third-party package on the device",
			"major upgrade blocked by the last hold", // the block is the hold the Held Reason column shows
			"major upgrade blocked: the device is not on the final release of its series yet",
		)
		// The boot-environment tokens are not repeated as warnings: the column says them.
		mustNotContain(t, "table", out, "no boot environment was made", "boot_environment_skipped_no_room")
	})
	t.Run("simple", func(t *testing.T) {
		out := render(t, "simple", "FormatFirmwareDevices", list)
		mustContain(t, "simple", out,
			"• fw-a: fast/managed, pinned 26.1.11_10 (series 26.1)", "major upgrade eligible", "boot environment on",
			"boot environment off (last: skipped, no room)",
			"    note: the last major upgrade found a third-party repository and a third-party package on the device\n",
			"    note: major upgrade blocked by the last hold\n",
			"    note: major upgrade blocked: the device is not on the final release of its series yet\n",
		)
	})
	t.Run("detailed", func(t *testing.T) {
		out := render(t, "detailed", "FormatFirmwareDevices", list)
		mustContain(t, "detailed", out,
			"Series: 26.1", "Major upgrade: eligible at the next window",
			"Major upgrade: blocked by the last hold",
			"Major upgrade: blocked: the device is not on the final release of its series yet",
			"Boot environment: on", "Boot environment: off; skipped at the last major upgrade: the pool had no room",
			"Boot environment: on; made before the last major upgrade",
			"Note: the last major upgrade found a third-party repository and a third-party package on the device",
		)
	})
	t.Run("one third-party finding names just that", func(t *testing.T) {
		for token, want := range map[string]string{
			"third_party_repository": "the last major upgrade found a third-party repository on the device",
			"third_party_package":    "the last major upgrade found a third-party package on the device",
		} {
			one := &models.FirmwareDeviceList{Total: 1, Items: []models.FirmwareDevice{
				{DeviceName: "fw-a", DeviceStatus: "ENABLED", Track: "fast", State: "managed", Notes: []string{token}}}}
			mustContain(t, token, render(t, "simple", "FormatFirmwareDevices", one), want)
		}
	})
	t.Run("a note this build does not know shows as its own words", func(t *testing.T) {
		one := &models.FirmwareDeviceList{Total: 1, Items: []models.FirmwareDevice{
			{DeviceName: "fw-a", DeviceStatus: "ENABLED", Track: "fast", State: "managed", Notes: []string{"third_party_firmware", "boot_environment_from_the_future"}}}}
		out := render(t, "simple", "FormatFirmwareDevices", one)
		mustContain(t, "unknown", out, "note: third_party_firmware")
		mustNotContain(t, "unknown", out, "boot_environment_from_the_future") // a boot-environment token is the column's
	})
	t.Run("a control plane that predates the major upgrade shows what it always showed", func(t *testing.T) {
		legacy := firmwareDevicesSample()
		table := render(t, "table", "FormatFirmwareDevices", legacy)
		mustNotContain(t, "table", table, "Series", "Boot Env", " Major ", "major upgrade")
		for _, headers := range tableHeaders(table) {
			if strings.Join(headers, "|") != "Device|Track|State|Pinned|Held Reason|Last Result|Last Run|Notes" {
				t.Errorf("headers = %v", headers)
			}
		}
		mustNotContain(t, "simple", render(t, "simple", "FormatFirmwareDevices", legacy), "series", "boot environment", "major upgrade")
		mustNotContain(t, "detailed", render(t, "detailed", "FormatFirmwareDevices", legacy), "Series", "Boot environment", "Major upgrade")
	})
	t.Run("strings outside the sets are shown as sent, never an error", func(t *testing.T) {
		odd := &models.FirmwareDeviceList{Total: 1, Items: []models.FirmwareDevice{{
			DeviceName: "fw-a", DeviceStatus: "ENABLED", Track: "fast", State: "managed", Series: "27.1",
			Major:           &models.FirmwareDeviceMajor{Eligible: false, BlockedBy: "needs_a_coffee"},
			BootEnvironment: &models.FirmwareBootEnvironment{Setting: true, Last: "from_the_future"},
		}}}
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareDevices", odd), "needs_a_coffee", "from_the_future", "27.1")
		}
	})
}

func TestFirmwareHistoryShowsTheKind(t *testing.T) {
	h := firmwareHistoryMajorSample()
	out := render(t, "table", "FormatFirmwareHistory", h)
	mustContain(t, "table", out, "Kind", "major", "minor", "26.7", "26.1.11_10 → 26.7.1_1")
	simple := render(t, "simple", "FormatFirmwareHistory", h)
	mustContain(t, "simple", simple, "(fast, major upgrade to 26.7)", "26.1.11_10 → 26.7.1_1")
	mustNotContain(t, "simple minor", strings.Split(simple, "\n")[2], "major upgrade")
	detailed := render(t, "detailed", "FormatFirmwareHistory", h)
	mustContain(t, "detailed", detailed, "Kind: major (the upgrade to the next OPNsense series)", "Kind: minor (an ordinary update)", "Target: 26.7")

	t.Run("a ledger of ordinary updates is what it was", func(t *testing.T) {
		legacy := firmwareHistorySample()
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareHistory", legacy)
			mustNotContain(t, format, out, "Kind", "major upgrade")
		}
		for _, headers := range tableHeaders(render(t, "table", "FormatFirmwareHistory", legacy)) {
			if strings.Join(headers, "|") != "When|Track|Action|Outcome|Reason|Target|Versions|Applied|Notes" {
				t.Errorf("headers = %v", headers)
			}
		}
	})
	t.Run("a minor ledger that names its kind shows no column, a major row makes one", func(t *testing.T) {
		minors := firmwareHistorySample()
		for i := range minors.Items {
			minors.Items[i].Kind = "minor"
		}
		mustNotContain(t, "minors", render(t, "table", "FormatFirmwareHistory", minors), "Kind")
		mustContain(t, "minors detailed", render(t, "detailed", "FormatFirmwareHistory", minors), "Kind: minor (an ordinary update)")
	})
}

func TestFirmwareRolloutShowsTheMajorUpgrades(t *testing.T) {
	r := firmwareRolloutMajorSample()

	t.Run("table", func(t *testing.T) {
		out := render(t, "table", "FormatFirmwareRollout", r)
		mustContain(t, "table", out,
			"Major upgrades:", "26.1 → 26.7", "approved", "waiting for evidence", "path closed",
			"on", "off",
			"stalled: the previous track does not apply major upgrades, so no evidence can come from it",
			"1 device waiting for the ordinary window that records the final release",
			"2 devices whose mirror does not offer the upgrade yet",
			"the upgrade payload was replaced upstream after this track approved the series",
			"blocked: OPNsense's upgrade path is closed (the control plane has not seen its files on the mirror lately)",
			"OPNsense gives 26.1 no more updates while the upgrade path from it is open",
			"Failures blocking a major upgrade:", "fw-b",
			"clear it with 'ndcli firmware failure clear <series> --major'",
			"NetDefense repository: https://repo.netdefense.io/dev/opnsense: FreeBSD:14:amd64 ok, FreeBSD:15:amd64 missing",
		)
		// The device counts, one column each.
		for _, header := range []string{"Eligible", "On Series", "Upgraded", "Apply"} {
			mustContain(t, "table", out, header)
		}
		// The end of life of the source series is said once, not once per track.
		if n := strings.Count(out, "OPNsense gives 26.1 no more updates"); n != 1 {
			t.Errorf("end of life said %d times, want once", n)
		}
	})
	t.Run("simple", func(t *testing.T) {
		out := render(t, "simple", "FormatFirmwareRollout", r)
		mustContain(t, "simple", out,
			"  Major upgrades:",
			"fast: 26.1 → 26.7 approved", "(auto)", "apply on; 2 eligible, 3 on the series, 1 upgraded",
			"regular: 26.1 → 26.7 waiting for evidence, soak left 2h, evidence 0/1; apply off",
			"note: stalled: the previous track does not apply major upgrades, so no evidence can come from it",
			"stable: 26.1 → 26.7 path closed",
			"  Failures blocking a major upgrade:", "• 26.7 in fast: 1 failure — fw-b",
			"  NetDefense repository: https://repo.netdefense.io/dev/opnsense: FreeBSD:14:amd64 ok, FreeBSD:15:amd64 missing",
		)
	})
	t.Run("detailed", func(t *testing.T) {
		out := render(t, "detailed", "FormatFirmwareRollout", r)
		mustContain(t, "detailed", out,
			"Major upgrade fast: 26.1 → 26.7", "Major upgrade regular: 26.1 → 26.7", "Major upgrade stable: 26.1 → 26.7",
			"Apply: on: the track's windows move its devices to the next OPNsense series",
			"Apply: off: the track's windows ask none of its devices to upgrade",
			"Approved: ", "(auto)", "Path opened: ",
			"Soak ends: ", "Evidence: 0 of 1 devices of the previous track",
			"Devices: 2 eligible, 3 on the series, 1 upgraded",
			"Failure blocking major upgrade 26.7", "Clear: 'ndcli firmware failure clear 26.7 --major'",
			"NetDefense repository", "FreeBSD:15:amd64 missing (checked ",
		)
	})

	t.Run("the switch being off says nothing of an approval an entry has not got", func(t *testing.T) {
		r := &models.FirmwareRollout{OrganizationName: "acme-corp", Status: "ENABLED", PlanEnabled: true, Majors: []models.FirmwareMajorEntry{
			{Track: "fast", Series: "26.7", SourceSeries: "26.1", State: "soaking", AutoApplyMajor: false, SoakRemainingSeconds: intRef(3600)},
			{Track: "regular", Series: "26.7", SourceSeries: "26.1", State: "path_closed", AutoApplyMajor: false, BlockedReason: "path_closed"},
		}}
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareRollout", r)
			mustContain(t, format, out, "soaking", "path closed")
			// The words of an approval belong to an entry that is approved.
			mustNotContain(t, format, strings.ReplaceAll(out, "not approved", ""), "approved")
		}
		mustContain(t, "detailed", render(t, "detailed", "FormatFirmwareRollout", r), "Apply: off: the track's windows ask none of its devices to upgrade")
	})
	t.Run("approved with the switch off is approved, not applied", func(t *testing.T) {
		r := firmwareRolloutMajorSample()
		r.Majors = []models.FirmwareMajorEntry{{Track: "fast", Series: "26.7", SourceSeries: "26.1", State: "approved", AutoApplyMajor: false}}
		for _, format := range []string{"table", "simple"} {
			mustContain(t, format, render(t, format, "FormatFirmwareRollout", r), "approved, not applied")
		}
		r.Majors[0].AutoApplyMajor = true
		mustNotContain(t, "applied", render(t, "table", "FormatFirmwareRollout", r), "not applied")
	})
	t.Run("a failure in the line points at the command that clears it", func(t *testing.T) {
		r := firmwareRolloutMajorSample()
		r.Majors = []models.FirmwareMajorEntry{{Track: "regular", Series: "26.7", SourceSeries: "26.1", State: "blocked_by_failure",
			Stalled: true, StallReason: "blocked_by_failure"}}
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareRollout", r), "blocked by a failure",
				"a failed major upgrade blocks it", "ndcli firmware failure clear 26.7 --major")
		}
	})
	t.Run("a stall for want of evidence with no cause named says what the minors say", func(t *testing.T) {
		r := firmwareRolloutMajorSample()
		r.Majors = []models.FirmwareMajorEntry{{Track: "regular", Series: "26.7", State: "waiting_for_evidence", Stalled: true, StallReason: "evidence_unreachable"}}
		mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", r), "too few devices in the previous track can still reach it")
		// With no source series the move is just the target.
		mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", r), "│ 26.7")
	})
	t.Run("every blocked reason reads as its sentence", func(t *testing.T) {
		for _, code := range models.FirmwareMajorBlockedReasons {
			r := firmwareRolloutMajorSample()
			r.Majors = []models.FirmwareMajorEntry{{Track: "fast", Series: "26.7", State: "approved", BlockedReason: code}}
			out := render(t, "simple", "FormatFirmwareRollout", r)
			mustContain(t, code, out, "note: blocked: "+models.FirmwareMajorBlockedReasonText(code))
		}
	})
	t.Run("strings outside the sets are shown as sent, never an error", func(t *testing.T) {
		r := firmwareRolloutMajorSample()
		r.Majors = []models.FirmwareMajorEntry{{Track: "fast", Series: "27.1", SourceSeries: "26.7", State: "from_the_future", Stalled: true,
			StallReason: "meteor", BlockedReason: "cosmic_rays", ApprovalSource: "oracle", ApprovedAt: "2026-10-01T00:00:00Z", Notice: "weather"}}
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatFirmwareRollout", r), "from_the_future", "meteor", "cosmic_rays", "weather", "27.1")
		}
		mustContain(t, "simple", render(t, "simple", "FormatFirmwareRollout", r), "oracle")
	})
	t.Run("the repository check says what each state means", func(t *testing.T) {
		for _, tc := range []struct {
			repo *models.FirmwareAgentRepo
			want string
		}{
			{&models.FirmwareAgentRepo{Configured: false, Trees: []models.FirmwareAgentRepoTree{}},
				"NetDefense repository: not checked: the control plane has no channel configured for it"},
			{&models.FirmwareAgentRepo{Configured: true, Channel: "https://repo.example/dev", Trees: []models.FirmwareAgentRepoTree{}},
				"NetDefense repository: https://repo.example/dev: no result in the last 30 minutes"},
			{&models.FirmwareAgentRepo{Configured: true, Trees: []models.FirmwareAgentRepoTree{{ABI: "FreeBSD:15:amd64", OK: true}}},
				"NetDefense repository: the configured channel: FreeBSD:15:amd64 ok"},
		} {
			r := firmwareRolloutSample()
			r.AgentRepo = tc.repo
			mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", r), tc.want)
		}
	})
	t.Run("a control plane that predates the major upgrade shows the board it always showed", func(t *testing.T) {
		legacy := firmwareRolloutSample()
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatFirmwareRollout", legacy)
			mustNotContain(t, format, out, "Major upgrade", "major upgrade", "NetDefense repository", "Failures blocking a major")
		}
	})
	t.Run("a board with majors and no failure of one says so", func(t *testing.T) {
		r := firmwareRolloutMajorSample()
		r.MajorFailures = nil
		mustContain(t, "table", render(t, "table", "FormatFirmwareRollout", r), "Major upgrade failures: none")
	})
}

func TestFirmwareHaltsPromotionsAndClearsOfMajorUpgrades(t *testing.T) {
	series := firmwareSeriesHaltSample()
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			halt := render(t, format, "FormatFirmwareHalt", &models.FirmwareHaltResult{Halt: series, CancelledTasks: 2})
			mustContain(t, format+" halt", halt, "Major upgrade to 26.7 halted: no track will approve it",
				"devices already upgrading are left alone and devices already on the series stay on it", "2 pending steps cancelled", "the payload changed")
			mustNotContain(t, format+" halt", halt, "Release")

			lifted := series
			lifted.UnhaltedAt, lifted.UnhaltedBy = "2026-10-10T12:00:00Z", "ops@acme.com"
			unhalt := render(t, format, "FormatFirmwareHalt", &models.FirmwareHaltResult{Halt: lifted})
			mustContain(t, format+" unhalt", unhalt, "Halt on major upgrade to 26.7 lifted: it competes for the tracks again")

			promo := render(t, format, "FormatFirmwarePromotion", &models.FirmwarePromotion{Series: "26.7", Results: []models.FirmwarePromotionResult{
				{Track: "fast", Result: "approved", Target: "26.7"}}})
			mustContain(t, format+" promote", promo, "Major upgrade to 26.7 approved in fast without waiting for its soak",
				"a track with major upgrades on upgrades its eligible devices at its next window")
			again := render(t, format, "FormatFirmwarePromotion", &models.FirmwarePromotion{Series: "26.7", Results: []models.FirmwarePromotionResult{
				{Track: "fast", Result: "already_approved", Target: "26.7"}}})
			mustContain(t, format+" promote again", again, "Major upgrade to 26.7 was already approved in every track asked")

			cleared := render(t, format, "FormatFirmwareFailuresCleared", &models.FirmwareFailuresCleared{Series: "26.7", Track: "fast", Cleared: 2})
			mustContain(t, format+" cleared", cleared, "Cleared 2 failures blocking the major upgrade to 26.7 in the fast track; the records stay in the history")
			none := render(t, format, "FormatFirmwareFailuresCleared", &models.FirmwareFailuresCleared{Series: "26.7", Cleared: 0})
			mustContain(t, format+" cleared none", none, "No recorded failure was blocking the major upgrade to 26.7 in every track")
		})
	}

	t.Run("a major promotion labels its third column Series, a release's keeps Target Now", func(t *testing.T) {
		major := &models.FirmwarePromotion{Series: "26.7", Results: []models.FirmwarePromotionResult{
			{Track: "fast", Result: "approved", Target: "26.7"}, {Track: "regular", Result: "already_approved", Target: "26.7"}}}
		release := &models.FirmwarePromotion{Version: "26.7.5", Results: []models.FirmwarePromotionResult{
			{Track: "fast", Result: "approved", Target: "26.7.5"}}}
		for _, headers := range tableHeaders(render(t, "table", "FormatFirmwarePromotion", major)) {
			if strings.Join(headers, "|") != "Track|Result|Series" {
				t.Errorf("major headers = %v", headers)
			}
		}
		for _, headers := range tableHeaders(render(t, "table", "FormatFirmwarePromotion", release)) {
			if strings.Join(headers, "|") != "Track|Result|Target Now" {
				t.Errorf("release headers = %v", headers)
			}
		}
		simple := render(t, "simple", "FormatFirmwarePromotion", major)
		mustContain(t, "simple major", simple, "• fast: approved now (series 26.7)", "• regular: already approved (series 26.7)")
		mustNotContain(t, "simple major", simple, "target now")
		mustContain(t, "simple release", render(t, "simple", "FormatFirmwarePromotion", release), "• fast: approved now (target now 26.7.5)")
		detailed := render(t, "detailed", "FormatFirmwarePromotion", major)
		mustContain(t, "detailed major", detailed, "Series: 26.7")
		mustNotContain(t, "detailed major", detailed, "Target now")
		mustContain(t, "detailed release", render(t, "detailed", "FormatFirmwarePromotion", release), "Target now: 26.7.5")
	})
	t.Run("a list that holds both kinds says which is which", func(t *testing.T) {
		list := &models.FirmwareHaltList{Total: 2, Items: []models.FirmwareHalt{firmwareHaltSample(), series}}
		table := render(t, "table", "FormatFirmwareHalts", list)
		mustContain(t, "table", table, "Kind", "Target", "minor", "major", "26.7.5", "26.7", "the payload changed")
		simple := render(t, "simple", "FormatFirmwareHalts", list)
		mustContain(t, "simple", simple, "• release 26.7.5 [in force]", "• major upgrade to 26.7 [in force]")
		detailed := render(t, "detailed", "FormatFirmwareHalts", list)
		mustContain(t, "detailed", detailed, "Halt on 26.7.5", "Halt on major upgrade to 26.7", "Status: in force", "the payload changed")
	})
	t.Run("a list of releases is what it was", func(t *testing.T) {
		list := &models.FirmwareHaltList{Total: 1, Items: []models.FirmwareHalt{firmwareHaltSample()}}
		out := render(t, "table", "FormatFirmwareHalts", list)
		mustNotContain(t, "table", out, "Kind", "Target")
		mustContain(t, "table", out, "Release", "26.7.5")
	})
	t.Run("a release's answers are what they were", func(t *testing.T) {
		mustContain(t, "release", render(t, "table", "FormatFirmwarePromotion", &models.FirmwarePromotion{Version: "26.7.5",
			Results: []models.FirmwarePromotionResult{{Track: "fast", Result: "approved", Target: "26.7.5"}}}),
			"Release 26.7.5 approved in fast without waiting for its soak")
		mustNotContain(t, "release", render(t, "table", "FormatFirmwarePromotion", &models.FirmwarePromotion{Version: "26.7.5",
			Results: []models.FirmwarePromotionResult{{Track: "fast", Result: "approved", Target: "26.7.5"}}}), "major")
		mustContain(t, "release", render(t, "table", "FormatFirmwareFailuresCleared", &models.FirmwareFailuresCleared{Version: "26.7.5", Cleared: 1}),
			"Cleared 1 failure blocking release 26.7.5 in every track")
	})
}

const majorApplied = `{"action":"advance","track":"fast","outcome":"success","reason":"applied","drift":false,"reinstall_entries":0,` +
	`"from_version":"26.1.11_10","to_version":"26.7.1_1","target_version":"26.7","pinned_version":"26.7.1_1","kind":"major",` +
	`"notes":["boot_environment_made","third_party_repository","third_party_package"]}`

const majorHeld = `{"action":"advance","track":"fast","outcome":"held","reason":"held_insufficient_space","drift":false,"reinstall_entries":0,` +
	`"from_version":"26.1.11_10","to_version":"26.1.11_10","target_version":"26.7","pinned_version":"26.1.11_10","kind":"major","notes":["boot_environment_off"]}`

func TestFormatTaskRendersTheMajorUpgradeResult(t *testing.T) {
	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			out := render(t, format, "FormatTask", firmwareTrackTask(majorApplied))
			mustContain(t, format, out,
				"advance on the fast track", "success", "26.1.11_10 → 26.7.1_1",
				"major upgrade to the 26.7 series",
				"a ZFS boot environment was made before the upgrade",
				"a package repository from outside OPNsense and NetDefense is enabled on the device",
				"a package from outside OPNsense and NetDefense is installed on the device",
			)
			held := render(t, format, "FormatTask", firmwareTrackTask(majorHeld))
			mustContain(t, format+" held", held, "the device has too little free disk space for the major upgrade (held_insufficient_space)",
				"major upgrade to the 26.7 series", "no boot environment was made: the device's setting is off")
		})
	}
	t.Run("a minor result says nothing of it", func(t *testing.T) {
		out := render(t, "table", "FormatTask", firmwareTrackTask(advanceApplied))
		mustNotContain(t, "table", out, "major upgrade", "Note:")
	})
	t.Run("a token this build does not know is shown as itself", func(t *testing.T) {
		raw := strings.Replace(majorApplied, `"boot_environment_made"`, `"boot_environment_from_the_future"`, 1)
		mustContain(t, "unknown", render(t, "table", "FormatTask", firmwareTrackTask(raw)), "Note:      boot_environment_from_the_future")
	})
	t.Run("a list says it is a major upgrade", func(t *testing.T) {
		task := firmwareTrackTask(majorApplied)
		task.ID = "aaaaaaaa1111"
		for _, format := range []string{"table", "simple", "detailed"} {
			mustContain(t, format, render(t, format, "FormatTasks", []models.Task{*task}, 1), "advance success 26.1.11_10 → 26.7.1_1 (major upgrade)")
		}
		held := firmwareTrackTask(majorHeld)
		held.ID = "bbbbbbbb2222"
		mustContain(t, "held", render(t, "table", "FormatTasks", []models.Task{*held}, 1),
			"advance held 26.1.11_10 (unchanged) — the device has too little free disk space for the major upgrade (major upgrade)")
	})
	t.Run("an escape sequence in a note or the kind never reaches the terminal", func(t *testing.T) {
		const esc = `\u001b`
		hostile := strings.Replace(strings.Replace(majorApplied, `"kind":"major"`, `"kind":"ma`+esc+`[2Jjor"`, 1),
			`"third_party_package"`, `"third_`+esc+`]0;pwn\u0007party"`, 1)
		for _, format := range []string{"table", "simple", "detailed"} {
			out := render(t, format, "FormatTask", firmwareTrackTask(hostile))
			if hasControl(out) {
				t.Errorf("%s: a control sequence reached the output: %q", format, firstControlContext(out))
			}
		}
	})
	t.Run("json carries the parsed kind and notes", func(t *testing.T) {
		out := render(t, "json", "FormatTask", firmwareTrackTask(majorApplied))
		mustContain(t, "json", out, `"kind": "major"`, `"boot_environment_made"`, `"third_party_repository"`, `"target_version": "26.7"`)
	})
}

func TestFirmwareTrackResultLinesOfAMajorUpgrade(t *testing.T) {
	r := models.ParseFirmwareTrackResult(majorApplied)
	got := strings.Join(firmwareTrackResultLines(r), "\n")
	want := strings.Join([]string{
		"Action:    advance on the fast track",
		"Outcome:   success",
		"Reason:    the update was applied (applied)",
		"Version:   26.1.11_10 → 26.7.1_1",
		"Target:    26.7",
		"Kind:      major upgrade to the 26.7 series",
		"Pinned:    26.7.1_1",
		"Note:      a ZFS boot environment was made before the upgrade",
		"Note:      a package repository from outside OPNsense and NetDefense is enabled on the device",
		"Note:      a package from outside OPNsense and NetDefense is installed on the device",
		"Drift:     no",
	}, "\n")
	if got != want {
		t.Errorf("lines:\n%s\nwant:\n%s", got, want)
	}
}
