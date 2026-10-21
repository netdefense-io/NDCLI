package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// backupRunSample is an on-demand backup that started two devices and left two
// out: one with backup off, one with a backup already running.
func backupRunSample() *models.BackupRunResult {
	return &models.BackupRunResult{
		Type:         "BACKUP",
		Organization: "acme-corp",
		Total:        2,
		Tasks: []models.RunTaskItem{
			{Task: "tQSd9RCV", DeviceUUID: "550e8400-e29b-41d4-a716-446655440000", DeviceName: "fw-branch-01", Status: "PENDING", ExpiresAt: "2026-10-20T18:21:07Z"},
			{Task: "hK3mZp0a", DeviceUUID: "11111111-2222-3333-4444-555555555555", DeviceName: "fw-branch-03", Status: "PENDING", ExpiresAt: "2026-10-20T18:21:08Z"},
		},
		Skipped: []models.BackupRunSkipped{
			{DeviceUUID: "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", DeviceName: "fw-branch-02", Code: "BACKUP_NOT_ENABLED", Reason: "Backup is not enabled for this device"},
			{DeviceUUID: "99999999-8888-7777-6666-555555555555", DeviceName: "fw-branch-04", Code: "BACKUP_IN_FLIGHT", Reason: "A backup of this device is already waiting to be sent or running"},
		},
	}
}

func TestBackupRunTable(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	fs, bufs := plainFormatters(t)

	if err := fs["table"].FormatBackupRunResult(backupRunSample()); err != nil {
		t.Fatal(err)
	}

	want := `✓ Started 2 backups in org "acme-corp" — 2 skipped
╭──────────────┬──────────┬─────────────────────╮
│ Device       │ Task     │ Expires             │
├──────────────┼──────────┼─────────────────────┤
│ fw-branch-01 │ tQSd9RCV │ 2026-10-20 18:21:07 │
│ fw-branch-03 │ hK3mZp0a │ 2026-10-20 18:21:08 │
╰──────────────┴──────────┴─────────────────────╯

⚠ 2 devices were not backed up
╭──────────────┬──────────────────────────────────────────────────────────────────╮
│ Device       │ Reason                                                           │
├──────────────┼──────────────────────────────────────────────────────────────────┤
│ fw-branch-02 │ Backup is not enabled for this device                            │
│ fw-branch-04 │ A backup of this device is already waiting to be sent or running │
╰──────────────┴──────────────────────────────────────────────────────────────────╯
`
	if got := bufs["table"].String(); got != want {
		t.Errorf("table output:\n%s\nwant:\n%s", got, want)
	}
}

func TestBackupRunSimple(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	fs, bufs := plainFormatters(t)

	if err := fs["simple"].FormatBackupRunResult(backupRunSample()); err != nil {
		t.Fatal(err)
	}

	want := `✓ Started 2 backups in org "acme-corp" — 2 skipped
• fw-branch-01 → task tQSd9RCV (expires 2026-10-20 18:21:07)
• fw-branch-03 → task hK3mZp0a (expires 2026-10-20 18:21:08)
• fw-branch-02 → skipped: Backup is not enabled for this device
• fw-branch-04 → skipped: A backup of this device is already waiting to be sent or running
`
	if got := bufs["simple"].String(); got != want {
		t.Errorf("simple output:\n%s\nwant:\n%s", got, want)
	}
}

func TestBackupRunDetailed(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	fs, bufs := plainFormatters(t)

	if err := fs["detailed"].FormatBackupRunResult(backupRunSample()); err != nil {
		t.Fatal(err)
	}

	out := bufs["detailed"].String()
	for _, want := range []string{
		`✓ Started 2 backups in org "acme-corp" — 2 skipped`,
		"─ fw-branch-01 ─", "Task: tQSd9RCV", "PENDING", "Expires: 2026-10-20 18:21:07",
		"─ fw-branch-03 ─", "Task: hK3mZp0a",
		"─ Not backed up ─",
		"fw-branch-02: Backup is not enabled for this device",
		"fw-branch-04: A backup of this device is already waiting to be sent or running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detailed output lacks %q:\n%s", want, out)
		}
	}
	// The UUIDs are internal to the human formats, as in a run.
	if strings.Contains(out, "550e8400") || strings.Contains(out, "7c0e2f4a") {
		t.Errorf("detailed output shows a device uuid:\n%s", out)
	}
}

// TestBackupRunJSON: one document, the answer as NDManager sent it, with the
// UUIDs and the codes a script needs and both lists present.
func TestBackupRunJSON(t *testing.T) {
	fs, bufs := plainFormatters(t)

	if err := fs["json"].FormatBackupRunResult(backupRunSample()); err != nil {
		t.Fatal(err)
	}

	var got models.BackupRunResult
	if err := json.Unmarshal(bufs["json"].Bytes(), &got); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, bufs["json"].String())
	}
	want := backupRunSample()
	if got.Total != want.Total || len(got.Tasks) != 2 || len(got.Skipped) != 2 ||
		got.Tasks[0] != want.Tasks[0] || got.Skipped[1] != want.Skipped[1] {
		t.Errorf("decoded %+v", got)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bufs["json"].Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"type", "organization", "total", "tasks", "skipped"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON lacks %q", key)
		}
	}
}

// TestBackupRunNothingSkipped: with no skipped device the human formats say
// nothing about skips, and JSON keeps the empty list.
func TestBackupRunNothingSkipped(t *testing.T) {
	inDisplayTimezone(t, "UTC")
	result := backupRunSample()
	result.Total, result.Tasks, result.Skipped = 1, result.Tasks[:1], []models.BackupRunSkipped{}

	fs, bufs := plainFormatters(t)
	for _, name := range []string{"table", "simple", "detailed"} {
		if err := fs[name].FormatBackupRunResult(result); err != nil {
			t.Fatal(err)
		}
		out := bufs[name].String()
		if !strings.Contains(out, `Started 1 backup in org "acme-corp"`) {
			t.Errorf("%s: header is not singular:\n%s", name, out)
		}
		if strings.Contains(strings.ToLower(out), "skipped") || strings.Contains(out, "not backed up") {
			t.Errorf("%s: mentions skips when there are none:\n%s", name, out)
		}
	}

	if err := fs["json"].FormatBackupRunResult(result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bufs["json"].String(), `"skipped": []`) {
		t.Errorf("json dropped the empty skipped list:\n%s", bufs["json"].String())
	}
}

// TestBackupRunUnknownCodeShowsItsReason: a code a later control plane adds is
// shown through its reason, verbatim; the code stands in only without one.
func TestBackupRunUnknownCodeShowsItsReason(t *testing.T) {
	result := backupRunSample()
	result.Skipped = []models.BackupRunSkipped{
		{DeviceName: "fw-new", Code: "SOME_FUTURE_CODE", Reason: "A reason this build has never heard of"},
		{DeviceName: "fw-bare", Code: "ANOTHER_FUTURE_CODE"},
	}

	fs, bufs := plainFormatters(t)
	for _, name := range []string{"table", "simple", "detailed"} {
		if err := fs[name].FormatBackupRunResult(result); err != nil {
			t.Fatal(err)
		}
		out := bufs[name].String()
		if !strings.Contains(out, "A reason this build has never heard of") {
			t.Errorf("%s: the reason was not shown as sent:\n%s", name, out)
		}
		if !strings.Contains(out, "ANOTHER_FUTURE_CODE") {
			t.Errorf("%s: a skip without a reason shows nothing:\n%s", name, out)
		}
	}
}

func TestBackupRunTableHeadersAreTitleCase(t *testing.T) {
	fs, bufs := plainFormatters(t)
	if err := fs["table"].FormatBackupRunResult(backupRunSample()); err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"│ Device ", "│ Task ", "│ Expires ", "│ Reason "} {
		if !strings.Contains(bufs["table"].String(), header) {
			t.Errorf("table lacks the Title Case header %q", strings.Trim(header, "│ "))
		}
	}
}
