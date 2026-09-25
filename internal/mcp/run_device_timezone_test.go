package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// The instant every test here schedules. It is absolute and far enough out
// that it never falls into the past while the suite runs, and it sits in
// June, where America/Sao_Paulo is at -03:00 and Europe/Lisbon at +01:00 —
// a real four-hour disagreement, not an artifact of a DST boundary.
const (
	factsTestAt       = "2030-06-15 03:00"
	factsDeviceTZ     = "America/Sao_Paulo"
	factsMismatchedTZ = "Europe/Lisbon"
)

// deviceFactsServer serves one device with a timezone fact at the device
// endpoint and a run result at the tasks endpoint, counting how often the
// device endpoint is consulted.
func deviceFactsServer(t *testing.T, facts string) (*httptest.Server, *int32) {
	t.Helper()
	var deviceHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organizations/acme/devices/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deviceHits, 1)
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/organizations/acme/devices/")
		w.Header().Set("Content-Type", "application/json")
		fmtFacts := ""
		if facts != "" {
			fmtFacts = `, "facts": ` + facts
		}
		w.Write([]byte(`{"name":"` + name + `","uuid":"1b0f0a2e-0000-4000-8000-000000000001","status":"ENABLED","organization":"acme"` + fmtFacts + `}`))
	})
	mux.HandleFunc("/api/v1/organizations/acme/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.RunResult{
			Type:         "REBOOT",
			Organization: "acme",
			Total:        1,
			Tasks:        []models.RunTaskItem{{Task: "t-1", DeviceName: "fw-01", Status: "PENDING"}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &deviceHits
}

const saoPauloFacts = `{"v":1,"timezone":{"name":"America/Sao_Paulo","utc_offset_sec":-10800,"abbrev":"-03"}}`

// previewData runs one unconfirmed scheduling call and returns its data map.
func previewData(t *testing.T, s *Server, in *runInput) map[string]interface{} {
	t.Helper()
	result, err := s.runCommand(context.Background(), "restart", models.TaskTypeReboot, nil, in)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %v", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected a data map, got %T", resp.Data)
	}
	return data
}

// TestSchedulingEchoNamesTheDeviceTimezone: a single-device target whose
// agent reports a timezone gets the instant echoed as the device reads it,
// alongside the instant as requested. Nothing about what is sent changes.
func TestSchedulingEchoNamesTheDeviceTimezone(t *testing.T) {
	srv, hits := deviceFactsServer(t, saoPauloFacts)
	s := newTestServer(t, srv, "acme")

	data := previewData(t, s, &runInput{
		Devices:  []string{"fw-01"},
		At:       factsTestAt,
		Timezone: factsDeviceTZ,
	})

	if got := data["device_timezone"]; got != factsDeviceTZ {
		t.Fatalf("device_timezone = %v, want %q", got, factsDeviceTZ)
	}
	local, _ := data["scheduled_at_device_local"].(string)
	if !strings.HasPrefix(local, "2030-06-15T03:00:00-03:00") {
		t.Fatalf("scheduled_at_device_local = %q, want the instant as the device reads it", local)
	}
	// Requested and device zone agree here, so there is nothing to warn about.
	if w, present := data["warning"]; present {
		t.Fatalf("no warning expected when the zones agree, got %v", w)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Fatalf("expected exactly one device lookup, got %d", *hits)
	}
}

// TestSchedulingEchoWarnsOnTimezoneMismatch is the point of the feature: the
// caller asked for a zone the device is not in, and finds that out before the
// firewall reboots at the wrong hour. The warning says so; it does not
// substitute the device's zone.
func TestSchedulingEchoWarnsOnTimezoneMismatch(t *testing.T) {
	srv, _ := deviceFactsServer(t, saoPauloFacts)
	s := newTestServer(t, srv, "acme")

	data := previewData(t, s, &runInput{
		Devices:  []string{"fw-01"},
		At:       factsTestAt,
		Timezone: factsMismatchedTZ,
	})

	warning, _ := data["warning"].(string)
	if warning == "" {
		t.Fatalf("expected a mismatch warning, got data %v", data)
	}
	for _, want := range []string{factsDeviceTZ, "fw-01"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning must name %q: %q", want, warning)
		}
	}
	// The instant actually scheduled is the one the caller asked for, in the
	// zone the caller named. Nothing here adopts the device's zone.
	if got := data["scheduled_at"]; got != "2030-06-15T02:00:00Z" {
		t.Fatalf("scheduled_at = %v, want the requested zone's instant unchanged", got)
	}
	if local, _ := data["scheduled_at_device_local"].(string); !strings.HasPrefix(local, "2030-06-14T23:00:00-03:00") {
		t.Fatalf("scheduled_at_device_local = %q, want the same instant read in the device's zone", local)
	}
}

// TestSchedulingEchoSkipsMultiDeviceTargets: an OU, an org-wide run or
// several devices can span timezones, so naming one device's zone as "the"
// device timezone would be the same confident wrong answer in new clothes.
// The device endpoint must not even be consulted.
func TestSchedulingEchoSkipsMultiDeviceTargets(t *testing.T) {
	cases := map[string]*runInput{
		"two devices": {Devices: []string{"fw-01", "fw-02"}, At: factsTestAt, Timezone: factsDeviceTZ},
		"an OU":       {OUs: []string{"branches"}, At: factsTestAt, Timezone: factsDeviceTZ},
		"the org":     {Org: true, At: factsTestAt, Timezone: factsDeviceTZ},
		"one device and one OU": {
			Devices: []string{"fw-01"}, OUs: []string{"branches"},
			At: factsTestAt, Timezone: factsDeviceTZ,
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			srv, hits := deviceFactsServer(t, saoPauloFacts)
			s := newTestServer(t, srv, "acme")
			data := previewData(t, s, in)
			for _, key := range []string{"device_timezone", "scheduled_at_device_local", "warning"} {
				if _, present := data[key]; present {
					t.Fatalf("%s target must not carry %s: %v", name, key, data)
				}
			}
			if atomic.LoadInt32(hits) != 0 {
				t.Fatalf("%s target must not consult a device: %d lookups", name, *hits)
			}
			// The ordinary schedule echo is untouched.
			if _, present := data["scheduled_at"]; !present {
				t.Fatalf("the schedule echo itself must survive: %v", data)
			}
		})
	}
}

// TestSchedulingEchoSkipsDeviceWithoutFacts: an older agent reports no
// timezone, and the response says nothing rather than guessing.
func TestSchedulingEchoSkipsDeviceWithoutFacts(t *testing.T) {
	srv, _ := deviceFactsServer(t, "")
	s := newTestServer(t, srv, "acme")

	data := previewData(t, s, &runInput{
		Devices:  []string{"fw-01"},
		At:       factsTestAt,
		Timezone: factsDeviceTZ,
	})
	for _, key := range []string{"device_timezone", "scheduled_at_device_local", "warning"} {
		if _, present := data[key]; present {
			t.Fatalf("a device with no facts must not produce %s: %v", key, data)
		}
	}
}

// TestSchedulingEchoAbsentWithoutAnInstant: an immediate run has no instant
// to read in any timezone, so there is nothing to echo and no reason to
// spend an API call finding out.
func TestSchedulingEchoAbsentWithoutAnInstant(t *testing.T) {
	srv, hits := deviceFactsServer(t, saoPauloFacts)
	s := newTestServer(t, srv, "acme")

	data := previewData(t, s, &runInput{Devices: []string{"fw-01"}})
	if _, present := data["device_timezone"]; present {
		t.Fatalf("an immediate run must not carry a device timezone: %v", data)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Fatalf("an immediate run must not consult a device: %d lookups", *hits)
	}
}

// TestSchedulingEchoSurvivesADeviceLookupFailure: the echo is informational,
// so an unreachable or unknown device must degrade to silence rather than
// failing a scheduling call that is otherwise valid.
func TestSchedulingEchoSurvivesADeviceLookupFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organizations/acme/devices/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newTestServer(t, srv, "acme")

	data := previewData(t, s, &runInput{
		Devices:  []string{"fw-01"},
		At:       factsTestAt,
		Timezone: factsDeviceTZ,
	})
	if _, present := data["device_timezone"]; present {
		t.Fatalf("a failed lookup must add nothing: %v", data)
	}
	if _, present := data["scheduled_at"]; !present {
		t.Fatalf("the scheduling call itself must still succeed: %v", data)
	}
}

// TestSchedulingEchoReachesTheConfirmedResponse: the preview is not the only
// place the caller sees the mismatch — the executed response carries it too,
// so a model that skipped straight to confirm=true still gets told.
func TestSchedulingEchoReachesTheConfirmedResponse(t *testing.T) {
	srv, _ := deviceFactsServer(t, saoPauloFacts)
	s := newTestServer(t, srv, "acme")

	result, err := s.runCommand(context.Background(), "restart", models.TaskTypeReboot, nil, &runInput{
		Devices:  []string{"fw-01"},
		At:       factsTestAt,
		Timezone: factsMismatchedTZ,
		Confirm:  true,
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %v", resp.Error)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["device_timezone"] != factsDeviceTZ {
		t.Fatalf("executed response must name the device timezone: %v", data)
	}
	if w, _ := data["warning"].(string); w == "" {
		t.Fatalf("executed response must carry the mismatch warning: %v", data)
	}
}

// TestFirmwareUpgradeCarriesTheDeviceTimezoneEcho: firmware_upgrade has its
// own handler rather than going through makeRunHandler, and it is the most
// destructive of the six — it must not be the one tool that stays silent.
func TestFirmwareUpgradeCarriesTheDeviceTimezoneEcho(t *testing.T) {
	srv, _ := deviceFactsServer(t, saoPauloFacts)
	s := newTestServer(t, srv, "acme")

	result, err := s.firmwareUpgrade(context.Background(), &runInput{
		Devices:  []string{"fw-01"},
		Mode:     "minor",
		At:       factsTestAt,
		Timezone: factsMismatchedTZ,
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	data, _ := resp.Data.(map[string]interface{})
	if data["device_timezone"] != factsDeviceTZ {
		t.Fatalf("firmware_upgrade preview must name the device timezone: %v", data)
	}
	if w, _ := data["warning"].(string); w == "" {
		t.Fatalf("firmware_upgrade preview must carry the mismatch warning: %v", data)
	}
}

// TestSchedulingToolDescriptionsNameTheDeviceTimezone pins the wording the
// generated MCP catalogs ship: a model reading only the parameter docs has to
// learn that the device zone is shown, never adopted.
func TestSchedulingToolDescriptionsNameTheDeviceTimezone(t *testing.T) {
	for _, want := range []string{"device_timezone", "scheduled_at_device_local"} {
		if !strings.Contains(atParamDescription, want) {
			t.Fatalf("at description must name %q", want)
		}
	}
	if !strings.Contains(timezoneParamDescription, "no device-timezone default") {
		t.Fatalf("timezone description must rule out a device-timezone default: %q", timezoneParamDescription)
	}
}
