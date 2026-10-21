package cli

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

const runBackupFixture = `{
  "type": "BACKUP", "organization": "acme", "total": 2,
  "tasks": [
    {"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01", "status": "PENDING", "expires_at": "2026-10-20T18:21:07Z"},
    {"task": "hK3mZp0a", "device_uuid": "11111111-2222-3333-4444-555555555555", "device_name": "fw-branch-03", "status": "PENDING", "expires_at": "2026-10-20T18:21:08Z"}
  ],
  "skipped": [
    {"device_uuid": "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"}
  ]
}`

// runBackupCommand returns the registered `run backup` with every local flag at
// its default, before the test and after it: the command is package-level, so a
// value one test sets would leak into the next.
func runBackupCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := findCommand(t, "run", "backup")
	if cmd.Name() != "backup" {
		t.Fatalf("found %q", cmd.Name())
	}
	reset := func() {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if s, ok := f.Value.(pflag.SliceValue); ok {
				_ = s.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset()
	t.Cleanup(reset)
	return cmd
}

// backupRunOutcome is what one `ndcli run backup` invocation did.
type backupRunOutcome struct {
	method, path, body string
	requests           int
	stdout, stderr     string
	err                error
}

// runBackup parses args the way cobra does and runs the command in format
// against a fake control plane answering status and body. It never goes
// through rootCmd.Execute, which would load the config of whoever runs the
// tests and could start a backup on their control plane.
func runBackup(t *testing.T, format string, status int, body string, args ...string) backupRunOutcome {
	t.Helper()
	cmd := runBackupCommand(t)

	var out backupRunOutcome
	handler := func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		out.requests++
		out.method, out.path, out.body = r.Method, r.URL.EscapedPath(), strings.TrimSpace(string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
	out.stdout, out.stderr = runStreams(t, handler, format, "", func() error {
		if err := cmd.ParseFlags(args); err != nil {
			out.err = err
			return nil
		}
		out.err = runRunBackup(cmd, cmd.Flags().Args())
		return nil
	})
	return out
}

// TestRunBackupWire pins the route and the body, literally, for every way of
// choosing targets. A backup goes through its own route, not POST /tasks, and
// sends targets and nothing else.
func TestRunBackupWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		body string
	}{
		{"one device", []string{"--device", "fw-branch-01"}, `{"targets":{"devices":["fw-branch-01"]}}`},
		{"several devices", []string{"--device", "fw-a", "--device", "fw-b"}, `{"targets":{"devices":["fw-a","fw-b"]}}`},
		{"comma-separated devices", []string{"--device", "fw-a,fw-b"}, `{"targets":{"devices":["fw-a","fw-b"]}}`},
		{"an OU", []string{"--ou", "edge"}, `{"targets":{"ous":["edge"]}}`},
		{"devices and OUs", []string{"--device", "fw-a", "--ou", "edge"}, `{"targets":{"devices":["fw-a"],"ous":["edge"]}}`},
		{"every device that has backup enabled", []string{"--org"}, `{"targets":{"all":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runBackup(t, "table", http.StatusCreated, runBackupFixture, tc.args...)

			if got.err != nil {
				t.Fatalf("run backup %v: %v", tc.args, got.err)
			}
			if got.requests != 1 || got.method != http.MethodPost || got.path != "/api/v1/organizations/acme/backups/run" {
				t.Errorf("%d request(s), %s %s", got.requests, got.method, got.path)
			}
			if got.body != tc.body {
				t.Errorf("body = %s\nwant   %s", got.body, tc.body)
			}
		})
	}
}

func TestRunBackupTableOutput(t *testing.T) {
	got := runBackup(t, "table", http.StatusCreated, runBackupFixture, "--device", "fw-branch-01")

	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, want := range []string{
		`Started 2 backups in org "acme" — 1 skipped`,
		"fw-branch-01", "tQSd9RCV", "fw-branch-03", "hK3mZp0a",
		"1 device was not backed up", "fw-branch-02", "Backup is not enabled for this device",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q", got.stderr)
	}
}

func TestRunBackupJSONOutput(t *testing.T) {
	got := runBackup(t, "json", http.StatusCreated, runBackupFixture, "--org")

	if got.err != nil {
		t.Fatal(got.err)
	}
	var result models.BackupRunResult
	decodeOne(t, got.stdout, &result)
	if result.Total != 2 || len(result.Tasks) != 2 || len(result.Skipped) != 1 ||
		result.Skipped[0].Code != "BACKUP_NOT_ENABLED" || result.Skipped[0].DeviceUUID != "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b" {
		t.Errorf("decoded %+v", result)
	}
}

// TestRunBackupRefusesDeferral: a backup starts now. --at and --schedule exist
// on the command only to be refused with the way forward, and nothing is sent.
func TestRunBackupRefusesDeferral(t *testing.T) {
	for _, args := range [][]string{
		{"--device", "fw-a", "--at", "2h"},
		{"--device", "fw-a", "--at", ""},
		{"--org", "--at", "2026-12-01T03:00:00Z"},
		{"--device", "fw-a", "--schedule", "nightly"},
		{"--device", "fw-a", "--schedule", ""},
		{"--org", "--at", "2h", "--schedule", "nightly"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			got := runBackup(t, "table", http.StatusCreated, runBackupFixture, args...)

			var svcErr *service.Error
			if !errors.As(got.err, &svcErr) || svcErr.Code != service.CodeInvalidInput {
				t.Fatalf("err = %v, want a CodeInvalidInput service error", got.err)
			}
			want := "ndcli run backup starts a backup now and cannot be deferred or scheduled (--at, --schedule). " +
				"Recurring backups are set with 'ndcli backup config set --schedule <name>'"
			if got.err.Error() != want {
				t.Errorf("message = %q\nwant      %q", got.err.Error(), want)
			}
			if got.requests != 0 {
				t.Errorf("%d request(s) were sent for a deferred backup", got.requests)
			}
		})
	}
}

func TestRunBackupNeedsATarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"none", nil, "at least one of --device, --ou, or --org is required"},
		{"--org with a device", []string{"--org", "--device", "fw-a"}, "--org cannot be combined with --device or --ou"},
		{"--org with an OU", []string{"--org", "--ou", "edge"}, "--org cannot be combined with --device or --ou"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runBackup(t, "table", http.StatusCreated, runBackupFixture, tc.args...)

			if got.err == nil || got.err.Error() != tc.want {
				t.Errorf("err = %v, want %q", got.err, tc.want)
			}
			if got.requests != 0 {
				t.Errorf("%d request(s) were sent", got.requests)
			}
		})
	}
}

// TestRunBackupShowsWhyNothingStarted: BACKUP_NO_ELIGIBLE_DEVICES prints the
// server's sentence, the reason for each device and, where backup being off is
// part of it, the command that turns it on.
func TestRunBackupShowsWhyNothingStarted(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	got := runBackup(t, "table", http.StatusConflict, `{
	  "error": "No backup was started: none of the targeted devices can be backed up right now",
	  "code": "BACKUP_NO_ELIGIBLE_DEVICES",
	  "skipped": [
	    {"device_uuid": "u2", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"},
	    {"device_uuid": "u1", "device_name": "fw-branch-01", "code": "BACKUP_IN_FLIGHT", "reason": "A backup of this device is already waiting to be sent or running"}
	  ]}`, "--device", "fw-branch-01", "--device", "fw-branch-02")

	if got.err == nil {
		t.Fatal("a backup that starts nothing must fail")
	}
	want := "Error: [BACKUP_NO_ELIGIBLE_DEVICES] No backup was started: none of the targeted devices can be backed up right now\n" +
		"  • fw-branch-02: Backup is not enabled for this device\n" +
		"  • fw-branch-01: A backup of this device is already waiting to be sent or running\n" +
		"  Hint: Backup is enabled per device: 'ndcli backup enable <device>'.\n"
	if got := printed(got.err); got != want {
		t.Errorf("printed:\n%s\nwant:\n%s", got, want)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want it empty on failure", got.stdout)
	}
}

// TestRunBackupNoDeviceHasBackup: the list is empty, so the hint says where
// backup is enabled.
func TestRunBackupNoDeviceHasBackup(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	got := runBackup(t, "table", http.StatusConflict,
		`{"error":"No device in this organization has backup enabled","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[]}`, "--org")

	want := "Error: [BACKUP_NO_ELIGIBLE_DEVICES] No device in this organization has backup enabled\n" +
		"  Hint: Backup is enabled per device: 'ndcli backup enable <device>'.\n"
	if got := printed(got.err); got != want {
		t.Errorf("printed:\n%s\nwant:\n%s", got, want)
	}
}

// TestRunBackupTargetsResolveToNothing: an OU with no members, or devices that
// are not enabled, give the same 409 with an empty list as an organization where
// no device has backup. Only the latter is cured by enabling backup, so the hint
// is given only to --org.
func TestRunBackupTargetsResolveToNothing(t *testing.T) {
	const serverMessage = "The targets include no enabled device"
	body := `{"error":"` + serverMessage + `","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[]}`
	const hint = "  Hint: Backup is enabled per device: 'ndcli backup enable <device>'.\n"

	for _, tc := range []struct {
		name string
		args []string
		hint bool
	}{
		{"an OU with no members", []string{"--ou", "empty-ou"}, false},
		{"a named device", []string{"--device", "fw-a"}, false},
		{"devices and OUs", []string{"--device", "fw-a", "--ou", "empty-ou"}, false},
		{"every device that has backup", []string{"--org"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useFormatter(t, output.NewTableFormatter(), "")

			got := runBackup(t, "table", http.StatusConflict, body, tc.args...)

			if got.err == nil {
				t.Fatal("a backup that starts nothing must fail")
			}
			want := "Error: [BACKUP_NO_ELIGIBLE_DEVICES] " + serverMessage + "\n"
			if tc.hint {
				want += hint
			}
			if got := printed(got.err); got != want {
				t.Errorf("printed:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestRunBackupConfigDisabledWithAnEmptyList: NDManager sends skipped: [] with
// this refusal too. It is the configuration that is off, not the devices.
func TestRunBackupConfigDisabledWithAnEmptyList(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	got := runBackup(t, "table", http.StatusConflict,
		`{"error":"The backup configuration is disabled; enable it before backing up devices","code":"BACKUP_CONFIG_DISABLED","skipped":[]}`, "--org")

	want := "Error: [BACKUP_CONFIG_DISABLED] The backup configuration is disabled; enable it before backing up devices\n" +
		"  Hint: Enable it first: 'ndcli backup config enable'.\n"
	if got := printed(got.err); got != want {
		t.Errorf("printed:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunBackupJSONRefusal(t *testing.T) {
	useFormatter(t, output.NewJSONFormatter(), "json")

	got := runBackup(t, "json", http.StatusConflict,
		`{"error":"The backup configuration is disabled; enable it before backing up devices","code":"BACKUP_CONFIG_DISABLED"}`, "--org")

	doc := decodeCommandError(t, printed(got.err))
	if doc["code"] != "BACKUP_CONFIG_DISABLED" ||
		doc["message"] != "The backup configuration is disabled; enable it before backing up devices\n  Hint: Enable it first: 'ndcli backup config enable'." {
		t.Errorf("error document = %v", doc)
	}
}

func TestRunBackupConfigurationMissing(t *testing.T) {
	useFormatter(t, output.NewTableFormatter(), "")

	got := runBackup(t, "table", http.StatusNotFound,
		`{"error":"This organization has no backup configuration; create one before backing up devices","code":"BACKUP_NOT_CONFIGURED"}`, "--org")

	want := "Error: [BACKUP_NOT_CONFIGURED] This organization has no backup configuration; create one before backing up devices\n" +
		"  Hint: Create the backup configuration first: 'ndcli backup config set'.\n"
	if got := printed(got.err); got != want {
		t.Errorf("printed:\n%s\nwant:\n%s", got, want)
	}
}

// TestRunBackupIsNotAGenericRun: a backup is not a `run` task type. It must not
// be reachable through the table that maps run names to task types, which feeds
// POST /tasks, and it is not built by the shared run constructor, so it carries
// no payload and its scheduling flags are not advertised.
func TestRunBackupIsNotAGenericRun(t *testing.T) {
	if _, ok := runFriendlyToTaskType["backup"]; ok {
		t.Error(`"backup" is in runFriendlyToTaskType: it would be sent to POST /tasks, which refuses BACKUP`)
	}

	cmd := runBackupCommand(t)
	for _, name := range []string{"at", "schedule"} {
		f := cmd.Flags().Lookup(name)
		if f == nil || !f.Hidden {
			t.Errorf("--%s must exist, to be refused, and stay out of the help", name)
		}
	}
	usage := cmd.UsageString()
	if strings.Contains(usage, "--at") || strings.Contains(usage, "--schedule") {
		t.Errorf("the usage advertises a scheduling flag:\n%s", usage)
	}
	for _, name := range []string{"device", "ou", "org"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s is missing", name)
		}
	}
	if !strings.Contains(cmd.Long, "cannot be deferred or scheduled") || !strings.Contains(cmd.Long, "expires a minute") {
		t.Errorf("the help does not say a backup starts now and expires:\n%s", cmd.Long)
	}
}
