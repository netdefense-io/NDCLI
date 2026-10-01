package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// firmwareUpgradeCommand returns the registered `run firmware-upgrade` with
// every local flag at its default, before the test and after it: the command
// is package-level, so a value one test sets would leak into the next.
func firmwareUpgradeCommand(t *testing.T) *cobra.Command {
	t.Helper()
	prepareRootCommand()

	cmd, _, err := rootCmd.Find([]string{"run", "firmware-upgrade"})
	if err != nil || cmd.Name() != "firmware-upgrade" {
		t.Fatalf("find run firmware-upgrade: %v", err)
	}
	reset := func() {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
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

// runRecorder is a formatter that accepts what `run` hands it.
type runRecorder struct {
	output.Formatter
}

func (runRecorder) FormatRunResult(*models.RunResult) error { return nil }
func (runRecorder) FormatScheduledTaskRegisterResult(*models.ScheduledTaskRegisterResult) error {
	return nil
}

// runFirmwareUpgrade parses args the way cobra does and runs the command
// against a fake control plane. It returns the body of the request the
// command sent (nil when it sent none), what it wrote to stderr, and the parse
// or run error.
//
// It never goes through rootCmd.Execute: that path loads the real config of
// whoever runs the tests, so a regression that let an argument list through
// would create a firmware upgrade task on that user's control plane.
func runFirmwareUpgrade(t *testing.T, args ...string) (sent map[string]any, stderr string, err error) {
	t.Helper()
	cmd := firmwareUpgradeCommand(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/organizations/acme/tasks" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("decode the request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, ok := sent["schedule"]; ok {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"code": "S1", "kind": "RUN", "schedule_name": "nightly", "enabled": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"type": "FIRMWARE_UPGRADE", "organization": "acme", "total": 1,
			"tasks": []any{map[string]any{"task": "T1", "device_name": "fw1", "status": "PENDING"}},
		})
	}))
	t.Cleanup(srv.Close)

	origSvc, origOrg, origFormatter := svc, orgName, formatter
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName, formatter = "acme", runRecorder{}
	t.Cleanup(func() { svc, orgName, formatter = origSvc, origOrg, origFormatter })

	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	t.Cleanup(func() { cmd.SetErr(nil) })

	if err := cmd.ParseFlags(args); err != nil {
		return nil, "", err
	}
	err = cmd.RunE(cmd, cmd.Flags().Args())
	return sent, errBuf.String(), err
}

// The warnings are pinned whole: a plain reboot does not install the base and
// kernel a --no-reboot run defers, so the text must not suggest it does.
const (
	rebootWarning   = "Warning: this command will trigger a firmware upgrade and reboot the targeted device(s).\n"
	noRebootWarning = "Warning: this command will upgrade firmware packages on the targeted device(s) without a reboot " +
		"and leave base and kernel for a later run. The device(s) stay in a mixed state until a firmware upgrade " +
		"without --no-reboot installs them.\n"
)

// TestFirmwareUpgradeRebootPayload pins the flag to the wire field: reboot is
// the inverse of --no-reboot on every target and scheduling path, and nothing
// else in the payload moves with it.
func TestFirmwareUpgradeRebootPayload(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		wantPayload map[string]any
		wantWarning string // the whole of stderr; "" means nothing may be written
		check       func(t *testing.T, body map[string]any)
	}{
		{
			name:        "without the flag the device reboots",
			args:        []string{"--device", "fw1", "--mode", "minor"},
			wantPayload: map[string]any{"mode": "minor", "reboot": true, "check_first": true, "dry_run": false},
			wantWarning: rebootWarning,
		},
		{
			name:        "--no-reboot sends reboot=false",
			args:        []string{"--device", "fw1", "--mode", "minor", "--no-reboot"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": true, "dry_run": false},
			wantWarning: noRebootWarning,
		},
		{
			name:        "a major upgrade reboots",
			args:        []string{"--device", "fw1", "--mode", "major"},
			wantPayload: map[string]any{"mode": "major", "reboot": true, "check_first": true, "dry_run": false},
			wantWarning: rebootWarning,
		},
		{
			name:        "--no-reboot with --dry-run and --no-check",
			args:        []string{"--device", "fw1", "--mode", "minor", "--no-reboot", "--dry-run", "--no-check"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": false, "dry_run": true},
		},
		{
			name:        "--no-reboot on an OU",
			args:        []string{"--ou", "edge", "--mode", "minor", "--no-reboot"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": true, "dry_run": false},
			wantWarning: noRebootWarning,
			check: func(t *testing.T, body map[string]any) {
				want := map[string]any{"devices": []any{}, "ous": []any{"edge"}, "all": false}
				if !reflect.DeepEqual(body["targets"], want) {
					t.Errorf("targets = %v, want %v", body["targets"], want)
				}
			},
		},
		{
			name:        "--no-reboot on the whole org",
			args:        []string{"--org", "--mode", "minor", "--no-reboot"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": true, "dry_run": false},
			wantWarning: noRebootWarning,
			check: func(t *testing.T, body map[string]any) {
				want := map[string]any{"devices": []any{}, "ous": []any{}, "all": true}
				if !reflect.DeepEqual(body["targets"], want) {
					t.Errorf("targets = %v, want %v", body["targets"], want)
				}
			},
		},
		{
			name:        "--no-reboot deferred with --at",
			args:        []string{"--device", "fw1", "--mode", "minor", "--no-reboot", "--at", "2h"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": true, "dry_run": false},
			wantWarning: noRebootWarning,
			check: func(t *testing.T, body map[string]any) {
				if at, _ := body["scheduled_at"].(string); !strings.HasSuffix(at, "Z") {
					t.Errorf("scheduled_at = %v, want a UTC instant", body["scheduled_at"])
				}
			},
		},
		{
			name:        "--no-reboot registered on a schedule",
			args:        []string{"--device", "fw1", "--mode", "minor", "--no-reboot", "--schedule", "nightly"},
			wantPayload: map[string]any{"mode": "minor", "reboot": false, "check_first": true, "dry_run": false},
			wantWarning: noRebootWarning,
			check: func(t *testing.T, body map[string]any) {
				if body["schedule"] != "nightly" {
					t.Errorf("schedule = %v, want nightly", body["schedule"])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, stderr, err := runFirmwareUpgrade(t, tc.args...)
			if err != nil {
				t.Fatalf("run firmware-upgrade %v: %v", tc.args, err)
			}
			if body["type"] != models.TaskTypeFirmwareUpgrade {
				t.Errorf("type = %v, want %s", body["type"], models.TaskTypeFirmwareUpgrade)
			}
			if !reflect.DeepEqual(body["payload"], tc.wantPayload) {
				t.Errorf("payload = %v, want %v", body["payload"], tc.wantPayload)
			}
			if stderr != tc.wantWarning {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantWarning)
			}
			if tc.check != nil {
				tc.check(t, body)
			}
		})
	}
}

func TestFirmwareUpgradeRefusesMajorWithNoReboot(t *testing.T) {
	body, _, err := runFirmwareUpgrade(t, "--device", "fw1", "--mode", "major", "--no-reboot")

	var svcErr *service.Error
	if !errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput {
		t.Fatalf("err = %#v, want %s", err, service.CodeInvalidInput)
	}
	if !strings.Contains(err.Error(), "--no-reboot is not allowed with --mode major") {
		t.Errorf("the refusal should name --no-reboot and --mode major:\n%s", err)
	}
	if body != nil {
		t.Errorf("nothing may be sent, sent %v", body)
	}
}

// TestFirmwareUpgradeHasNoRebootFlag: --no-reboot is the only reboot flag.
// Every spelling of --reboot is an unknown flag, refused before anything is
// sent.
func TestFirmwareUpgradeHasNoRebootFlag(t *testing.T) {
	for _, flag := range []string{"--reboot", "--reboot=false", "--reboot=true"} {
		t.Run(flag, func(t *testing.T) {
			body, _, err := runFirmwareUpgrade(t, "--device", "fw1", "--mode", "minor", flag)
			if err == nil || !strings.Contains(err.Error(), "unknown flag: --reboot") {
				t.Fatalf("err = %v, want unknown flag: --reboot", err)
			}
			if body != nil {
				t.Errorf("nothing may be sent, sent %v", body)
			}
		})
	}

	f := firmwareUpgradeCommand(t).Flags().Lookup("no-reboot")
	if f == nil || f.Value.Type() != "bool" || f.DefValue != "false" {
		t.Fatalf("--no-reboot must be a bool flag defaulting to false, got %+v", f)
	}
}

// TestFirmwareUpgradeHelpNamesOnlyRealFlags: every flag the help text names
// exists on the command, so typing what the help says never fails with
// "unknown flag".
func TestFirmwareUpgradeHelpNamesOnlyRealFlags(t *testing.T) {
	cmd := firmwareUpgradeCommand(t)

	help := []string{cmd.Long, cmd.Example}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) { help = append(help, f.Usage) })
	text := strings.Join(help, "\n")

	for _, name := range regexp.MustCompile(`--([a-z][a-z-]*)`).FindAllStringSubmatch(text, -1) {
		if cmd.Flags().Lookup(name[1]) == nil {
			t.Errorf("the help names --%s, which run firmware-upgrade does not have", name[1])
		}
	}
	if !strings.Contains(cmd.Example, "--no-reboot") {
		t.Errorf("the examples should show --no-reboot:\n%s", cmd.Example)
	}
}
