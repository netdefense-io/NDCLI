package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func runResultWith(runID, org string) *models.RunResult {
	return &models.RunResult{
		Type: "REBOOT", Organization: org, RunID: runID, Total: 1,
		Tasks: []models.RunTaskItem{{Task: "T1", DeviceName: "fw1", Status: "PENDING", ExpiresAt: "2030-05-12T03:05:00Z"}},
	}
}

// renderRun renders result in every human format.
func renderRun(t *testing.T, result *models.RunResult) map[string]string {
	t.Helper()
	restore := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = restore })

	table := captureStdout(t, func() {
		if err := NewTableFormatter().FormatRunResult(result); err != nil {
			t.Errorf("table: %v", err)
		}
	})
	var simple, detailed bytes.Buffer
	if err := (&SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &simple}}).FormatRunResult(result); err != nil {
		t.Fatalf("simple: %v", err)
	}
	if err := (&DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &detailed}}).FormatRunResult(result); err != nil {
		t.Fatalf("detailed: %v", err)
	}
	return map[string]string{"table": table, "simple": simple.String(), "detailed": detailed.String()}
}

func TestRunResultShowsTheRunCode(t *testing.T) {
	scheduled := runResultWith("Rn4kT8qW", "acme")
	scheduled.ScheduledAt = "2030-05-12T03:00:00Z"
	for _, tc := range []struct {
		name   string
		result *models.RunResult
		header string
	}{
		{"now", runResultWith("Rn4kT8qW", "acme"), `✓ Created 1 REBOOT task in org "acme" (run Rn4kT8qW)`},
		{"scheduled", scheduled, `✓ Scheduled 1 REBOOT task in org "acme" (run Rn4kT8qW) — fires at `},
	} {
		for format, out := range renderRun(t, tc.result) {
			if !strings.HasPrefix(out, tc.header) {
				t.Errorf("%s/%s does not open with %q:\n%s", tc.name, format, tc.header, out)
			}
			if !strings.Contains(out, "List the run's tasks with: ndcli task list --run Rn4kT8qW -o acme\n") {
				t.Errorf("%s/%s lacks the follow-up command:\n%s", tc.name, format, out)
			}
		}
	}
}

// An older control plane sends no run code, and the output stays as it was.
func TestRunResultWithoutARunCodeIsUnchanged(t *testing.T) {
	for format, out := range renderRun(t, runResultWith("", "acme")) {
		if !strings.HasPrefix(out, `✓ Created 1 REBOOT task in org "acme"`+"\n") {
			t.Errorf("%s header changed:\n%s", format, out)
		}
		if strings.Contains(out, "run ") || strings.Contains(out, "--run") {
			t.Errorf("%s mentions a run:\n%s", format, out)
		}
	}
}

// The follow-up is a command the user may paste, so only a run code and an
// organization name of the server's own shape go into it.
func TestRunResultFollowUpTakesOnlyWellFormedValues(t *testing.T) {
	for _, tc := range []struct {
		name, runID, org string
		header           bool
	}{
		{"run code of another shape", "x; rm -rf ~", "acme", false},
		{"organization of another shape", "Rn4kT8qW", "acme; rm -rf ~", true},
	} {
		for format, out := range renderRun(t, runResultWith(tc.runID, tc.org)) {
			if strings.Contains(out, "--run") {
				t.Errorf("%s/%s prints a follow-up:\n%s", tc.name, format, out)
			}
			if got := strings.Contains(out, "(run Rn4kT8qW)"); got != tc.header {
				t.Errorf("%s/%s: run code in the header = %v, want %v:\n%s", tc.name, format, got, tc.header, out)
			}
		}
	}
}

func TestRunResultJSONCarriesTheRunCode(t *testing.T) {
	for _, tc := range []struct {
		runID string
		want  bool
	}{{"Rn4kT8qW", true}, {"", false}} {
		var buf bytes.Buffer
		if err := (&JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}).FormatRunResult(runResultWith(tc.runID, "acme")); err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("not one JSON document: %v\n%s", err, buf.String())
		}
		got, present := doc["run_id"]
		if present != tc.want || (tc.want && got != tc.runID) {
			t.Errorf("run_id %q: got %v (present %v)", tc.runID, got, present)
		}
	}
}
