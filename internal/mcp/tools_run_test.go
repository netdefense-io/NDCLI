package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// isTaskCreation reports whether a request is the POST that actually creates
// tasks, as opposed to the device read the scheduling tools now make to echo
// the target device's own timezone. The invariant these tests protect is
// "no task is created without confirm", not "no request leaves the process":
// the device read is a GET, it mutates nothing, and it is skipped entirely for
// multi-device targets and for runs with no scheduled instant.
func isTaskCreation(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tasks")
}

// deviceLookupNotFound answers the scheduling tools' device read with a 404,
// which they tolerate by adding nothing to the echo. Tests that care about
// the echo itself serve a device with facts instead (run_device_timezone_test.go).
func deviceLookupNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
}

// specRegisterJSON is a minimal, valid ScheduledTaskRegisterResult payload.
func specRegisterJSON(code, scheduleName string) map[string]interface{} {
	return map[string]interface{}{
		"code":          code,
		"kind":          "RUN",
		"request":       map[string]interface{}{},
		"enabled":       true,
		"schedule_name": scheduleName,
		"created_by":    "admin@acme.com",
		"last_fired_at": nil,
		"created_at":    "2026-05-01T00:00:00+00:00",
		"updated_at":    "2026-05-01T00:00:00+00:00",
	}
}

// TestRunCommand_ScheduleGatedByConfirm verifies that registering a
// recurring spec via --schedule is gated by confirm just like an immediate
// run: without confirm, the tasks endpoint must not be hit and the response
// must be a preview; with confirm=true, the spec is registered. Fails
// against pre-fix code, where the schedule branch ran before the confirm
// check and always hit the endpoint.
func TestRunCommand_ScheduleGatedByConfirm(t *testing.T) {
	var hits int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(specRegisterJSON("AbCd1234", "nightly"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		Schedule: "nightly",
	}

	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("expected 0 requests before confirm, got %d", hits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected an unconfirmed schedule call to return a preview, got %v", resp.Data)
	}

	input.Confirm = true
	result, err = s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 request after confirm, got %d", hits)
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
}

// TestFirmwareUpgrade_ScheduleGatedByConfirm is the same regression, but for
// the dedicated firmware_upgrade handler core, which has its own
// schedule-vs-confirm ordering.
func TestFirmwareUpgrade_ScheduleGatedByConfirm(t *testing.T) {
	var hits int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(specRegisterJSON("EfGh5678", "monthly"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		Mode:     "minor",
		Schedule: "monthly",
	}

	result, err := s.firmwareUpgrade(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("expected 0 requests before confirm, got %d", hits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected an unconfirmed schedule call to return a preview, got %v", resp.Data)
	}

	input.Confirm = true
	result, err = s.firmwareUpgrade(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 request after confirm, got %d", hits)
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
}

// runResultJSON is a minimal, valid RunResult payload echoing whatever
// scheduled_at the request carried — the same shape NDManager returns.
func runResultJSON(taskType, scheduledAt string) map[string]interface{} {
	return map[string]interface{}{
		"type":         taskType,
		"organization": "acme",
		"scheduled_at": scheduledAt,
		"total":        1,
		"tasks": []map[string]interface{}{
			{"task": "aiW6RPTR", "device": "fw-01", "status": "SCHEDULED"},
		},
	}
}

// captureRunRequest runs fn against a stub tasks endpoint and returns the
// request body it sent.
func captureRunRequest(t *testing.T, fn func(s *Server)) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(runResultJSON("SHUTDOWN", "2126-09-22T02:00:00Z"))
	}))
	defer srv.Close()
	fn(newTestServer(t, srv, "acme"))
	return body
}

// TestRunCommand_ExplicitOffsetIsSentAsUTC: an `at` carrying a -03:00 offset
// used to be forwarded verbatim, and the control plane stored the
// wall-clock reading as UTC — the task fired three hours early.
func TestRunCommand_ExplicitOffsetIsSentAsUTC(t *testing.T) {
	body := captureRunRequest(t, func(s *Server) {
		input := &runInput{
			Devices: []string{"fw-01"},
			At:      "2126-09-21T23:00:00-03:00",
			Confirm: true,
		}
		if _, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	})
	if got := string(body["scheduled_at"]); got != `"2126-09-22T02:00:00Z"` {
		t.Fatalf("scheduled_at = %s, want \"2126-09-22T02:00:00Z\"", got)
	}
}

// TestFirmwareUpgrade_ExplicitOffsetIsSentAsUTC covers the same bug on the
// dedicated firmware-upgrade handler, which builds its own RunOpts.
func TestFirmwareUpgrade_ExplicitOffsetIsSentAsUTC(t *testing.T) {
	body := captureRunRequest(t, func(s *Server) {
		input := &runInput{
			Devices: []string{"fw-01"},
			Mode:    "minor",
			At:      "2126-09-21T23:00:00-03:00",
			Confirm: true,
		}
		if _, err := s.firmwareUpgrade(context.Background(), input); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	})
	if got := string(body["scheduled_at"]); got != `"2126-09-22T02:00:00Z"` {
		t.Fatalf("scheduled_at = %s, want \"2126-09-22T02:00:00Z\"", got)
	}
}

// TestRunCommand_BareTimestampNeedsATimezone: the MCP caller is a model that
// may be reasoning about any of three clocks, so a bare timestamp is refused
// rather than silently resolved.
func TestRunCommand_BareTimestampNeedsATimezone(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "2126-09-21 23:00", Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("expected no request for an ambiguous at, got %d", hits)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected a bare timestamp with no timezone to be refused")
	}
	if resp.Error.Code != "INVALID_INPUT" {
		t.Fatalf("error code = %q, want INVALID_INPUT", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "timezone") {
		t.Fatalf("error message %q must tell the caller to pass timezone", resp.Error.Message)
	}
}

// TestRunCommand_BareTimestampWithTimezone resolves against the named zone,
// not the host's.
func TestRunCommand_BareTimestampWithTimezone(t *testing.T) {
	body := captureRunRequest(t, func(s *Server) {
		input := &runInput{
			Devices:  []string{"fw-01"},
			At:       "2126-09-21 23:00",
			Timezone: "America/Sao_Paulo",
			Confirm:  true,
		}
		if _, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	})
	if got := string(body["scheduled_at"]); got != `"2126-09-22T02:00:00Z"` {
		t.Fatalf("scheduled_at = %s, want \"2126-09-22T02:00:00Z\"", got)
	}
}

func TestRunCommand_InvalidTimezoneIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent for an invalid timezone")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		At:       "2126-09-21 23:00",
		Timezone: "Mars/Olympus_Mons",
		Confirm:  true,
	}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success || resp.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got success=%v error=%+v", resp.Success, resp.Error)
	}
}

// TestRunCommand_RelativeOffsetIsSentAsUTC: a relative offset carries its own
// anchor, so timezone is not required and the wire value is still absolute.
func TestRunCommand_RelativeOffsetIsSentAsUTC(t *testing.T) {
	body := captureRunRequest(t, func(s *Server) {
		input := &runInput{Devices: []string{"fw-01"}, At: "2h", Confirm: true}
		if _, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	})
	var sent string
	if err := json.Unmarshal(body["scheduled_at"], &sent); err != nil {
		t.Fatalf("scheduled_at is not a string: %v", err)
	}
	if !strings.HasSuffix(sent, "Z") {
		t.Fatalf("scheduled_at = %q, want a Z-suffixed UTC instant", sent)
	}
	parsed, err := time.Parse(time.RFC3339, sent)
	if err != nil {
		t.Fatalf("scheduled_at %q is not RFC3339: %v", sent, err)
	}
	if delta := time.Until(parsed); delta < 110*time.Minute || delta > 130*time.Minute {
		t.Fatalf("2h resolved to %v from now", delta)
	}
}

func TestRunCommand_PastAtIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent for a past at")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "2020-01-01T00:00:00Z", Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success || !strings.Contains(resp.Error.Message, "in the past") {
		t.Fatalf("expected an in-the-past refusal, got success=%v error=%+v", resp.Success, resp.Error)
	}
}

// TestRunCommand_PreviewEchoesTheResolvedInstant: the preview is what the model
// restates to the user before confirming, so it has to carry the resolved
// instant as data — in UTC, in the timezone used, and against "now".
func TestRunCommand_PreviewEchoesTheResolvedInstant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("an unconfirmed call must not reach the endpoint")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "2126-09-21T23:00:00-03:00"}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["preview"] != true {
		t.Fatalf("expected a preview, got %v", resp.Data)
	}
	if data["scheduled_at"] != "2126-09-22T02:00:00Z" {
		t.Fatalf("preview scheduled_at = %v, want 2126-09-22T02:00:00Z", data["scheduled_at"])
	}
	if data["scheduled_at_local"] != "2126-09-21T23:00:00-03:00" {
		t.Fatalf("preview scheduled_at_local = %v, want the input rendered back", data["scheduled_at_local"])
	}
	if data["scheduled_at_timezone"] != "UTC-03:00" {
		t.Fatalf("preview scheduled_at_timezone = %v, want UTC-03:00", data["scheduled_at_timezone"])
	}
	if _, ok := data["now_utc"].(string); !ok {
		t.Fatalf("preview must carry now_utc, got %v", data["now_utc"])
	}
}

// TestRunCommand_ResponseEchoesTheResolvedInstant: same fields on the executed
// call, so the model can state the schedule back after the fact too.
func TestRunCommand_ResponseEchoesTheResolvedInstant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Echo the buggy naive form the control plane used to return — the
		// client must still report the instant it actually sent.
		json.NewEncoder(w).Encode(runResultJSON("SHUTDOWN", "2126-09-21T23:00:00"))
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "2126-09-21T23:00:00-03:00", Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected data shape: %v", resp.Data)
	}
	if data["scheduled_at"] != "2126-09-22T02:00:00Z" {
		t.Fatalf("scheduled_at = %v, want the resolved UTC instant", data["scheduled_at"])
	}
	if data["scheduled_at_timezone"] != "UTC-03:00" {
		t.Fatalf("scheduled_at_timezone = %v, want UTC-03:00", data["scheduled_at_timezone"])
	}
}

// TestRunCommand_AtWithScheduleIsRefused: a recurring spec carries no
// scheduled_at on the wire, so a preview echoing a one-time instant for one
// would describe a firing that never happens. Refused before the preview.
func TestRunCommand_AtWithScheduleIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent when at and schedule are combined")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{
		Devices:  []string{"fw-01"},
		At:       "2126-09-21T23:00:00-03:00",
		Schedule: "nightly",
	}
	for _, confirm := range []bool{false, true} {
		input.Confirm = confirm
		result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		var resp ToolResponse
		decodeToolResult(t, result, &resp)
		if resp.Success {
			t.Fatalf("confirm=%v: expected at+schedule to be refused, got %v", confirm, resp.Data)
		}
		if resp.Error.Code != "INVALID_INPUT" {
			t.Fatalf("confirm=%v: error code = %q, want INVALID_INPUT", confirm, resp.Error.Code)
		}
	}
}

// TestRunCommand_TimezoneIgnoredForAnchoredInput pins the parameter's own
// contract: `timezone` is consulted only where it changes the answer, so a
// stale value alongside a relative offset must not be a hard failure.
func TestRunCommand_TimezoneIgnoredForAnchoredInput(t *testing.T) {
	for _, at := range []string{"2h", "2126-09-21T23:00:00-03:00", "2126-09-22T02:00:00Z"} {
		body := captureRunRequest(t, func(s *Server) {
			input := &runInput{
				Devices:  []string{"fw-01"},
				At:       at,
				Timezone: "Mars/Olympus_Mons",
				Confirm:  true,
			}
			if _, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input); err != nil {
				t.Fatalf("at=%q: unexpected transport error: %v", at, err)
			}
		})
		var sent string
		if err := json.Unmarshal(body["scheduled_at"], &sent); err != nil {
			t.Fatalf("at=%q: scheduled_at missing or not a string: %v", at, err)
		}
		if !strings.HasSuffix(sent, "Z") {
			t.Fatalf("at=%q: scheduled_at = %q, want a Z-suffixed UTC instant", at, sent)
		}
	}
}

// TestRunCommand_UnparseableAtSaysSo: an input that no timezone can rescue
// must fail as a parse error, not as "pass a timezone" — otherwise the caller
// burns a round trip retrying with one.
func TestRunCommand_UnparseableAtSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent for an unparseable at")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "banana", Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected an unparseable at to be refused")
	}
	if !strings.Contains(resp.Error.Message, "could not parse") {
		t.Fatalf("error = %q, want a parse failure rather than a timezone prompt", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "Pass timezone") {
		t.Fatalf("error = %q, must not ask for a timezone that cannot help", resp.Error.Message)
	}
}

// TestRunCommand_PastBareTimestampSaysSo: a bare timestamp already past under
// every timezone must not be answered with "pass a timezone" — no timezone can
// rescue it, and following that advice costs a round trip.
func TestRunCommand_PastBareTimestampSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent for a past at")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	input := &runInput{Devices: []string{"fw-01"}, At: "2020-01-01 00:00", Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected a past bare timestamp to be refused")
	}
	if !strings.Contains(resp.Error.Message, "in the past") {
		t.Fatalf("error = %q, want an in-the-past refusal", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "Pass timezone") {
		t.Fatalf("error = %q, must not ask for a timezone that cannot help", resp.Error.Message)
	}
}

// TestRunCommand_NearFutureBareTimestampStillAsksForATimezone guards the other
// side of that check: a time that is future in some zones and past in others is
// exactly the ambiguity the timezone parameter exists to resolve.
func TestRunCommand_NearFutureBareTimestampStillAsksForATimezone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("no request should be sent for an ambiguous at")
	}))
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	at := time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02 15:04")
	input := &runInput{Devices: []string{"fw-01"}, At: at, Confirm: true}
	result, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil, input)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success || !strings.Contains(resp.Error.Message, "Pass timezone") {
		t.Fatalf("expected a timezone prompt, got success=%v error=%+v", resp.Success, resp.Error)
	}
}

// TestRunCommand_PreviewInstantSurvivesTheConfirmingCall: a relative offset is
// re-resolved on the confirming call, so the preview tells the caller to send
// the resolved instant back instead. Feeding it back must produce exactly the
// instant the preview showed.
func TestRunCommand_PreviewInstantSurvivesTheConfirmingCall(t *testing.T) {
	s := newTestServer(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTaskCreation(r) {
			deviceLookupNotFound(w)
			return
		}
		t.Error("an unconfirmed call must not reach the endpoint")
	})), "acme")

	preview, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil,
		&runInput{Devices: []string{"fw-01"}, At: "30m"})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, preview, &resp)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected preview shape: %v", resp.Data)
	}
	previewed, ok := data["scheduled_at"].(string)
	if !ok {
		t.Fatalf("preview carries no scheduled_at: %v", data)
	}
	hint, ok := data["confirm_hint"].(string)
	if !ok || !strings.Contains(hint, previewed) {
		t.Fatalf("confirm_hint = %v, want it to name the resolved instant %q", data["confirm_hint"], previewed)
	}

	body := captureRunRequest(t, func(s *Server) {
		if _, err := s.runCommand(context.Background(), "poweroff", models.TaskTypeShutdown, nil,
			&runInput{Devices: []string{"fw-01"}, At: previewed, Confirm: true}); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	})
	var sent string
	if err := json.Unmarshal(body["scheduled_at"], &sent); err != nil {
		t.Fatalf("scheduled_at missing or not a string: %v", err)
	}
	if sent != previewed {
		t.Fatalf("confirmed instant = %q, want the previewed %q", sent, previewed)
	}
}
