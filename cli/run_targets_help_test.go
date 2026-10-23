package cli

import (
	"strings"
	"testing"
)

// TestRunHelpSaysAllStandsAlone: the run help called the targets the UNION of
// --device, --ou and --all, but the commands refuse --all with either of the
// others ("--all cannot be combined with --device or --ou").
func TestRunHelpSaysAllStandsAlone(t *testing.T) {
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	run := findCommand(t, "run")
	if strings.Contains(strings.ToUpper(run.Long), "UNION") {
		t.Errorf("run help still calls the targets a union:\n%s", run.Long)
	}
	for _, want := range []string{
		"--all Target every enabled device in the org. Use it alone.",
		"--device and --ou combine: the targets are the named devices and the enabled devices of the named OUs, each device once. --all stands alone and cannot be combined with --device or --ou.",
	} {
		if !strings.Contains(flat(run.Long), want) {
			t.Errorf("run help lacks %q:\n%s", want, run.Long)
		}
	}

	backup := findCommand(t, "run", "backup")
	for _, want := range []string{
		"--all Back up every device in the org that has backup enabled. Use it alone.",
		"--device and --ou combine; --all cannot be combined with either.",
	} {
		if !strings.Contains(flat(backup.Long), want) {
			t.Errorf("run backup help lacks %q:\n%s", want, backup.Long)
		}
	}

	for _, sub := range run.Commands() {
		all := sub.Flags().Lookup("all")
		if all == nil {
			continue
		}
		if !strings.HasSuffix(all.Usage, "(not with --device or --ou)") {
			t.Errorf("run %s --all usage = %q, want it to say it stands alone", sub.Name(), all.Usage)
		}
	}
}
