package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// backupRunFixture is the 201 NDManager answers an on-demand backup with.
const backupRunFixture = `{
  "type": "BACKUP",
  "organization": "acme",
  "total": 2,
  "tasks": [
    {"task": "tQSd9RCV", "device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01", "status": "PENDING", "expires_at": "2026-10-20T18:21:07Z"},
    {"task": "hK3mZp0a", "device_uuid": "11111111-2222-3333-4444-555555555555", "device_name": "fw-branch-03", "status": "PENDING", "expires_at": "2026-10-20T18:21:08Z"}
  ],
  "skipped": [
    {"device_uuid": "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", "device_name": "fw-branch-02", "code": "BACKUP_IN_FLIGHT", "reason": "A backup of this device is already waiting to be sent or running"},
    {"device_uuid": "99999999-8888-7777-6666-555555555555", "device_name": "fw-branch-04", "code": "SOME_FUTURE_CODE", "reason": "A reason this build has never heard of"}
  ]
}`

// sentRequest is what the fake control plane saw of the one request.
type sentRequest struct {
	method, path, rawPath, body string
	hits                        int
}

// backupRunServer answers every request with status and body, and records what
// it was asked.
func backupRunServer(t *testing.T, status int, body string) (*Service, *sentRequest) {
	t.Helper()
	sent := &sentRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent.hits++
		sent.method, sent.path, sent.rawPath, sent.body = r.Method, r.URL.Path, r.URL.EscapedPath(), strings.TrimSpace(string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv), sent
}

// TestBackupRunWireRequest pins the route and the body, literally. The body
// carries targets and nothing else: the route refuses payload, scheduled_at and
// schedule, and the encryption key is never a client's to send.
func TestBackupRunWireRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		org  string
		opts BackupRunOpts
		path string
		body string
	}{
		{
			name: "named devices and OUs",
			org:  "acme",
			opts: BackupRunOpts{Devices: []string{"fw-branch-01", "fw-branch-02"}, OUs: []string{"edge"}},
			path: "/api/v1/organizations/acme/backups/run",
			body: `{"targets":{"devices":["fw-branch-01","fw-branch-02"],"ous":["edge"]}}`,
		},
		{
			name: "one device",
			org:  "acme",
			opts: BackupRunOpts{Devices: []string{"fw-branch-01"}},
			path: "/api/v1/organizations/acme/backups/run",
			body: `{"targets":{"devices":["fw-branch-01"]}}`,
		},
		{
			name: "one OU",
			org:  "acme",
			opts: BackupRunOpts{OUs: []string{"edge"}},
			path: "/api/v1/organizations/acme/backups/run",
			body: `{"targets":{"ous":["edge"]}}`,
		},
		{
			name: "every device that has backup enabled",
			org:  "acme",
			opts: BackupRunOpts{AllDevices: true},
			path: "/api/v1/organizations/acme/backups/run",
			body: `{"targets":{"all":true}}`,
		},
		{
			name: "the organization name is escaped into the path",
			org:  "acme corp/eu",
			opts: BackupRunOpts{AllDevices: true},
			path: "/api/v1/organizations/acme%20corp%2Feu/backups/run",
			body: `{"targets":{"all":true}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sent := backupRunServer(t, http.StatusCreated, backupRunFixture)

			if _, err := svc.BackupRun(context.Background(), tc.org, tc.opts); err != nil {
				t.Fatalf("BackupRun: %v", err)
			}
			if sent.hits != 1 {
				t.Fatalf("%d requests, want 1", sent.hits)
			}
			if sent.method != http.MethodPost {
				t.Errorf("method = %s, want POST", sent.method)
			}
			if sent.rawPath != tc.path {
				t.Errorf("path = %s, want %s", sent.rawPath, tc.path)
			}
			if sent.body != tc.body {
				t.Errorf("body = %s, want %s", sent.body, tc.body)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal([]byte(sent.body), &keys); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			for key := range keys {
				if key != "targets" {
					t.Errorf("body carries %q; only targets may be sent", key)
				}
			}
		})
	}
}

func TestBackupRunResult(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusCreated, backupRunFixture)

	result, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	if err != nil {
		t.Fatalf("BackupRun: %v", err)
	}
	if result.Type != "BACKUP" || result.Organization != "acme" || result.Total != 2 {
		t.Errorf("header = %s %s %d", result.Type, result.Organization, result.Total)
	}
	wantTask := models.RunTaskItem{Task: "tQSd9RCV", DeviceUUID: "550e8400-e29b-41d4-a716-446655440000", DeviceName: "fw-branch-01", Status: "PENDING", ExpiresAt: "2026-10-20T18:21:07Z"}
	if len(result.Tasks) != 2 || result.Tasks[0] != wantTask {
		t.Errorf("tasks = %+v", result.Tasks)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if got := result.Skipped[0]; got.Code != models.BackupSkipInFlight || got.DeviceName != "fw-branch-02" ||
		got.Reason != "A backup of this device is already waiting to be sent or running" {
		t.Errorf("first skipped = %+v", got)
	}
	// A code this build does not know is still a skip, with its reason intact.
	if got := result.Skipped[1]; got.Code != "SOME_FUTURE_CODE" || got.Reason != "A reason this build has never heard of" {
		t.Errorf("unknown code was not kept as sent: %+v", got)
	}
}

// TestBackupRunKeepsBothListsAsArrays: -f json and the MCP result must show []
// rather than null for a list the server left out.
func TestBackupRunKeepsBothListsAsArrays(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusCreated, `{"type":"BACKUP","organization":"acme","total":1}`)

	result, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	if err != nil {
		t.Fatalf("BackupRun: %v", err)
	}
	out, _ := json.Marshal(result)
	if !strings.Contains(string(out), `"tasks":[]`) || !strings.Contains(string(out), `"skipped":[]`) {
		t.Errorf("lists are not arrays: %s", out)
	}
}

func TestBackupRunValidatesBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		org  string
		opts BackupRunOpts
	}{
		{"no organization", "", BackupRunOpts{AllDevices: true}},
		{"no target", "acme", BackupRunOpts{}},
		{"all with a device", "acme", BackupRunOpts{AllDevices: true, Devices: []string{"fw-a"}}},
		{"all with an OU", "acme", BackupRunOpts{AllDevices: true, OUs: []string{"edge"}}},
		{"a blank device", "acme", BackupRunOpts{Devices: []string{"fw-a", "  "}}},
		{"a blank OU", "acme", BackupRunOpts{OUs: []string{""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sent := backupRunServer(t, http.StatusCreated, backupRunFixture)

			_, err := svc.BackupRun(context.Background(), tc.org, tc.opts)

			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
				t.Fatalf("err = %v, want a CodeInvalidInput service error", err)
			}
			if sent.hits != 0 {
				t.Errorf("%d requests were sent for an invalid selection", sent.hits)
			}
			// The message is shared with the MCP tool, so it names no CLI flag.
			if strings.Contains(err.Error(), "--") {
				t.Errorf("message names a flag: %q", err.Error())
			}
		})
	}
}

// TestRunStillRefusesBackup: a backup must not be reachable through the generic
// run path. POST /tasks refuses BACKUP by design, because the task's payload is
// the encryption key, which only the control plane resolves.
func TestRunStillRefusesBackup(t *testing.T) {
	svc, sent := backupRunServer(t, http.StatusCreated, backupRunFixture)

	for _, run := range []func() error{
		func() error {
			_, err := svc.Run(context.Background(), "acme", RunOpts{Type: "BACKUP", Devices: []string{"fw-a"}})
			return err
		},
		func() error {
			_, err := svc.RunRegisterSpec(context.Background(), "acme", RunOpts{Type: "BACKUP", Devices: []string{"fw-a"}, Schedule: "nightly"})
			return err
		},
	} {
		var svcErr *Error
		if err := run(); !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
			t.Errorf("err = %v, want a CodeInvalidInput service error", err)
		}
	}
	if sent.hits != 0 {
		t.Errorf("%d requests were sent for a BACKUP run", sent.hits)
	}
}

const noEligibleBody = `{
  "error": "No backup was started: none of the targeted devices can be backed up right now",
  "code": "BACKUP_NO_ELIGIBLE_DEVICES",
  "skipped": [
    {"device_uuid": "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b", "device_name": "fw-branch-02", "code": "BACKUP_NOT_ENABLED", "reason": "Backup is not enabled for this device"},
    {"device_uuid": "550e8400-e29b-41d4-a716-446655440000", "device_name": "fw-branch-01", "code": "BACKUP_IN_FLIGHT", "reason": "A backup of this device is already waiting to be sent or running"}
  ]
}`

var testBackupVocabulary = BackupRunVocabulary{
	ConfigCreate: "CREATE-CONFIG",
	ConfigEnable: "ENABLE-CONFIG",
	DeviceEnable: "ENABLE-DEVICE",
}

func TestBackupRunNoEligibleDevices(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusConflict, noEligibleBody)

	_, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{Devices: []string{"fw-branch-01", "fw-branch-02"}})
	if err == nil {
		t.Fatal("a request that starts no backup must be an error")
	}
	if code := ErrorCode(err); code != "BACKUP_NO_ELIGIBLE_DEVICES" {
		t.Errorf("code = %q", code)
	}
	// The reasons are in the message every front-end prints, as sent.
	for _, line := range []string{
		"  • fw-branch-02: Backup is not enabled for this device",
		"  • fw-branch-01: A backup of this device is already waiting to be sent or running",
	} {
		if !strings.Contains(err.Error(), line) {
			t.Errorf("message lacks %q:\n%s", line, err.Error())
		}
	}

	skipped, ok := BackupRunRefusal(err)
	if !ok || len(skipped) != 2 {
		t.Fatalf("BackupRunRefusal = %+v, %v", skipped, ok)
	}
	if skipped[0].DeviceName != "fw-branch-02" || skipped[0].Code != models.BackupSkipNotEnabled || skipped[0].DeviceUUID != "7c0e2f4a-9b1d-4f6e-8a3b-5d2c1e0f9a8b" {
		t.Errorf("first skipped = %+v", skipped[0])
	}

	// A device without backup is in the list, so the way to enable it is offered.
	opts := BackupRunOpts{Devices: []string{"fw-branch-01", "fw-branch-02"}}
	advised := BackupRunAdvice(err, opts, testBackupVocabulary)
	if !strings.HasSuffix(advised.Error(), "\n  Hint: Backup is enabled per device: ENABLE-DEVICE.") {
		t.Errorf("advice =\n%s", advised.Error())
	}
	if ErrorCode(advised) != "BACKUP_NO_ELIGIBLE_DEVICES" {
		t.Errorf("advice lost the code: %q", ErrorCode(advised))
	}
	if again := BackupRunAdvice(advised, opts, testBackupVocabulary); again.Error() != advised.Error() {
		t.Errorf("advice was given twice:\n%s", again.Error())
	}
	if _, ok := BackupRunRefusal(advised); !ok {
		t.Error("the refusal is no longer reachable through the advice")
	}
}

// TestBackupRunNoEligibleDevicesAllInFlight: nothing here is fixed by enabling
// backup, so no hint about enabling it is given.
func TestBackupRunNoEligibleDevicesAllInFlight(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusConflict, `{"error":"No backup was started","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[
	  {"device_uuid":"u1","device_name":"fw-a","code":"BACKUP_IN_FLIGHT","reason":"A backup of this device is already waiting to be sent or running"}]}`)

	opts := BackupRunOpts{Devices: []string{"fw-a"}}
	_, err := svc.BackupRun(context.Background(), "acme", opts)
	if advised := BackupRunAdvice(err, opts, testBackupVocabulary); advised.Error() != err.Error() {
		t.Errorf("unexpected advice:\n%s", advised.Error())
	}
}

// TestBackupRunNoEligibleDevicesEmptyList: the list is empty. For a request for
// every device with backup, that says no device in the organization has backup
// enabled, and the hint says where it is enabled. For named devices or OUs it
// says the targets resolved to no enabled device (an OU with no members), which
// enabling backup would not fix, so no hint is given. Either way it is the same
// refusal, with an empty, non-nil list.
func TestBackupRunNoEligibleDevicesEmptyList(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts BackupRunOpts
		msg  string
		hint string
	}{
		{"every device with backup", BackupRunOpts{AllDevices: true}, "No device in this organization has backup enabled",
			"\n  Hint: Backup is enabled per device: ENABLE-DEVICE."},
		{"an OU with no members", BackupRunOpts{OUs: []string{"empty-ou"}}, "The targets include no enabled device", ""},
		{"a named device", BackupRunOpts{Devices: []string{"fw-a"}}, "The targets include no enabled device", ""},
		{"devices and OUs", BackupRunOpts{Devices: []string{"fw-a"}, OUs: []string{"empty-ou"}}, "The targets include no enabled device", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := backupRunServer(t, http.StatusConflict, `{"error":"`+tc.msg+`","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[]}`)

			_, err := svc.BackupRun(context.Background(), "acme", tc.opts)
			skipped, ok := BackupRunRefusal(err)
			if !ok || skipped == nil || len(skipped) != 0 {
				t.Fatalf("BackupRunRefusal = %#v, %v; want an empty, non-nil list", skipped, ok)
			}
			if got, want := BackupRunAdvice(err, tc.opts, testBackupVocabulary).Error(), tc.msg+tc.hint; got != want {
				t.Errorf("advice = %q, want %q", got, want)
			}
		})
	}
}

// TestBackupRunAdviceForNoEligibleDevices walks the hint over the lists a
// refusal can carry and the requests it can answer. Enabling backup is offered
// when it is part of the answer: some listed device has it off, or the request
// was for every device with backup and the list is empty.
func TestBackupRunAdviceForNoEligibleDevices(t *testing.T) {
	skip := func(code string) []string { return []string{code} }
	const enableHint = "\n  Hint: Backup is enabled per device: ENABLE-DEVICE."
	named := BackupRunOpts{Devices: []string{"fw-a"}}
	all := BackupRunOpts{AllDevices: true}

	for _, tc := range []struct {
		name     string
		codes    []string
		opts     BackupRunOpts
		wantHint bool
	}{
		{"empty list, every device with backup", nil, all, true},
		{"empty list, named devices", nil, named, false},
		{"empty list, an OU", nil, BackupRunOpts{OUs: []string{"edge"}}, false},
		{"backup off for a listed device, named", skip("BACKUP_NOT_ENABLED"), named, true},
		{"backup off for a listed device, every device", skip("BACKUP_NOT_ENABLED"), all, true},
		{"one off among others", []string{"BACKUP_IN_FLIGHT", "BACKUP_NOT_ENABLED", "NO_ENCRYPTION_KEY"}, named, true},
		{"only in flight", skip("BACKUP_IN_FLIGHT"), named, false},
		{"only device not enabled (an OU member)", skip("DEVICE_NOT_ENABLED"), BackupRunOpts{OUs: []string{"edge"}}, false},
		{"only device not enabled, every device", skip("DEVICE_NOT_ENABLED"), all, false},
		{"only no encryption key", skip("NO_ENCRYPTION_KEY"), all, false},
		{"only a code from a later release", skip("SOME_FUTURE_CODE"), named, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]string, 0, len(tc.codes))
			for _, c := range tc.codes {
				items = append(items, `{"device_uuid":"u","device_name":"fw-a","code":"`+c+`","reason":"r-`+c+`"}`)
			}
			svc, _ := backupRunServer(t, http.StatusConflict,
				`{"error":"No backup was started","code":"BACKUP_NO_ELIGIBLE_DEVICES","skipped":[`+strings.Join(items, ",")+`]}`)

			_, err := svc.BackupRun(context.Background(), "acme", tc.opts)
			advised := BackupRunAdvice(err, tc.opts, testBackupVocabulary)
			if got := strings.HasSuffix(advised.Error(), enableHint); got != tc.wantHint {
				t.Errorf("hint given = %v, want %v:\n%s", got, tc.wantHint, advised.Error())
			}
			if !tc.wantHint && advised != err {
				t.Errorf("a refusal without advice must come back untouched:\n%s", advised.Error())
			}
			if ErrorCode(advised) != "BACKUP_NO_ELIGIBLE_DEVICES" {
				t.Errorf("code = %q", ErrorCode(advised))
			}
		})
	}
}

func TestBackupRunConfigurationRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
		hint   string
	}{
		{
			name: "no configuration", status: http.StatusNotFound,
			body: `{"error":"This organization has no backup configuration; create one before backing up devices","code":"BACKUP_NOT_CONFIGURED"}`,
			code: "BACKUP_NOT_CONFIGURED",
			hint: "\n  Hint: Create the backup configuration first: CREATE-CONFIG.",
		},
		{
			name: "configuration disabled", status: http.StatusConflict,
			body: `{"error":"The backup configuration is disabled; enable it before backing up devices","code":"BACKUP_CONFIG_DISABLED"}`,
			code: "BACKUP_CONFIG_DISABLED",
			hint: "\n  Hint: Enable it first: ENABLE-CONFIG.",
		},
		{
			// NDManager sends an empty list with this refusal too. It is not
			// "no device has backup enabled".
			name: "configuration disabled, with an empty skipped list", status: http.StatusConflict,
			body: `{"error":"The backup configuration is disabled; enable it before backing up devices","code":"BACKUP_CONFIG_DISABLED","skipped":[]}`,
			code: "BACKUP_CONFIG_DISABLED",
			hint: "\n  Hint: Enable it first: ENABLE-CONFIG.",
		},
	} {
		for _, opts := range []BackupRunOpts{{AllDevices: true}, {Devices: []string{"fw-a"}}} {
			t.Run(tc.name, func(t *testing.T) {
				svc, _ := backupRunServer(t, tc.status, tc.body)

				_, err := svc.BackupRun(context.Background(), "acme", opts)
				if ErrorCode(err) != tc.code {
					t.Fatalf("code = %q, want %q", ErrorCode(err), tc.code)
				}
				if _, ok := BackupRunRefusal(err); ok {
					t.Error("only BACKUP_NO_ELIGIBLE_DEVICES is a refusal with a device list")
				}
				advised := BackupRunAdvice(err, opts, testBackupVocabulary).Error()
				if !strings.HasSuffix(advised, tc.hint) || strings.Contains(advised, "ENABLE-DEVICE") {
					t.Errorf("advice =\n%s", advised)
				}
			})
		}
	}
}

// TestBackupRunRefusalsWithoutAdvice: the other refusals are printed as sent.
func TestBackupRunRefusalsWithoutAdvice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
		msg    string
	}{
		{"unknown device", http.StatusUnprocessableEntity, `{"error":"Device not found: fw-x","code":"DEVICE_NOT_FOUND"}`, "DEVICE_NOT_FOUND", "Device not found: fw-x"},
		{"bad targets", http.StatusUnprocessableEntity, `{"error":"Provide at least one of targets","code":"INVALID_PARAM"}`, "INVALID_PARAM", "Provide at least one of targets"},
		{"read-only", http.StatusForbidden, `{"error":"Access denied","code":"ACCESS_DENIED"}`, "ACCESS_DENIED", "Access denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := backupRunServer(t, tc.status, tc.body)

			_, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{Devices: []string{"fw-x"}})
			if ErrorCode(err) != tc.code || err.Error() != tc.msg {
				t.Errorf("err = [%s] %q", ErrorCode(err), err.Error())
			}
			if BackupRunAdvice(err, BackupRunOpts{Devices: []string{"fw-x"}}, testBackupVocabulary) != err {
				t.Error("a refusal without advice must come back untouched")
			}
		})
	}
}

// TestBackupRunBodyModelRefusal: the route refuses a body field it does not
// name with FastAPI's {"detail": [...]}, which carries no code.
func TestBackupRunBodyModelRefusal(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusUnprocessableEntity,
		`{"detail":[{"type":"value_error","loc":["body"],"msg":"Value error, `+"`payload`"+` is not accepted","input":{}}]}`)

	_, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	if err == nil || !strings.Contains(err.Error(), "is not accepted") {
		t.Fatalf("err = %v", err)
	}
	if ErrorCode(err) != "" {
		t.Errorf("code = %q, want none", ErrorCode(err))
	}
}

// TestBackupRunAgainstAnOlderControlPlane: a control plane that predates the
// route answers a bare 404 with no code. That is a version skew, not a missing
// resource; BACKUP_NOT_CONFIGURED is also a 404 and is not.
func TestBackupRunAgainstAnOlderControlPlane(t *testing.T) {
	svc, _ := backupRunServer(t, http.StatusNotFound, `{"detail":"Not Found"}`)

	_, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld {
		t.Fatalf("err = %v, want CodeServerTooOld", err)
	}
	if ErrorCode(err) != CodeServerTooOld {
		t.Errorf("shown code = %q", ErrorCode(err))
	}
	if !api.IsNotFoundError(err) {
		t.Error("the 404 underneath must stay reachable")
	}
	if strings.Contains(err.Error(), "--") {
		t.Errorf("message names a flag: %q", err.Error())
	}

	svc, _ = backupRunServer(t, http.StatusNotFound, `{"error":"no configuration","code":"BACKUP_NOT_CONFIGURED"}`)
	_, err = svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	if ErrorCode(err) != "BACKUP_NOT_CONFIGURED" {
		t.Errorf("a coded 404 was taken for a version skew: %v", err)
	}
}

func TestBackupRunSendsTheCallersTargetsUntouched(t *testing.T) {
	svc, sent := backupRunServer(t, http.StatusCreated, backupRunFixture)
	devices := []string{"b", "a", "b"}

	if _, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{Devices: devices}); err != nil {
		t.Fatal(err)
	}
	// Deduplication is the server's ("a device reached twice gets one task").
	if want := `{"targets":{"devices":["b","a","b"]}}`; sent.body != want {
		t.Errorf("body = %s, want %s", sent.body, want)
	}
	if !reflect.DeepEqual(devices, []string{"b", "a", "b"}) {
		t.Errorf("the caller's slice was changed: %v", devices)
	}
}

// TestBackupRunSanitizesWhatTheServerSends: the reasons and names of a result
// reach the terminal as sent, so the decode scrubs terminal control bytes.
func TestBackupRunSanitizesWhatTheServerSends(t *testing.T) {
	esc := string(rune(0x1b))
	body, err := json.Marshal(map[string]any{
		"type": "BACKUP", "organization": "acme", "total": 1,
		"tasks":   []map[string]any{{"task": "T1", "device_uuid": "u1", "device_name": "fw" + esc + "[2J-a", "status": "PENDING", "expires_at": "2026-10-20T18:21:07Z"}},
		"skipped": []map[string]any{{"device_uuid": "u2", "device_name": "fw-b", "code": "BACKUP_NOT_ENABLED", "reason": "off" + esc + "[31m"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `\u001b`) {
		t.Fatalf("the fixture carries no control byte: %s", body)
	}
	svc, _ := backupRunServer(t, http.StatusCreated, string(body))

	result, err := svc.BackupRun(context.Background(), "acme", BackupRunOpts{AllDevices: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Tasks[0].DeviceName+result.Skipped[0].Reason, esc) {
		t.Errorf("a control byte survived the decode: %+v", result)
	}
	if !strings.Contains(result.Skipped[0].Reason, "off") {
		t.Errorf("the printable text was lost: %q", result.Skipped[0].Reason)
	}
}
