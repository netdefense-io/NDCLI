package output

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseFirmwareUpgradeData_Nil(t *testing.T) {
	for _, raw := range []string{"", "plain text", `{"message": "hello"}`} {
		got := parseFirmwareUpgradeData(raw)
		if got != nil {
			t.Errorf("parseFirmwareUpgradeData(%q): expected nil, got %+v", raw, got)
		}
	}
}

func TestParseFirmwareUpgradeData_Valid(t *testing.T) {
	raw := `{"resolved_mode":"minor","from_version":"26.1.2","to_version":"26.1.9","reboot_performed":true,"applied":true,"no_update":false,"packages_applied":116,"mixed_state":false}`
	got := parseFirmwareUpgradeData(raw)
	if got == nil {
		t.Fatal("expected non-nil firmware data")
	}
	if got.ResolvedMode != "minor" {
		t.Errorf("ResolvedMode: got %q, want %q", got.ResolvedMode, "minor")
	}
	if got.FromVersion != "26.1.2" {
		t.Errorf("FromVersion: got %q, want %q", got.FromVersion, "26.1.2")
	}
	if got.ToVersion != "26.1.9" {
		t.Errorf("ToVersion: got %q, want %q", got.ToVersion, "26.1.9")
	}
	if !got.RebootPerformed {
		t.Error("RebootPerformed: want true")
	}
	if !got.Applied {
		t.Error("Applied: want true")
	}
	if got.MixedState {
		t.Error("MixedState: want false")
	}
	if got.PackagesApplied != 116 {
		t.Errorf("PackagesApplied: got %d, want 116", got.PackagesApplied)
	}
}

func TestParseFirmwareUpgradeData_MixedState(t *testing.T) {
	raw := `{"resolved_mode":"minor","from_version":"26.1.2","to_version":"26.1.9","reboot_performed":false,"applied":true,"packages_applied":116,"mixed_state":true}`
	got := parseFirmwareUpgradeData(raw)
	if got == nil {
		t.Fatal("expected non-nil firmware data")
	}
	if !got.MixedState {
		t.Error("MixedState: want true")
	}
	if got.RebootPerformed {
		t.Error("RebootPerformed: want false")
	}
}

func TestParseFirmwareUpgradeData_NoUpdate(t *testing.T) {
	raw := `{"resolved_mode":"minor","from_version":"26.1.9","no_update":true}`
	got := parseFirmwareUpgradeData(raw)
	if got == nil {
		t.Fatal("expected non-nil firmware data")
	}
	if !got.NoUpdate {
		t.Error("NoUpdate: want true")
	}
}

func TestFormatFirmwareDataLines_NoUpdate(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode: "minor",
		FromVersion:  "26.1.9",
		NoUpdate:     true,
	}
	lines := formatFirmwareDataLines(d)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for no-op, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "no update") {
		t.Errorf("expected 'no update' in line, got %q", lines[0])
	}
}

func TestFormatFirmwareDataLines_MixedState(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode:    "minor",
		FromVersion:     "26.1.2",
		ToVersion:       "26.1.9",
		Applied:         true,
		PackagesApplied: 116,
		MixedState:      true,
		RebootPerformed: false,
	}
	lines := formatFirmwareDataLines(d)
	var hasMixed bool
	for _, l := range lines {
		if strings.Contains(l, "Mixed") || strings.Contains(l, "mixed") || strings.Contains(l, "base/kernel") {
			hasMixed = true
		}
	}
	if !hasMixed {
		t.Errorf("expected mixed-state line, got: %v", lines)
	}
}

func TestFormatFirmwareDataLines_NilReturnsNil(t *testing.T) {
	if got := formatFirmwareDataLines(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// renderTableBlock simulates the table formatter: header + two-space indent.
func renderTableBlock(d *firmwareUpgradeData) string {
	lines := formatFirmwareDataLines(d)
	var sb strings.Builder
	sb.WriteString("Firmware upgrade result:\n")
	for _, l := range lines {
		fmt.Fprintf(&sb, "  %s\n", l)
	}
	return sb.String()
}

// TestFormatFirmwareDataLines_DryRunMinor covers the case where the agent ran
// in dry-run mode: nothing installed, 66 packages would be applied.
// Expected table block (verbatim):
//
//	Firmware upgrade result:
//	  Preview:  yes (dry-run — nothing was installed)
//	  Mode:     minor
//	  Version:  26.1.2 → 26.1.9
//	  Packages: 66 to apply
func TestFormatFirmwareDataLines_DryRunMinor(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode:    "minor",
		FromVersion:     "26.1.2",
		ToVersion:       "26.1.9",
		Applied:         false,
		DryRun:          true,
		PackagesApplied: 66,
		RebootPerformed: false,
	}
	got := renderTableBlock(d)
	want := "Firmware upgrade result:\n" +
		"  Preview:  yes (dry-run — nothing was installed)\n" +
		"  Mode:     minor\n" +
		"  Version:  26.1.2 → 26.1.9\n" +
		"  Packages: 66 to apply\n"
	if got != want {
		t.Errorf("dry-run minor: output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
	// Must not contain "Applied:" or "Rebooted:" in dry-run output.
	if strings.Contains(got, "Applied:") {
		t.Error("dry-run minor: output must not contain 'Applied:' line")
	}
	if strings.Contains(got, "Rebooted:") {
		t.Error("dry-run minor: output must not contain 'Rebooted:' line")
	}
}

// TestFormatFirmwareDataLines_NoRebootMixedState covers the --no-reboot path:
// packages were applied but base/kernel updates are pending a reboot.
// Expected table block (verbatim):
//
//	Firmware upgrade result:
//	  Mode:     minor
//	  Version:  26.1.2 → 26.1.9
//	  Packages: 116 applied
//	  Pending:  base/kernel deferred — reboot required to complete upgrade
//	  Rebooted: no
//	  Applied:  yes
func TestFormatFirmwareDataLines_NoRebootMixedState(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode:    "minor",
		FromVersion:     "26.1.2",
		ToVersion:       "26.1.9",
		Applied:         true,
		DryRun:          false,
		PackagesApplied: 116,
		MixedState:      true,
		RebootPerformed: false,
	}
	got := renderTableBlock(d)
	want := "Firmware upgrade result:\n" +
		"  Mode:     minor\n" +
		"  Version:  26.1.2 → 26.1.9\n" +
		"  Packages: 116 applied\n" +
		"  Pending:  base/kernel deferred — reboot required to complete upgrade\n" +
		"  Rebooted: no\n" +
		"  Applied:  yes\n"
	if got != want {
		t.Errorf("no-reboot mixed-state: output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatFirmwareDataLines_FullMinorWithReboot covers a complete minor upgrade
// that triggered a reboot.
// Expected table block (verbatim):
//
//	Firmware upgrade result:
//	  Mode:     minor
//	  Version:  26.1.2 → 26.1.9
//	  Packages: 116 applied
//	  Rebooted: yes
//	  Applied:  yes
func TestFormatFirmwareDataLines_FullMinorWithReboot(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode:    "minor",
		FromVersion:     "26.1.2",
		ToVersion:       "26.1.9",
		Applied:         true,
		DryRun:          false,
		PackagesApplied: 116,
		MixedState:      false,
		RebootPerformed: true,
	}
	got := renderTableBlock(d)
	want := "Firmware upgrade result:\n" +
		"  Mode:     minor\n" +
		"  Version:  26.1.2 → 26.1.9\n" +
		"  Packages: 116 applied\n" +
		"  Rebooted: yes\n" +
		"  Applied:  yes\n"
	if got != want {
		t.Errorf("full minor with reboot: output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatFirmwareDataLines_VersionArrow(t *testing.T) {
	d := &firmwareUpgradeData{
		ResolvedMode:    "minor",
		FromVersion:     "26.1.2",
		ToVersion:       "26.1.9",
		Applied:         true,
		PackagesApplied: 116,
		RebootPerformed: true,
	}
	lines := formatFirmwareDataLines(d)
	var hasArrow bool
	for _, l := range lines {
		if strings.Contains(l, "26.1.2") && strings.Contains(l, "26.1.9") && strings.Contains(l, "→") {
			hasArrow = true
		}
	}
	if !hasArrow {
		t.Errorf("expected version arrow line, got: %v", lines)
	}
}
