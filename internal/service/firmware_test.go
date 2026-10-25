package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// firmwareRequest is what the fake NDManager saw.
type firmwareRequest struct {
	method string
	path   string // escaped, as it went over the wire
	query  url.Values
	body   map[string]interface{}
	hits   int
}

// firmwareServer answers every request with status and body and records the
// last one it got.
func firmwareServer(t *testing.T, status int, body string) (*httptest.Server, *firmwareRequest) {
	t.Helper()
	seen := &firmwareRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.hits++
		seen.method, seen.path, seen.query, seen.body = r.Method, r.URL.EscapedPath(), r.URL.Query(), nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, &seen.body); err != nil {
				t.Errorf("request body is not JSON: %v\n%s", err, raw)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

const (
	cfgBody     = `{"organization_name":"acme","status":"ENABLED","reboot":true,"plan_enabled":true,"tracks":[{"track":"fast","soak_days":1,"min_success":0,"scheduled":false,"devices":{"total":0}}],"created_at":"2026-10-08T12:00:00Z","message":"ok"}`
	trackBody   = `{"track":"regular","soak_days":9,"min_success":1,"scheduled":false,"devices":{"total":0}}`
	assignBody  = `{"organization_name":"acme","track":"fast","results":[{"device_name":"fw-a","result":"added","track":"fast","state":"enrolling","notes":[]}],"cancelled_tasks":0}`
	devicesBody = `{"items":[{"device_name":"fw-a","device_uuid":"u","device_status":"ENABLED"}],"total":1,"page":1,"per_page":50,"pages":1}`
	historyBody = `{"device_name":"fw-a","items":[],"total":0,"page":1,"per_page":50,"pages":1}`
	rolloutBody = `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[],"failures":[],"halts":[]}`
	haltsBody   = `{"items":[],"total":0,"page":1,"per_page":50,"pages":1}`
	haltBody    = `{"halt":{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"2026-10-08T12:00:00Z","halted_by":"a@b.c"},"cancelled_tasks":1}`
	promoBody   = `{"version":"26.7.5","results":[{"track":"fast","result":"approved","target":"26.7.5"}]}`
	clearBody   = `{"version":"26.7.5","cleared":2}`
	deleteBody  = `{"message":"Firmware configuration deleted"}`
)

func TestFirmwareRoutes(t *testing.T) {
	two, five := 2, 5
	detach := (*string)(nil)
	night := "saturday-night"
	for _, tc := range []struct {
		name     string
		response string
		call     func(s *Service) (interface{}, error)
		method   string
		path     string
		query    map[string]string
		body     map[string]interface{}
	}{
		{"config get", cfgBody,
			func(s *Service) (interface{}, error) { return s.FirmwareConfigGet(context.Background(), "acme") },
			"GET", "/api/v1/organizations/acme/firmware/config", nil, nil},
		{"config create defaults to a reboot", cfgBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareConfigCreate(context.Background(), "acme", FirmwareConfigCreateOpts{})
			},
			"POST", "/api/v1/organizations/acme/firmware/config", nil, map[string]interface{}{"reboot": true}},
		{"config create without a reboot", cfgBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareConfigCreate(context.Background(), "acme", FirmwareConfigCreateOpts{NoReboot: true})
			},
			"POST", "/api/v1/organizations/acme/firmware/config", nil, map[string]interface{}{"reboot": false}},
		{"config update", cfgBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareConfigUpdate(context.Background(), "acme", false)
			},
			"PUT", "/api/v1/organizations/acme/firmware/config", nil, map[string]interface{}{"reboot": false}},
		{"config enable", cfgBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareConfigSetStatus(context.Background(), "acme", true)
			},
			"PUT", "/api/v1/organizations/acme/firmware/config/status", nil, map[string]interface{}{"status": "ENABLED"}},
		{"config disable", cfgBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareConfigSetStatus(context.Background(), "acme", false)
			},
			"PUT", "/api/v1/organizations/acme/firmware/config/status", nil, map[string]interface{}{"status": "DISABLED"}},
		{"config delete", deleteBody,
			func(s *Service) (interface{}, error) { return s.FirmwareConfigDelete(context.Background(), "acme") },
			"DELETE", "/api/v1/organizations/acme/firmware/config", nil, nil},
		{"track list reads the configuration", cfgBody,
			func(s *Service) (interface{}, error) { return s.FirmwareTrackList(context.Background(), "acme") },
			"GET", "/api/v1/organizations/acme/firmware/config", nil, nil},
		{"track get lower-cases the track", trackBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareTrackGet(context.Background(), "acme", " Regular ")
			},
			"GET", "/api/v1/organizations/acme/firmware/tracks/regular", nil, nil},
		{"track update sends only what is set", trackBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{SoakDays: &five})
			},
			"PUT", "/api/v1/organizations/acme/firmware/tracks/regular", nil, map[string]interface{}{"soak_days": float64(5)}},
		{"track update sends both", trackBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareTrackUpdate(context.Background(), "acme", "stable", FirmwareTrackUpdateOpts{SoakDays: &five, MinSuccess: &two})
			},
			"PUT", "/api/v1/organizations/acme/firmware/tracks/stable", nil,
			map[string]interface{}{"soak_days": float64(5), "min_success": float64(2)}},
		{"track update accepts zero", trackBody,
			func(s *Service) (interface{}, error) {
				zero := 0
				return s.FirmwareTrackUpdate(context.Background(), "acme", "fast", FirmwareTrackUpdateOpts{MinSuccess: &zero})
			},
			"PUT", "/api/v1/organizations/acme/firmware/tracks/fast", nil, map[string]interface{}{"min_success": float64(0)}},
		{"track schedule attach", trackBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareTrackSetSchedule(context.Background(), "acme", "fast", &night)
			},
			"PUT", "/api/v1/organizations/acme/firmware/tracks/fast/schedule", nil, map[string]interface{}{"schedule": "saturday-night"}},
		{"track schedule detach sends null", trackBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareTrackSetSchedule(context.Background(), "acme", "fast", detach)
			},
			"PUT", "/api/v1/organizations/acme/firmware/tracks/fast/schedule", nil, map[string]interface{}{"schedule": nil}},
		{"device assign names devices one by one", assignBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a", " fw-b ", "fw-a", ""}, FirmwareDeviceAssignOpts{Track: "FAST"})
			},
			"PUT", "/api/v1/organizations/acme/firmware/devices", nil,
			map[string]interface{}{"devices": []interface{}{"fw-a", "fw-b"}, "track": "fast"}},
		{"device assign to unmanaged", assignBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "unmanaged"})
			},
			"PUT", "/api/v1/organizations/acme/firmware/devices", nil,
			map[string]interface{}{"devices": []interface{}{"fw-a"}, "track": "unmanaged"}},
		{"device list defaults", devicesBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{})
			},
			"GET", "/api/v1/organizations/acme/firmware/devices", map[string]string{"page": "1", "per_page": "50"}, nil},
		{"device list filters", devicesBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{
					Track: "Unmanaged", State: "HELD", Name: " fw ", Page: 2, PerPage: 25,
				})
			},
			"GET", "/api/v1/organizations/acme/firmware/devices",
			map[string]string{"track": "unmanaged", "state": "held", "name": "fw", "page": "2", "per_page": "25"}, nil},
		{"history", historyBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHistory(context.Background(), "acme", "fw-a", 3, 10)
			},
			"GET", "/api/v1/organizations/acme/firmware/devices/fw-a/history", map[string]string{"page": "3", "per_page": "10"}, nil},
		{"history escapes the device name", historyBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHistory(context.Background(), "acme", "edge/fw 1", 0, 0)
			},
			"GET", "/api/v1/organizations/acme/firmware/devices/edge%2Ffw%201/history", map[string]string{"page": "1", "per_page": "50"}, nil},
		{"rollout", rolloutBody,
			func(s *Service) (interface{}, error) { return s.FirmwareRollout(context.Background(), "acme") },
			"GET", "/api/v1/organizations/acme/firmware/status", nil, nil},
		{"halts list the ones in force", haltsBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{})
			},
			"GET", "/api/v1/organizations/acme/firmware/halts", map[string]string{"active": "true", "page": "1", "per_page": "50"}, nil},
		{"halts list with the lifted ones", haltsBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{IncludeLifted: true})
			},
			"GET", "/api/v1/organizations/acme/firmware/halts", map[string]string{"active": "false"}, nil},
		{"halt", haltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHalt(context.Background(), "acme", "", " 26.7.5 ", " regression ", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/halts", nil,
			map[string]interface{}{"version": "26.7.5", "reason": "regression"}},
		{"unhalt", haltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareUnhalt(context.Background(), "acme", "minor", "24.7.12_2", 0)
			},
			"DELETE", "/api/v1/organizations/acme/firmware/halts/24.7.12_2", nil, nil},
		{"promote", promoBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwarePromote(context.Background(), "acme", "", "26.7.5", "ALL", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/promote", nil,
			map[string]interface{}{"version": "26.7.5", "track": "all"}},
		{"failures clear in one track", clearBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareFailuresClear(context.Background(), "acme", "", "26.7.5", "Fast", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/failures/clear", nil,
			map[string]interface{}{"version": "26.7.5", "track": "fast"}},
		{"failures clear in every track sends no track", clearBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareFailuresClear(context.Background(), "acme", "minor", "26.7.5", "", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/failures/clear", nil,
			map[string]interface{}{"version": "26.7.5"}},
		{"the organization is escaped", cfgBody,
			func(s *Service) (interface{}, error) { return s.FirmwareConfigGet(context.Background(), "ac me/x") },
			"GET", "/api/v1/organizations/ac%20me%2Fx/firmware/config", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, http.StatusOK, tc.response)
			out, err := tc.call(newTestService(t, srv))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatal("no result")
			}
			if seen.method != tc.method || seen.path != tc.path {
				t.Errorf("sent %s %s, want %s %s", seen.method, seen.path, tc.method, tc.path)
			}
			for k, want := range tc.query {
				if got := seen.query.Get(k); got != want {
					t.Errorf("query %s = %q, want %q", k, got, want)
				}
			}
			if !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("body = %v, want %v", seen.body, tc.body)
			}
		})
	}
}

func TestFirmwareResultsCarryWhatTheClientKnows(t *testing.T) {
	srv, _ := firmwareServer(t, http.StatusOK, deleteBody)
	deleted, err := newTestService(t, srv).FirmwareConfigDelete(context.Background(), "acme")
	if err != nil || deleted.Organization != "acme" || deleted.Message != "Firmware configuration deleted" {
		t.Errorf("delete = %+v, %v; want the organization filled in", deleted, err)
	}

	srv, _ = firmwareServer(t, http.StatusOK, clearBody)
	cleared, err := newTestService(t, srv).FirmwareFailuresClear(context.Background(), "acme", "", "26.7.5", "stable", 0)
	if err != nil || cleared.Track != "stable" || cleared.Cleared != 2 {
		t.Errorf("clear = %+v, %v; want the track the clear was limited to", cleared, err)
	}
	srv, _ = firmwareServer(t, http.StatusOK, clearBody)
	cleared, err = newTestService(t, srv).FirmwareFailuresClear(context.Background(), "acme", "", "26.7.5", "", 0)
	if err != nil || cleared.Track != "" {
		t.Errorf("clear in every track = %+v, %v; want no track", cleared, err)
	}

	srv, _ = firmwareServer(t, http.StatusOK, cfgBody)
	tracks, err := newTestService(t, srv).FirmwareTrackList(context.Background(), "acme")
	if err != nil || len(tracks) != 1 || tracks[0].Track != "fast" {
		t.Errorf("track list = %+v, %v", tracks, err)
	}
}

// Nothing reaches the control plane for an input the client can see is wrong.
func TestFirmwareRefusesBadInputBeforeSending(t *testing.T) {
	neg := -1
	empty := "  "
	ok := 3
	for _, tc := range []struct {
		name string
		call func(s *Service) error
		want string // a fragment of the message
	}{
		{"unknown track on get", func(s *Service) error { _, err := s.FirmwareTrackGet(context.Background(), "acme", "slow"); return err }, `unknown track "slow"`},
		{"no track on get", func(s *Service) error { _, err := s.FirmwareTrackGet(context.Background(), "acme", " "); return err }, "track is required"},
		{"unmanaged is not a track to configure", func(s *Service) error {
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "unmanaged", FirmwareTrackUpdateOpts{SoakDays: &ok})
			return err
		}, `unknown track "unmanaged"`},
		{"nothing to update", func(s *Service) error {
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "fast", FirmwareTrackUpdateOpts{})
			return err
		}, "provide soak_days and/or min_success, or auto_apply_major"},
		{"the switch is not changed beside soak days", func(s *Service) error {
			on := true
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "fast", FirmwareTrackUpdateOpts{AutoApplyMajor: &on, SoakDays: &ok})
			return err
		}, "auto_apply_major is changed on its own"},
		{"the switch is not changed beside min success", func(s *Service) error {
			off := false
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &off, MinSuccess: &ok})
			return err
		}, "auto_apply_major is changed on its own"},
		{"unmanaged is not a track whose switch can be changed", func(s *Service) error {
			on := true
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "unmanaged", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
			return err
		}, `unknown track "unmanaged"`},
		{"negative soak", func(s *Service) error {
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "fast", FirmwareTrackUpdateOpts{SoakDays: &neg})
			return err
		}, "soak_days cannot be negative"},
		{"negative evidence", func(s *Service) error {
			_, err := s.FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{MinSuccess: &neg})
			return err
		}, "min_success cannot be negative"},
		{"a blank schedule is not a detach", func(s *Service) error {
			_, err := s.FirmwareTrackSetSchedule(context.Background(), "acme", "fast", &empty)
			return err
		}, "schedule is required"},
		{"all is not a track to assign to", func(s *Service) error {
			_, err := s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "all"})
			return err
		}, `unknown track "all"`},
		{"no devices", func(s *Service) error {
			_, err := s.FirmwareDeviceAssign(context.Background(), "acme", []string{" ", ""}, FirmwareDeviceAssignOpts{Track: "fast"})
			return err
		}, "at least one device name is required"},
		{"neither a track nor a setting", func(s *Service) error {
			_, err := s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{})
			return err
		}, "give a track, a boot_environment setting, or both"},
		{"a blank track is no track", func(s *Service) error {
			_, err := s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "  "})
			return err
		}, "give a track, a boot_environment setting, or both"},
		{"a device that leaves its track keeps no setting", func(s *Service) error {
			off := false
			_, err := s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "Unmanaged", BootEnvironment: &off})
			return err
		}, "boot_environment cannot be set while moving devices to unmanaged"},
		{"bad device filter", func(s *Service) error {
			_, err := s.FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{State: "asleep"})
			return err
		}, `unknown state "asleep"`},
		{"history without a device", func(s *Service) error {
			_, err := s.FirmwareHistory(context.Background(), "acme", " ", 1, 1)
			return err
		}, "device name is required"},
		{"halt without a reason", func(s *Service) error {
			_, err := s.FirmwareHalt(context.Background(), "acme", "", "26.7.5", "  ", 0)
			return err
		}, "reason is required"},
		{"halt without a version", func(s *Service) error {
			_, err := s.FirmwareHalt(context.Background(), "acme", "", "", "r", 0)
			return err
		}, "version is required"},
		{"halt of a major upgrade without a series", func(s *Service) error {
			_, err := s.FirmwareHalt(context.Background(), "acme", "major", " ", "r", 0)
			return err
		}, "series is required"},
		{"halt of a kind that is neither", func(s *Service) error {
			_, err := s.FirmwareHalt(context.Background(), "acme", "all", "26.7", "r", 0)
			return err
		}, `unknown kind "all"`},
		{"unhalt without a version", func(s *Service) error {
			_, err := s.FirmwareUnhalt(context.Background(), "acme", "", "", 0)
			return err
		}, "version is required"},
		{"unhalt of a major upgrade without a series", func(s *Service) error {
			_, err := s.FirmwareUnhalt(context.Background(), "acme", "major", "", 0)
			return err
		}, "series is required"},
		{"promote to unmanaged", func(s *Service) error {
			_, err := s.FirmwarePromote(context.Background(), "acme", "", "26.7.5", "unmanaged", 0)
			return err
		}, `unknown track "unmanaged"`},
		{"promote without a track", func(s *Service) error {
			_, err := s.FirmwarePromote(context.Background(), "acme", "", "26.7.5", "", 0)
			return err
		}, "track is required"},
		{"promote of a major upgrade to unmanaged", func(s *Service) error {
			_, err := s.FirmwarePromote(context.Background(), "acme", "major", "26.7", "unmanaged", 0)
			return err
		}, `unknown track "unmanaged"`},
		{"clear with a bad track", func(s *Service) error {
			_, err := s.FirmwareFailuresClear(context.Background(), "acme", "", "26.7.5", "all", 0)
			return err
		}, `unknown track "all"`},
		{"clear of a major upgrade without a series", func(s *Service) error {
			_, err := s.FirmwareFailuresClear(context.Background(), "acme", "major", "", "", 0)
			return err
		}, "series is required"},
		{"halt list of a kind that does not exist", func(s *Service) error {
			_, err := s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{Kind: "everything"})
			return err
		}, `unknown kind "everything"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, http.StatusOK, `{}`)
			err := tc.call(newTestService(t, srv))
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
				t.Fatalf("err = %#v, want a %s error", err, CodeInvalidInput)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("message %q does not contain %q", err.Error(), tc.want)
			}
			if seen.hits != 0 {
				t.Errorf("%d request(s) sent for an input that was refused", seen.hits)
			}
			// The message is shared with the MCP tools, so it names no flag.
			if strings.Contains(err.Error(), "--") {
				t.Errorf("a service message must not name a CLI flag: %q", err.Error())
			}
		})
	}
}

// The server's own refusals reach the caller with their code, status and the
// devices that block, for errors.As and api.GetErrorCode.
func TestFirmwareKeepsTheServersRefusal(t *testing.T) {
	srv, _ := firmwareServer(t, http.StatusConflict,
		`{"error":"Devices are still in a firmware track","code":"HAS_DEPENDENCIES","blocking_resources":["fw-a","fw-b"],"count":5}`)
	_, err := newTestService(t, srv).FirmwareConfigDelete(context.Background(), "acme")
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %#v, want the API error underneath", err)
	}
	if apiErr.StatusCode != http.StatusConflict || apiErr.Code != "HAS_DEPENDENCIES" || apiErr.BlockingCount != 5 ||
		!reflect.DeepEqual(apiErr.BlockingResources, []string{"fw-a", "fw-b"}) {
		t.Errorf("api error = %+v", apiErr)
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeAPIError {
		t.Errorf("err = %#v, want a %s service error", err, CodeAPIError)
	}
}

func TestFirmwareAdvice(t *testing.T) {
	vocabulary := FirmwareVocabulary{
		ConfigCreate:  "CREATE",
		ConfigEnable:  "ENABLE",
		DeviceStatus:  "STATUS",
		DeviceRelease: "RELEASE",
		TrackSchedule: "SCHEDULE " + FirmwareTrackPlaceholder,
		TrackDetach:   "DETACH " + FirmwareTrackPlaceholder,
	}
	refusal := func(t *testing.T, status int, code string) error {
		t.Helper()
		srv, _ := firmwareServer(t, status, `{"error":"the server's words","code":"`+code+`"}`)
		_, err := newTestService(t, srv).FirmwareRollout(context.Background(), "acme")
		return err
	}
	for _, tc := range []struct {
		code   string
		status int
		hints  []string
	}{
		{"FIRMWARE_NOT_CONFIGURED", 404, []string{"CREATE"}},
		{"FIRMWARE_DISABLED", 409, []string{"ENABLE"}},
		{"FIRMWARE_DEVICE_MANAGED", 409, []string{"RELEASE"}},
		// A track's schedule entry, whose refusal names no track: the hint names
		// the command that detaches the track and the one that moves it, as templates.
		{"FIRMWARE_SPEC_MANAGED", 409, []string{"detach the track from its schedule: DETACH <track>", "move it to another schedule: SCHEDULE <track>", "cannot be deleted while a track uses it"}},
		{"FIRMWARE_DEVICE_LEAVING", 409, []string{"STATUS"}},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := refusal(t, tc.status, tc.code)
			advised := FirmwareAdvice(err, vocabulary)

			if !strings.HasPrefix(advised.Error(), "the server's words\n  Hint: ") {
				t.Errorf("advice = %q, want the server's words, then a hint", advised.Error())
			}
			for _, hint := range tc.hints {
				if !strings.Contains(advised.Error(), hint) {
					t.Errorf("advice = %q, want the hint to say %q", advised.Error(), hint)
				}
			}
			// Both the code NDManager sent and the status behind it survive.
			if got := api.GetErrorCode(advised); got != tc.code {
				t.Errorf("code = %q, want %q", got, tc.code)
			}
			if !api.IsConflictError(advised) && tc.status == 409 {
				t.Error("the 409 behind the advice was lost")
			}
			var svcErr *Error
			if !errors.As(advised, &svcErr) || svcErr.Code != CodeAPIError {
				t.Errorf("advised error = %#v, want the service code kept", advised)
			}
			// Advice given once is not given again by an outer layer.
			if again := FirmwareAdvice(advised, vocabulary); again.Error() != advised.Error() {
				t.Errorf("advised twice:\n%s", again.Error())
			}
		})
	}

	t.Run("a refusal with no advice comes back as it was", func(t *testing.T) {
		err := refusal(t, 409, "FIRMWARE_ALREADY_HALTED")
		if got := FirmwareAdvice(err, vocabulary); got != err {
			t.Errorf("got %#v, want the same error", got)
		}
	})
	t.Run("an error that is not the server's comes back as it was", func(t *testing.T) {
		err := &Error{Code: CodeInvalidInput, Message: "nope"}
		if got := FirmwareAdvice(err, vocabulary); got != error(err) {
			t.Errorf("got %#v, want the same error", got)
		}
		if FirmwareAdvice(nil, vocabulary) != nil {
			t.Error("nil must stay nil")
		}
	})
}

// NDManager's own bodies for the two FIRMWARE_SPEC_MANAGED refusals, copied from
// the control plane: the one on a track's schedule entry (enable, disable and
// remove through the generic routes) names no track, and the one on deleting a
// schedule lists the tracks whose windows run on it, in conveyor order and
// lowercase, with their number in count.
const (
	specEntryRefusal = `{"error":"This schedule entry belongs to a firmware track. Attach or detach the track's schedule through the firmware configuration instead.","code":"FIRMWARE_SPEC_MANAGED"}`

	scheduleHeldByOneTrack = `{"error":"This schedule carries the update window of firmware track fast. Deleting it would leave its devices pinned with nothing to move them. Detach the track from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast"],"count":1}`

	scheduleHeldByTwoTracks = `{"error":"This schedule carries the update windows of firmware tracks fast, regular. Deleting it would leave their devices pinned with nothing to move them. Detach the tracks from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast","regular"],"count":2}`

	scheduleHeldByAllTracks = `{"error":"This schedule carries the update windows of firmware tracks fast, regular, stable. Deleting it would leave their devices pinned with nothing to move them. Detach the tracks from this schedule in the firmware configuration first, then delete it.","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":["fast","regular","stable"],"count":3}`
)

var specManagedVocabulary = FirmwareVocabulary{
	ConfigCreate:  "CREATE",
	ConfigEnable:  "ENABLE",
	DeviceStatus:  "STATUS",
	DeviceRelease: "RELEASE",
	TrackSchedule: "SCHEDULE " + FirmwareTrackPlaceholder,
	TrackDetach:   "DETACH " + FirmwareTrackPlaceholder,
}

// Deleting a schedule a track's windows run on is refused with the tracks in the
// body, so the hint names each one in the detach command and in the move command:
// nothing is left to fill in but the schedule to move to.
func TestFirmwareAdviceNamesTheTracksAScheduleIsHeldBy(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		hint string
	}{
		{"one track", scheduleHeldByOneTrack,
			"To detach the track from its schedule: DETACH fast. To move it to another schedule: SCHEDULE fast."},
		{"two tracks", scheduleHeldByTwoTracks,
			"To detach the tracks from their schedule: DETACH fast; DETACH regular. To move them to another schedule: SCHEDULE fast; SCHEDULE regular."},
		{"all three", scheduleHeldByAllTracks,
			"To detach the tracks from their schedule: DETACH fast; DETACH regular; DETACH stable. " +
				"To move them to another schedule: SCHEDULE fast; SCHEDULE regular; SCHEDULE stable."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, 409, tc.body)
			err := newTestService(t, srv).ScheduleDelete(context.Background(), "acme", "night")
			if err == nil {
				t.Fatal("expected the refusal")
			}
			if seen.method != http.MethodDelete || seen.path != "/api/v1/organizations/acme/schedules/night" {
				t.Errorf("sent %s %s", seen.method, seen.path)
			}

			advised := FirmwareAdvice(err, specManagedVocabulary)
			var body struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal([]byte(tc.body), &body)
			// The server's words come first, as sent, and the hint ends the message.
			if want := body.Error + "\n  Hint: " + tc.hint; advised.Error() != want {
				t.Errorf("advice =\n%q\nwant\n%q", advised.Error(), want)
			}
			if strings.Contains(advised.Error(), FirmwareTrackPlaceholder) {
				t.Errorf("the tracks are named, so no template is left: %q", advised.Error())
			}
			if got := api.GetErrorCode(advised); got != "FIRMWARE_SPEC_MANAGED" || !api.IsConflictError(advised) {
				t.Errorf("code = %q, conflict = %v", got, api.IsConflictError(advised))
			}
			var apiErr *api.APIError
			if !errors.As(advised, &apiErr) || len(apiErr.BlockingResources) == 0 {
				t.Errorf("the tracks the server listed must stay on the error: %+v", apiErr)
			}
			// Advice given once is not given again by an outer layer.
			if again := FirmwareAdvice(advised, specManagedVocabulary); again.Error() != advised.Error() {
				t.Errorf("advised twice:\n%s", again.Error())
			}
		})
	}
}

// The refusal on a track's schedule entry names no track, so the hint keeps the
// template, and so does one whose list is not made of tracks: a command is only
// ever built from one of the three names.
func TestFirmwareAdviceKeepsTheTemplateWhenNoTrackIsNamed(t *testing.T) {
	const generic = "To detach the track from its schedule: DETACH <track>. To move it to another schedule: SCHEDULE <track>."
	listed := func(resources string) string {
		return `{"error":"held","code":"FIRMWARE_SPEC_MANAGED","blocking_resources":` + resources + `}`
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a track's schedule entry", specEntryRefusal},
		{"an empty list", listed(`[]`)},
		{"a list that names something else", listed(`["night"]`)},
		{"a list that mixes a track with something else", listed(`["fast","regular; rm -rf ~"]`)},
		{"a track in another letter case", listed(`["Fast"]`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := firmwareServer(t, 409, tc.body)
			_, err := newTestService(t, srv).ScheduledTaskSetEnabledByCode(context.Background(), "acme", "Fw123456", true)
			advised := FirmwareAdvice(err, specManagedVocabulary)
			if !strings.Contains(advised.Error(), generic) {
				t.Errorf("advice = %q, want the template hint", advised.Error())
			}
		})
	}
}

func TestFirmwareVocabularyCommandsNameOnlyARealTrack(t *testing.T) {
	v := FirmwareVocabulary{TrackDetach: "DETACH " + FirmwareTrackPlaceholder, TrackSchedule: "SCHEDULE " + FirmwareTrackPlaceholder + " <schedule>"}
	for _, track := range []string{"fast", "regular", "stable"} {
		if got := v.DetachCommand(track); got != "DETACH "+track {
			t.Errorf("DetachCommand(%q) = %q", track, got)
		}
		if got := v.ScheduleCommand(track); got != "SCHEDULE "+track+" <schedule>" {
			t.Errorf("ScheduleCommand(%q) = %q", track, got)
		}
	}
	for _, notATrack := range []string{"", "all", "unmanaged", "Fast", " fast", "fast; reboot", "<track>"} {
		if got := v.DetachCommand(notATrack); got != v.TrackDetach {
			t.Errorf("DetachCommand(%q) = %q, want the template %q", notATrack, got, v.TrackDetach)
		}
		if got := v.ScheduleCommand(notATrack); got != v.TrackSchedule {
			t.Errorf("ScheduleCommand(%q) = %q, want the template %q", notATrack, got, v.TrackSchedule)
		}
	}
}

// poisonJSON rewrites every string in a JSON document to carry terminal
// control sequences, the way a hostile control plane would.
func poisonJSON(t *testing.T, doc string) string {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	var walk func(interface{}) interface{}
	walk = func(in interface{}) interface{} {
		switch x := in.(type) {
		case string:
			return "\x1b[31m" + x + "\x1b]0;pwn\x07\u009b2J"
		case []interface{}:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		case map[string]interface{}:
			for k, val := range x {
				x[k] = walk(val)
			}
			return x
		default:
			return in
		}
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

func hasControl(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

func controlIn(v reflect.Value, path string, found *[]string) {
	switch v.Kind() {
	case reflect.String:
		if hasControl(v.String()) {
			*found = append(*found, path)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			controlIn(v.Elem(), path, found)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			controlIn(v.Index(i), path+"[]", found)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			controlIn(v.Field(i), path+"."+v.Type().Field(i).Name, found)
		}
	}
}

// TestFirmwareResponsesAreSanitized runs every method that returns a model
// against a response whose every string carries escape sequences, through the
// real decode path, and fails if any survives. A model that decoded outside
// api.ParseResponse (a hand-rolled json.Decoder) would fail here.
func TestFirmwareResponsesAreSanitized(t *testing.T) {
	devicesPoison := poisonJSON(t, `{"items":[{"device_name":"fw-a","device_uuid":"u","device_status":"ENABLED","agent_version":"1.24.0","track":"fast","state":"held","pinned_version":"26.7.3","held_reason":"held_custom_flavour","last_run_at":"2026-10-08T12:00:00Z","last_status":"failed","last_error":"boom","blocked":"agent_too_old","released":{"at":"2026-10-01T00:00:00Z","reason":"dead_man"}}],"total":1,"page":1,"per_page":50,"pages":1}`)
	rolloutPoison := poisonJSON(t, `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[{"track":"regular","soak_days":9,"min_success":1,"schedule":"night","scheduled":true,"devices":{"total":1},"series":[{"series":"26.7","target":"26.7.3","target_approved_at":"2026-10-01T00:00:00Z","target_source":"auto","next_candidate":{"version":"26.7.4","state":"soaking","soak_ends_at":"2026-10-20T00:00:00Z","evidence":{"reached":0,"required":1},"stalled":true,"stall_reason":"evidence_unreachable"}}]}],"failures":[{"version":"26.7.4","track":"fast","devices":["fw-b"],"count":1,"last_at":"2026-10-08T12:00:00Z"}],"halts":[{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"2026-10-08T12:00:00Z","halted_by":"a@b.c"}]}`)

	for _, tc := range []struct {
		name     string
		response string
		call     func(s *Service) (interface{}, error)
	}{
		{"config", poisonJSON(t, cfgBody), func(s *Service) (interface{}, error) {
			return s.FirmwareConfigGet(context.Background(), "acme")
		}},
		{"config delete", poisonJSON(t, deleteBody), func(s *Service) (interface{}, error) {
			return s.FirmwareConfigDelete(context.Background(), "acme")
		}},
		{"track", poisonJSON(t, trackBody), func(s *Service) (interface{}, error) {
			return s.FirmwareTrackGet(context.Background(), "acme", "regular")
		}},
		{"assignment", poisonJSON(t, assignBody), func(s *Service) (interface{}, error) {
			return s.FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "fast"})
		}},
		{"devices", devicesPoison, func(s *Service) (interface{}, error) {
			return s.FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{})
		}},
		{"history", poisonJSON(t, `{"device_name":"fw-a","items":[{"completed_at":"2026-10-08T12:00:00Z","track":"fast","action":"advance","outcome":"success","reason":"applied","target":"26.7.5","from_version":"26.7.3","to_version":"26.7.5","applied":true,"cleared_by":"a@b.c"}],"total":1,"page":1,"per_page":50,"pages":1}`),
			func(s *Service) (interface{}, error) {
				return s.FirmwareHistory(context.Background(), "acme", "fw-a", 1, 50)
			}},
		{"rollout", rolloutPoison, func(s *Service) (interface{}, error) {
			return s.FirmwareRollout(context.Background(), "acme")
		}},
		{"halts", poisonJSON(t, `{"items":[{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"2026-10-08T12:00:00Z","halted_by":"a@b.c"}],"total":1,"page":1,"per_page":50,"pages":1}`),
			func(s *Service) (interface{}, error) {
				return s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{})
			}},
		{"halt", poisonJSON(t, haltBody), func(s *Service) (interface{}, error) {
			return s.FirmwareHalt(context.Background(), "acme", "", "26.7.5", "r", 0)
		}},
		{"promotion", poisonJSON(t, promoBody), func(s *Service) (interface{}, error) {
			return s.FirmwarePromote(context.Background(), "acme", "", "26.7.5", "fast", 0)
		}},
		{"cleared", poisonJSON(t, clearBody), func(s *Service) (interface{}, error) {
			return s.FirmwareFailuresClear(context.Background(), "acme", "", "26.7.5", "", 0)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !hasControl(tc.response) && !strings.Contains(tc.response, `\u001b`) {
				t.Fatal("the fixture carries no escape sequence")
			}
			srv, _ := firmwareServer(t, http.StatusOK, tc.response)
			out, err := tc.call(newTestService(t, srv))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var found []string
			controlIn(reflect.ValueOf(out), "result", &found)
			for _, path := range found {
				t.Errorf("a control sequence survived the decode at %s", path)
			}
		})
	}

	// The server's refusal is sanitized too (api.ParseError), including the
	// devices it lists.
	srv, _ := firmwareServer(t, http.StatusConflict, poisonJSON(t,
		`{"error":"blocked","code":"HAS_DEPENDENCIES","blocking_resources":["fw-a"],"count":1}`))
	_, err := newTestService(t, srv).FirmwareConfigDelete(context.Background(), "acme")
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if hasControl(apiErr.Message) || hasControl(strings.Join(apiErr.BlockingResources, "")) || hasControl(err.Error()) {
		t.Errorf("a control sequence survived in the refusal: %q %q", err.Error(), apiErr.BlockingResources)
	}
}

func TestRunRefusesTheServerOnlyFirmwareTaskType(t *testing.T) {
	srv, seen := firmwareServer(t, http.StatusOK, `{}`)
	_, err := newTestService(t, srv).Run(context.Background(), "acme", RunOpts{
		Type: models.TaskTypeFirmwareTrack, Devices: []string{"fw-a"},
	})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
		t.Fatalf("err = %#v, want invalid input", err)
	}
	if seen.hits != 0 {
		t.Errorf("a client must not be able to ask for FIRMWARE_TRACK; %d request(s) sent", seen.hits)
	}
}

// ── major upgrades ───────────────────────────────────────────────────────────

const (
	trackOnBody  = `{"track":"regular","soak_days":9,"min_success":1,"scheduled":false,"auto_apply_major":true,"devices":{"total":0}}`
	trackOffBody = `{"track":"regular","soak_days":9,"min_success":1,"scheduled":false,"auto_apply_major":false,"cancelled_tasks":2,"devices":{"total":0}}`

	// What an NDManager that predates major upgrades answers (400) to a track
	// update whose only field it ignored (e8b38d2: update_track, with the
	// router's status for MISSING_FIELD), and what 1.97.0 answers to the
	// boot-environment setting sent for devices that stand in no track.
	emptyTrackUpdateRefusal = `{"error":"Provide at least one field to change","code":"MISSING_FIELD"}`
	unmanagedRefusal        = `{"error":"The boot-environment setting belongs to a device's track row, which these devices do not have; add them to a track first, or send the track too: fw-a, fw-b","code":"FIRMWARE_DEVICE_UNMANAGED","blocking_resources":["fw-a","fw-b"],"count":2}`

	seriesHaltBody  = `{"halt":{"series":"26.7","kind":"major","reason":"r","halted_at":"2026-10-08T12:00:00Z","halted_by":"a@b.c"},"cancelled_tasks":1}`
	seriesPromoBody = `{"series":"26.7","results":[{"track":"fast","result":"approved","target":"26.7"}]}`
	seriesClearBody = `{"series":"26.7","cleared":1}`
)

// The switch is sent on its own, with nothing beside it, and the answer is held
// to it: an NDManager that predates major upgrades ignores the key.
func TestFirmwareTrackSwitchIsSentAloneAndTheEchoIsChecked(t *testing.T) {
	on, off := true, false

	t.Run("on", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, trackOnBody)
		track, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "Regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
		if err != nil {
			t.Fatal(err)
		}
		if seen.method != "PUT" || seen.path != "/api/v1/organizations/acme/firmware/tracks/regular" ||
			!reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": true}) {
			t.Errorf("sent %s %s %v", seen.method, seen.path, seen.body)
		}
		if track.AutoApplyMajor == nil || !*track.AutoApplyMajor {
			t.Errorf("track = %+v", track)
		}
	})
	t.Run("off says how many queued steps it cancelled", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, trackOffBody)
		track, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &off})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": false}) {
			t.Errorf("body = %v; false is a value, and is sent", seen.body)
		}
		if track.CancelledTasks == nil || *track.CancelledTasks != 2 {
			t.Errorf("cancelled = %v", track.CancelledTasks)
		}
	})
	t.Run("the 400 an NDManager that predates it gives to the switch alone", func(t *testing.T) {
		// It ignores the key, sees an update that names no field, and refuses it.
		srv, seen := firmwareServer(t, http.StatusBadRequest, emptyTrackUpdateRefusal)
		_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld ||
			!strings.HasPrefix(err.Error(), "this NDManager predates major upgrades:") || !strings.Contains(err.Error(), "the switch was not changed") {
			t.Fatalf("err = %#v", err)
		}
		if !reflect.DeepEqual(seen.body, map[string]interface{}{"auto_apply_major": true}) || seen.hits != 1 {
			t.Errorf("sent %v, %d request(s); the switch goes alone", seen.body, seen.hits)
		}
		// The refusal stays underneath; what is shown with the error is the diagnosis.
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "MISSING_FIELD" {
			t.Errorf("the 400 behind it was lost: %v", err)
		}
		if got := ErrorCode(err); got != CodeServerTooOld {
			t.Errorf("ErrorCode = %q, want %s and not the MISSING_FIELD that would send the user looking for a field they gave", got, CodeServerTooOld)
		}
	})
	t.Run("another refusal is not taken for it", func(t *testing.T) {
		for name, tc := range map[string]struct {
			status int
			body   string
		}{
			"another 400":                     {400, `{"error":"The fast track takes no evidence requirement","code":"INVALID_PARAM"}`},
			"MISSING_FIELD at another status": {422, `{"error":"Provide at least one field to change","code":"MISSING_FIELD"}`},
			"the plan":                        {403, `{"error":"Firmware tracks are not available on your current plan.","code":"FEATURE_NOT_AVAILABLE"}`},
			"no configuration":                {404, `{"error":"No firmware configuration","code":"FIRMWARE_NOT_CONFIGURED"}`},
		} {
			srv, _ := firmwareServer(t, tc.status, tc.body)
			_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
			var svcErr *Error
			if err == nil || (errors.As(err, &svcErr) && svcErr.Code == CodeServerTooOld) {
				t.Errorf("%s: err = %#v, must not be read as an old NDManager", name, err)
			}
		}
	})
	t.Run("a MISSING_FIELD for a request that did not carry the switch is not about it", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusBadRequest, emptyTrackUpdateRefusal)
		five := 5
		_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{SoakDays: &five})
		var svcErr *Error
		if errors.As(err, &svcErr) && svcErr.Code == CodeServerTooOld {
			t.Errorf("err = %v", err)
		}
		if api.GetErrorCode(err) != "MISSING_FIELD" || ErrorCode(err) != "MISSING_FIELD" {
			t.Errorf("the server's own code should show: %q / %q", api.GetErrorCode(err), ErrorCode(err))
		}
	})
	t.Run("an answer that does not carry the switch is an NDManager that predates it, the second guard", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, trackBody) // the answer of one that ignored the key
		_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld ||
			!strings.Contains(err.Error(), "this NDManager predates major upgrades") || !strings.Contains(err.Error(), "the switch was not changed") {
			t.Fatalf("err = %#v", err)
		}
		if ErrorCode(err) != CodeServerTooOld {
			t.Errorf("ErrorCode = %q", ErrorCode(err))
		}
	})
	t.Run("an answer with the other value is a write that did not happen", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, trackOnBody)
		_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &off})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeAPIError ||
			!strings.Contains(err.Error(), "auto_apply_major=true to a request for false") {
			t.Fatalf("err = %#v", err)
		}
	})
	t.Run("the other settings are not held to an echo they do not have", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, trackBody)
		five := 5
		if _, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{SoakDays: &five}); err != nil {
			t.Errorf("a soak change against an older NDManager must keep working: %v", err)
		}
	})
	t.Run("the refusal of the server is kept", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusForbidden, `{"error":"Firmware tracks are not available on your current plan.","code":"FEATURE_NOT_AVAILABLE"}`)
		_, err := newTestService(t, srv).FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &on})
		if api.GetErrorCode(err) != "FEATURE_NOT_AVAILABLE" {
			t.Errorf("code = %q", api.GetErrorCode(err))
		}
	})
}

// The boot-environment setting is a field of PUT /firmware/devices. Alone, it
// carries no track; beside one, both go in the request. The answer must echo it.
func TestFirmwareBootEnvironmentIsSentAndTheEchoIsChecked(t *testing.T) {
	on, off := true, false
	devices := []string{"fw-a", "fw-b"}
	echoed := func(setting string) string {
		return `{"organization_name":"acme","track":null,"results":[` +
			`{"device_name":"fw-a","result":"updated","track":"fast","state":"managed","boot_environment":` + setting + `,"notes":[]},` +
			`{"device_name":"fw-b","result":"unchanged","track":"fast","state":"managed","boot_environment":` + setting + `,"notes":[]}],"cancelled_tasks":1}`
	}

	t.Run("alone", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, echoed("false"))
		out, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", devices, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		if err != nil {
			t.Fatal(err)
		}
		if seen.method != "PUT" || seen.path != "/api/v1/organizations/acme/firmware/devices" ||
			!reflect.DeepEqual(seen.body, map[string]interface{}{"devices": []interface{}{"fw-a", "fw-b"}, "boot_environment": false}) {
			t.Errorf("sent %s %s %v; the setting goes alone, with no track", seen.method, seen.path, seen.body)
		}
		if out.Track != "" || out.CancelledTasks != 1 || out.Results[0].Result != "updated" {
			t.Errorf("out = %+v", out)
		}
	})
	t.Run("beside a track", func(t *testing.T) {
		both := `{"organization_name":"acme","track":"fast","results":[{"device_name":"fw-a","result":"added","track":"fast","state":"enrolling","boot_environment":true,"notes":[]}],"cancelled_tasks":0}`
		srv, seen := firmwareServer(t, http.StatusOK, both)
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "Fast", BootEnvironment: &on})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(seen.body, map[string]interface{}{"devices": []interface{}{"fw-a"}, "track": "fast", "boot_environment": true}) {
			t.Errorf("body = %v", seen.body)
		}
	})
	t.Run("the track alone is sent without the setting, and no echo is asked of it", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, assignBody)
		if _, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "fast"}); err != nil {
			t.Fatal(err)
		}
		if _, present := seen.body["boot_environment"]; present {
			t.Errorf("body = %v; the setting was not asked for", seen.body)
		}
	})
	t.Run("a result without the setting is an NDManager that predates it", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, assignBody)
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld ||
			!strings.Contains(err.Error(), "this NDManager predates the boot-environment setting") || !strings.Contains(err.Error(), "fw-a") {
			t.Fatalf("err = %#v", err)
		}
		if strings.Contains(err.Error(), "itself was made") {
			t.Errorf("no move was asked for, so none is claimed: %q", err.Error())
		}
	})
	t.Run("beside a track it says the move itself was made", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, assignBody)
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{Track: "fast", BootEnvironment: &off})
		if err == nil || !strings.Contains(err.Error(), "predates the boot-environment setting") || !strings.Contains(err.Error(), "the move to fast itself was made") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a different value is a write that did not happen", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, echoed("true"))
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", devices, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeAPIError ||
			!strings.Contains(err.Error(), "different boot_environment than the requested false for fw-a, fw-b") {
			t.Fatalf("err = %#v", err)
		}
	})
	t.Run("a refusal of the setting sent alone, the validator of an NDManager that requires a track", func(t *testing.T) {
		srv, _ := firmwareServer(t, 422, `{"detail":[{"loc":["body","track"],"msg":"Field required","type":"missing"}]}`)
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", devices, FirmwareDeviceAssignOpts{BootEnvironment: &on})
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeServerTooOld ||
			!strings.Contains(err.Error(), "this NDManager predates the boot-environment setting") {
			t.Fatalf("err = %#v", err)
		}
		// What the server said is still underneath.
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 422 {
			t.Errorf("the 422 behind it was lost: %v", err)
		}
	})
	t.Run("another refusal is not taken for it", func(t *testing.T) {
		for name, tc := range map[string]struct {
			status int
			body   string
		}{
			"a validation error that is not about the track":               {422, `{"detail":[{"loc":["body","devices"],"msg":"List should have at most 500 items","type":"too_long"}]}`},
			"a validation error about the track that is not a missing one": {422, `{"detail":[{"loc":["body","track"],"msg":"Input should be 'fast', 'regular' or 'stable'","type":"enum"}]}`},
			"an invalid parameter":                                         {422, `{"error":"boot_environment is not valid here","code":"INVALID_PARAM"}`},
			"an unknown device":                                            {404, `{"error":"Device 'fw-z' not found","code":"DEVICE_NOT_FOUND"}`},
			"the plan":                                                     {403, `{"error":"Firmware tracks are not available on your current plan.","code":"FEATURE_NOT_AVAILABLE"}`},
		} {
			srv, _ := firmwareServer(t, tc.status, tc.body)
			_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", devices, FirmwareDeviceAssignOpts{BootEnvironment: &on})
			var svcErr *Error
			if err == nil || (errors.As(err, &svcErr) && svcErr.Code == CodeServerTooOld) {
				t.Errorf("%s: err = %#v, must not be read as an old NDManager", name, err)
			}
		}
	})
	t.Run("a track sent beside the setting keeps the server's 422", func(t *testing.T) {
		srv, _ := firmwareServer(t, 422, `{"detail":[{"loc":["body","track"],"msg":"Value error","type":"value_error"}]}`)
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", devices, FirmwareDeviceAssignOpts{Track: "fast", BootEnvironment: &on})
		var svcErr *Error
		if errors.As(err, &svcErr) && svcErr.Code == CodeServerTooOld {
			t.Errorf("a request that carried the track is not the one an old server refuses: %v", err)
		}
	})
}

// A major upgrade is addressed by series: its own body field, its own path.
// Never a kind flag beside a version, which an NDManager that predates series
// upgrades would ignore, and act on the GA release of the same name.
func TestFirmwareSeriesOperationsAreAddressedBySeries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		call     func(s *Service) (interface{}, error)
		method   string
		path     string
		query    map[string]string
		body     map[string]interface{}
	}{
		{"halt", seriesHaltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHalt(context.Background(), "acme", " Major ", " 26.7 ", " payload changed ", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/halts", nil,
			map[string]interface{}{"series": "26.7", "reason": "payload changed"}},
		{"unhalt has a path of its own", seriesHaltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareUnhalt(context.Background(), "acme", "major", "26.7", 0)
			},
			"DELETE", "/api/v1/organizations/acme/firmware/halts/series/26.7", nil, nil},
		{"unhalt escapes the series", seriesHaltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareUnhalt(context.Background(), "acme", "major", "26/7", 0)
			},
			"DELETE", "/api/v1/organizations/acme/firmware/halts/series/26%2F7", nil, nil},
		{"promote", seriesPromoBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwarePromote(context.Background(), "acme", "major", "26.7", "FAST", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/promote", nil,
			map[string]interface{}{"series": "26.7", "track": "fast"}},
		{"clear in one track", seriesClearBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareFailuresClear(context.Background(), "acme", "major", "26.7", "stable", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/failures/clear", nil,
			map[string]interface{}{"series": "26.7", "track": "stable"}},
		{"clear in every track sends no track", seriesClearBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareFailuresClear(context.Background(), "acme", "major", "26.7", "", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/failures/clear", nil,
			map[string]interface{}{"series": "26.7"}},
		{"a release keeps its version, whatever else is true of its name", haltBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHalt(context.Background(), "acme", "minor", "26.7", "r", 0)
			},
			"POST", "/api/v1/organizations/acme/firmware/halts", nil,
			map[string]interface{}{"version": "26.7", "reason": "r"}},
		{"halts of major upgrades", haltsBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{Kind: "Major"})
			},
			"GET", "/api/v1/organizations/acme/firmware/halts", map[string]string{"kind": "major", "active": "true"}, nil},
		{"halts of both kinds", haltsBody,
			func(s *Service) (interface{}, error) {
				return s.FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{Kind: "all", IncludeLifted: true})
			},
			"GET", "/api/v1/organizations/acme/firmware/halts", map[string]string{"kind": "all", "active": "false"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, http.StatusOK, tc.response)
			out, err := tc.call(newTestService(t, srv))
			if err != nil || out == nil {
				t.Fatalf("out = %v, err = %v", out, err)
			}
			if seen.method != tc.method || seen.path != tc.path {
				t.Errorf("sent %s %s, want %s %s", seen.method, seen.path, tc.method, tc.path)
			}
			for k, want := range tc.query {
				if got := seen.query.Get(k); got != want {
					t.Errorf("query %s = %q, want %q", k, got, want)
				}
			}
			if !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("body = %v, want %v", seen.body, tc.body)
			}
		})
	}

	t.Run("a default list leaves the kind to the server", func(t *testing.T) {
		srv, seen := firmwareServer(t, http.StatusOK, haltsBody)
		if _, err := newTestService(t, srv).FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{}); err != nil {
			t.Fatal(err)
		}
		if _, present := seen.query["kind"]; present {
			t.Errorf("query = %v; an old NDManager ignores kind, and a new one defaults to releases", seen.query)
		}
	})
	t.Run("the answers keep their series", func(t *testing.T) {
		srv, _ := firmwareServer(t, http.StatusOK, seriesHaltBody)
		halt, err := newTestService(t, srv).FirmwareHalt(context.Background(), "acme", "major", "26.7", "r", 0)
		if err != nil || halt.Halt.Series != "26.7" || halt.Halt.Version != "" || !halt.Halt.IsMajor() || halt.CancelledTasks != 1 {
			t.Errorf("halt = %+v, %v", halt, err)
		}
		srv, _ = firmwareServer(t, http.StatusOK, seriesPromoBody)
		promo, err := newTestService(t, srv).FirmwarePromote(context.Background(), "acme", "major", "26.7", "fast", 0)
		if err != nil || promo.Series != "26.7" || promo.Version != "" || !promo.IsMajor() {
			t.Errorf("promotion = %+v, %v", promo, err)
		}
		srv, _ = firmwareServer(t, http.StatusOK, seriesClearBody)
		cleared, err := newTestService(t, srv).FirmwareFailuresClear(context.Background(), "acme", "major", "26.7", "fast", 0)
		if err != nil || cleared.Series != "26.7" || cleared.Track != "fast" || cleared.Cleared != 1 || !cleared.IsMajor() {
			t.Errorf("cleared = %+v, %v", cleared, err)
		}
	})
}

func TestCheckFirmwareTarget(t *testing.T) {
	for _, tc := range []struct {
		kind, name         string
		wantKind, wantName string
		wantErr            string
	}{
		{"", " 26.7.5 ", "minor", "26.7.5", ""},
		{"MINOR", "26.7", "minor", "26.7", ""},
		{" Major ", " 26.7 ", "major", "26.7", ""},
		{"major", "", "", "", "series is required"},
		{"minor", " ", "", "", "version is required"},
		{"all", "26.7", "", "", `unknown kind "all"`},
	} {
		kind, name, err := CheckFirmwareTarget(tc.kind, tc.name)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("CheckFirmwareTarget(%q, %q) err = %v, want %q", tc.kind, tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || kind != tc.wantKind || name != tc.wantName {
			t.Errorf("CheckFirmwareTarget(%q, %q) = %q, %q, %v", tc.kind, tc.name, kind, name, err)
		}
	}
}

func TestCheckFirmwareDeviceAssign(t *testing.T) {
	off := false
	names, track, err := CheckFirmwareDeviceAssign([]string{" fw-a ", "fw-a", "", "fw-b"}, FirmwareDeviceAssignOpts{Track: " Regular ", BootEnvironment: &off})
	if err != nil || !reflect.DeepEqual(names, []string{"fw-a", "fw-b"}) || track != "regular" {
		t.Errorf("= %v %q %v", names, track, err)
	}
	names, track, err = CheckFirmwareDeviceAssign([]string{"fw-a"}, FirmwareDeviceAssignOpts{BootEnvironment: &off})
	if err != nil || track != "" || len(names) != 1 {
		t.Errorf("the setting alone = %v %q %v", names, track, err)
	}
}

// The two codes of the major upgrade carry the way forward, in the front-end's
// words, and the devices the refusal lists.
func TestFirmwareAdviceForTheSeriesUpgradeRefusals(t *testing.T) {
	vocabulary := FirmwareVocabulary{Rollout: "ROLLOUT", DeviceAdd: "ADD " + FirmwareTrackPlaceholder, DeviceAddWithSetting: "ADD " + FirmwareTrackPlaceholder + " WITH SETTING"}
	refusal := func(t *testing.T, body string) error {
		t.Helper()
		srv, _ := firmwareServer(t, 409, body)
		_, err := newTestService(t, srv).FirmwarePromote(context.Background(), "acme", "major", "26.7", "fast", 0)
		return err
	}

	t.Run("the upgrade path is closed", func(t *testing.T) {
		err := refusal(t, `{"error":"The upgrade to 26.7 is not open yet","code":"FIRMWARE_UPGRADE_NOT_OPEN"}`)
		advised := FirmwareAdvice(err, vocabulary)
		if want := "The upgrade to 26.7 is not open yet\n  Hint: A major upgrade cannot be approved while OPNsense's upgrade path is closed: " +
			"the control plane has not seen all of its files on the mirror lately. See where it stands with ROLLOUT."; advised.Error() != want {
			t.Errorf("advice =\n%q\nwant\n%q", advised.Error(), want)
		}
		if api.GetErrorCode(advised) != "FIRMWARE_UPGRADE_NOT_OPEN" || !api.IsConflictError(advised) {
			t.Errorf("code = %q", api.GetErrorCode(advised))
		}
		if again := FirmwareAdvice(advised, vocabulary); again.Error() != advised.Error() {
			t.Errorf("advised twice:\n%s", again.Error())
		}
	})
	t.Run("devices in no track: the commands, and the names only in the server's words", func(t *testing.T) {
		srv, _ := firmwareServer(t, 409, unmanagedRefusal)
		off := false
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a", "fw-b"}, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		advised := FirmwareAdvice(err, vocabulary)
		want := "The boot-environment setting belongs to a device's track row, which these devices do not have; add them to a track first, or send the track too: fw-a, fw-b" +
			"\n  Hint: To add the devices to a track: ADD <track>. To add them with the setting in one go: ADD <track> WITH SETTING."
		if advised.Error() != want {
			t.Errorf("advice =\n%q\nwant\n%q", advised.Error(), want)
		}
		for _, name := range []string{"fw-a", "fw-b"} {
			if n := strings.Count(advised.Error(), name); n != 1 {
				t.Errorf("%s appears %d times in %q, want once (the server names it already)", name, n, advised.Error())
			}
		}
		if api.GetErrorCode(advised) != "FIRMWARE_DEVICE_UNMANAGED" {
			t.Errorf("code = %q", api.GetErrorCode(advised))
		}
		if again := FirmwareAdvice(advised, vocabulary); again.Error() != advised.Error() {
			t.Errorf("advised twice:\n%s", again.Error())
		}
	})
	t.Run("a list longer than what was sent says how many there are", func(t *testing.T) {
		body := `{"error":"no track: fw-a","code":"FIRMWARE_DEVICE_UNMANAGED","blocking_resources":["fw-a"],"count":7}`
		srv, _ := firmwareServer(t, 409, body)
		off := false
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		text := FirmwareAdvice(err, vocabulary).Error()
		if !strings.Contains(text, "\n  Hint: 7 devices are in no track; the refusal names 1. To add the devices to a track: ADD <track>.") {
			t.Errorf("advice = %q", text)
		}
		if n := strings.Count(text, "fw-a"); n != 1 {
			t.Errorf("fw-a appears %d times in %q, want once", n, text)
		}
	})
	t.Run("without a list the hint is the commands alone", func(t *testing.T) {
		srv, _ := firmwareServer(t, 409, `{"error":"no track","code":"FIRMWARE_DEVICE_UNMANAGED"}`)
		off := false
		_, err := newTestService(t, srv).FirmwareDeviceAssign(context.Background(), "acme", []string{"fw-a"}, FirmwareDeviceAssignOpts{BootEnvironment: &off})
		want := "no track\n  Hint: To add the devices to a track: ADD <track>. To add them with the setting in one go: ADD <track> WITH SETTING."
		if text := FirmwareAdvice(err, vocabulary).Error(); text != want {
			t.Errorf("advice =\n%q\nwant\n%q", text, want)
		}
	})
}

// Every model the major upgrade added decodes through the sanitizing path.
func TestFirmwareMajorResponsesAreSanitized(t *testing.T) {
	devices := poisonJSON(t, `{"items":[{"device_name":"fw-a","device_uuid":"u","device_status":"ENABLED","track":"fast","state":"managed","series":"26.7","major":{"eligible":false,"blocked_by":"held_ha_member"},"boot_environment":{"setting":true,"last":"made"},"notes":["third_party_package"]}],"total":1,"page":1,"per_page":50,"pages":1}`)
	rollout := poisonJSON(t, `{"organization_name":"acme","status":"ENABLED","plan_enabled":true,"tracks":[],"failures":[],"halts":[],"majors":[{"track":"fast","series":"26.7","source_series":"26.1","state":"approved","soak_ends_at":"2026-10-20T00:00:00Z","evidence":{"reached":1,"required":1},"stalled":false,"stall_reason":"evidence_unreachable","auto_apply_major":true,"devices_eligible":1,"devices_on_series":1,"devices_hopped":1,"devices_awaiting_minor_evidence":0,"offer_not_seen":0,"blocked_reason":"path_closed","upgrade_open_at":"2026-09-20T00:00:00Z","approved_at":"2026-10-01T00:00:00Z","approval_source":"auto","source_series_end_of_life":true,"notice":"payload_changed"}],"major_failures":[{"kind":"major","series":"26.7","track":"fast","devices":["fw-b"],"count":1,"last_at":"2026-10-08T12:00:00Z"}],"agent_repo":{"configured":true,"channel":"https://repo.example/dev","trees":[{"abi":"FreeBSD:15:amd64","ok":false,"checked_at":"2026-10-12T12:00:00Z"}]}}`)
	for _, tc := range []struct {
		name     string
		response string
		call     func(s *Service) (interface{}, error)
	}{
		{"devices", devices, func(s *Service) (interface{}, error) {
			return s.FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{})
		}},
		{"rollout", rollout, func(s *Service) (interface{}, error) { return s.FirmwareRollout(context.Background(), "acme") }},
		{"series halt", poisonJSON(t, seriesHaltBody), func(s *Service) (interface{}, error) {
			return s.FirmwareHalt(context.Background(), "acme", "major", "26.7", "r", 0)
		}},
		{"series promotion", poisonJSON(t, seriesPromoBody), func(s *Service) (interface{}, error) {
			return s.FirmwarePromote(context.Background(), "acme", "major", "26.7", "fast", 0)
		}},
		{"series clear", poisonJSON(t, seriesClearBody), func(s *Service) (interface{}, error) {
			return s.FirmwareFailuresClear(context.Background(), "acme", "major", "26.7", "", 0)
		}},
		{"track", poisonJSON(t, trackOffBody), func(s *Service) (interface{}, error) {
			off := false
			return s.FirmwareTrackUpdate(context.Background(), "acme", "regular", FirmwareTrackUpdateOpts{AutoApplyMajor: &off})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !hasControl(tc.response) && !strings.Contains(tc.response, `\u001b`) {
				t.Fatal("the fixture carries no escape sequence")
			}
			srv, _ := firmwareServer(t, http.StatusOK, tc.response)
			out, err := tc.call(newTestService(t, srv))
			// The poisoned echo of the switch is not the value asked for: the
			// service refuses it, and what matters here is that nothing it prints
			// carries a control byte.
			if err != nil {
				if hasControl(err.Error()) {
					t.Errorf("a control sequence reached the error: %q", err.Error())
				}
				return
			}
			var found []string
			controlIn(reflect.ValueOf(out), "result", &found)
			for _, path := range found {
				t.Errorf("a control sequence survived the decode at %s", path)
			}
		})
	}
}

// A halt list is held to the kind asked for on the client too: an NDManager that
// predates major upgrades ignores the kind parameter and answers its release
// halts, which must not show for a request for major upgrades (nor in the
// completion of an unhalt of one).
func TestFirmwareHaltListIsFilteredByTheKindAskedFor(t *testing.T) {
	const (
		release   = `{"version":"26.7.5","kind":"minor","reason":"r","halted_at":"t","halted_by":"a"}`
		noKind    = `{"version":"26.7.4","reason":"r","halted_at":"t","halted_by":"a"}`
		majorHalt = `{"series":"26.7","kind":"major","reason":"r","halted_at":"t","halted_by":"a"}`
	)
	page := func(n string, rows ...string) string {
		return `{"items":[` + strings.Join(rows, ",") + `],"total":120,"page":` + n + `,"per_page":50,"pages":3}`
	}
	list := func(rows ...string) string { return page("1", rows...) }
	for _, tc := range []struct {
		name      string
		kind      string
		response  string
		want      []string // names kept
		wantTotal int
		wantPage  int
		wantPages int
	}{
		{"an older NDManager's release halts never show for major", "major", list(release, noKind), nil, 0, 1, 1},
		{"a current NDManager's major halts stay, and so does its paging", "major", list(majorHalt), []string{"26.7"}, 120, 1, 3},
		{"a current NDManager's second page stays the second", "major", page("2", majorHalt), []string{"26.7"}, 120, 2, 3},
		{"a server that ignored the kind and sent both: only the major one", "major", list(release, majorHalt), []string{"26.7"}, 1, 1, 1},
		{"the second page of a server that ignored the kind is not a page 2 of 1", "major", page("2", release, noKind), nil, 0, 1, 1},
		{"a row with no kind is a release's", "minor", list(release, noKind), []string{"26.7.5", "26.7.4"}, 120, 1, 3},
		{"the major row of a server that ignored minor is dropped", "minor", list(release, majorHalt, noKind), []string{"26.7.5", "26.7.4"}, 2, 1, 1},
		{"all keeps every row and its paging", "all", list(release, majorHalt, noKind), []string{"26.7.5", "26.7", "26.7.4"}, 120, 1, 3},
		{"no kind asked leaves the server's list as it is", "", list(release, majorHalt), []string{"26.7.5", "26.7"}, 120, 1, 3},
		{"the kind is read in any letter case", " Major ", list(release, majorHalt), []string{"26.7"}, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, http.StatusOK, tc.response)
			out, err := newTestService(t, srv).FirmwareHaltList(context.Background(), "acme", FirmwareHaltListOpts{Kind: tc.kind})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range out.Items {
				got = append(got, h.Name())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("kept %v, want %v", got, tc.want)
			}
			if out.Total != tc.wantTotal || out.Page != tc.wantPage || out.Pages != tc.wantPages {
				t.Errorf("total %d, page %d of %d; want %d, %d of %d", out.Total, out.Page, out.Pages, tc.wantTotal, tc.wantPage, tc.wantPages)
			}
			if tc.kind != "" && seen.query.Get("kind") != strings.ToLower(strings.TrimSpace(tc.kind)) {
				t.Errorf("kind sent = %q", seen.query.Get("kind"))
			}
		})
	}
}

func TestErrorCodeShowsTheServersCodeExceptForAVersionSkew(t *testing.T) {
	refusal := &api.APIError{StatusCode: 400, Code: "MISSING_FIELD", Message: "Provide at least one field to change"}
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"nil":                                    {nil, ""},
		"NDManager's code":                       {refusal, "MISSING_FIELD"},
		"through the service's":                  {wrapAPI("%v", refusal), "MISSING_FIELD"},
		"a service error of its own":             {&Error{Code: CodeInvalidInput, Message: "nope"}, ""},
		"a version skew over the refusal":        {&Error{Code: CodeServerTooOld, Err: refusal, Message: "this NDManager predates it"}, CodeServerTooOld},
		"a version skew with nothing underneath": {&Error{Code: CodeServerTooOld, Message: "this NDManager predates it"}, CodeServerTooOld},
		"a version skew inside advice":           {FirmwareAdvice(&Error{Code: CodeServerTooOld, Err: refusal, Message: "m"}, FirmwareVocabulary{}), CodeServerTooOld},
	} {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("%s: ErrorCode = %q, want %q", name, got, tc.want)
		}
	}
}

func TestFirmwareRevisionIsSentWhenNamed(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		body   string
		call   func(*Service) (interface{}, error)
		method string
		path   string
		query  map[string]string
		want   map[string]interface{}
	}{
		{"halt", haltBody, func(s *Service) (interface{}, error) { return s.FirmwareHalt(ctx, "acme", "", "26.1.11", "r", 2) },
			"POST", "/api/v1/organizations/acme/firmware/halts", nil,
			map[string]interface{}{"version": "26.1.11", "reason": "r", "revision": float64(2)}},
		{"unhalt", haltBody, func(s *Service) (interface{}, error) { return s.FirmwareUnhalt(ctx, "acme", "", "26.1.11", 2) },
			"DELETE", "/api/v1/organizations/acme/firmware/halts/26.1.11", map[string]string{"revision": "2"}, nil},
		{"promote", promotionBodyForRevision, func(s *Service) (interface{}, error) {
			return s.FirmwarePromote(ctx, "acme", "", "26.1.11", "all", 2)
		}, "POST", "/api/v1/organizations/acme/firmware/promote", nil,
			map[string]interface{}{"version": "26.1.11", "track": "all", "revision": float64(2)}},
		{"clear", clearBody, func(s *Service) (interface{}, error) {
			return s.FirmwareFailuresClear(ctx, "acme", "", "26.1.11", "", 2)
		},
			"POST", "/api/v1/organizations/acme/firmware/failures/clear", nil,
			map[string]interface{}{"version": "26.1.11", "revision": float64(2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := firmwareServer(t, http.StatusOK, tc.body)
			if _, err := tc.call(newTestService(t, srv)); err != nil {
				t.Fatal(err)
			}
			if seen.method != tc.method || seen.path != tc.path {
				t.Errorf("request = %s %s, want %s %s", seen.method, seen.path, tc.method, tc.path)
			}
			for k, v := range tc.query {
				if got := seen.query.Get(k); got != v {
					t.Errorf("query %s = %q, want %q", k, got, v)
				}
			}
			if !reflect.DeepEqual(seen.body, tc.want) {
				t.Errorf("body = %v, want %v", seen.body, tc.want)
			}
		})
	}
}

func TestFirmwareRevisionRefusedBeforeSending(t *testing.T) {
	ctx := context.Background()
	srv, seen := firmwareServer(t, http.StatusOK, haltBody)
	s := newTestService(t, srv)
	if _, err := s.FirmwareHalt(ctx, "acme", "major", "26.7", "r", 2); err == nil {
		t.Error("a revision beside a major upgrade must be refused")
	}
	if _, err := s.FirmwarePromote(ctx, "acme", "", "26.1.11", "all", -1); err == nil {
		t.Error("a negative revision must be refused")
	}
	if _, err := s.FirmwareUnhalt(ctx, "acme", "major", "26.7", 2); err == nil {
		t.Error("a revision beside the unhalt of a major upgrade must be refused")
	}
	if _, err := s.FirmwareFailuresClear(ctx, "acme", "major", "26.7", "", 2); err == nil {
		t.Error("a revision beside the clear of a major upgrade must be refused")
	}
	if seen.hits != 0 {
		t.Errorf("%d request(s) reached the server", seen.hits)
	}
}

func TestFirmwareClearedCarriesTheRevision(t *testing.T) {
	srv, _ := firmwareServer(t, http.StatusOK, clearBody)
	cleared, err := newTestService(t, srv).FirmwareFailuresClear(context.Background(), "acme", "", "26.1.11", "", 3)
	if err != nil || cleared.Revision == nil || *cleared.Revision != 3 {
		t.Errorf("clear = %+v, %v; want the revision the clear was limited to", cleared, err)
	}
}

const promotionBodyForRevision = `{"version":"26.1.11","revision":2,"results":[{"track":"fast","result":"approved","target":"26.1.11","target_revision":2}]}`

func TestFirmwareRevisionFieldsDecode(t *testing.T) {
	body := `{"items":[{"device_name":"fw-a","device_uuid":"u","device_status":"ENABLED","track":"fast","state":"enrolled","pinned_version":"26.1.10","blocked":"agent_upgrade_required","revision":2,"pending":{"count":17,"kind":"hotfix","checked_at":"2026-10-23T18:14:08Z"},"hotfix_warning":{"release":"26.1.11","revision":2,"detected_at":"2026-10-23T10:33:00Z","approved_in_track":false}}],"total":1,"page":1,"per_page":50,"pages":1}`
	srv, _ := firmwareServer(t, http.StatusOK, body)
	list, err := newTestService(t, srv).FirmwareDeviceList(context.Background(), "acme", FirmwareDeviceListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	d := list.Items[0]
	if d.Blocked != models.FirmwareBlockedAgentUpgradeRequired || d.Revision == nil || *d.Revision != 2 ||
		d.Pending == nil || d.Pending.Count != 17 || d.Pending.Kind != models.FirmwarePendingHotfix ||
		d.HotfixWarning == nil || d.HotfixWarning.Release != "26.1.11" || d.HotfixWarning.ApprovedInTrack {
		t.Errorf("device = %+v", d)
	}
}
