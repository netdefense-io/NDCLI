package cli

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestFirmwareBoolFlagsTakeTheirValue: --boot-environment and
// --auto-apply-major were Bool flags. A bare one read as true and left the
// next word as an argument: "device set fw-a --boot-environment false" sent
// devices [fw-a false] with boot_environment true, and a bare
// --auto-apply-major turned major upgrades on, without asking under --force.
func TestFirmwareBoolFlagsTakeTheirValue(t *testing.T) {
	base := "/api/v1/organizations/acme/firmware"

	for _, tc := range []struct {
		name     string
		path     []string
		args     []string
		response string
		route    string
		body     map[string]interface{}
	}{
		{"device set, the value after a space", []string{"device", "set"}, []string{"fw-a", "--boot-environment", "false"}, fwAssignSetting,
			base + "/devices", map[string]interface{}{"devices": []interface{}{"fw-a"}, "boot_environment": false}},
		{"device set, the value after =", []string{"device", "set"}, []string{"fw-a", "--boot-environment=false"}, fwAssignSetting,
			base + "/devices", map[string]interface{}{"devices": []interface{}{"fw-a"}, "boot_environment": false}},
		{"track set, the value after a space", []string{"track", "set"}, []string{"fast", "--auto-apply-major", "false"}, fwTrackOff,
			base + "/tracks/fast", map[string]interface{}{"auto_apply_major": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, seen, err := runFirmware(t, http.StatusOK, tc.response, "json", tc.path, tc.args...)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if seen.path != tc.route || !reflect.DeepEqual(seen.body, tc.body) {
				t.Errorf("sent %s %v, want %s %v", seen.path, seen.body, tc.route, tc.body)
			}
		})
	}

	for _, tc := range []struct {
		name string
		path []string
		args []string
		want string
	}{
		{"device set, bare at the end", []string{"device", "set"}, []string{"fw-a", "--boot-environment"},
			"flag needs an argument: --boot-environment"},
		{"device set, bare before a device", []string{"device", "set"}, []string{"--boot-environment", "fw-a"},
			`invalid argument "fw-a" for "--boot-environment" flag: want true or false`},
		{"track set, bare at the end", []string{"track", "set"}, []string{"fast", "--auto-apply-major"},
			"flag needs an argument: --auto-apply-major"},
		{"track set, bare before --force", []string{"track", "set"}, []string{"fast", "--auto-apply-major", "--force"},
			`invalid argument "--force" for "--auto-apply-major" flag: want true or false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, seen, err := runFirmware(t, http.StatusOK, fwTrackOn, "table", tc.path, tc.args...)
			if err == nil || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
			if seen.hits != 0 {
				t.Errorf("%d request(s) sent for a refused flag", seen.hits)
			}
		})
	}

	for path, flag := range map[string]string{"track set": "auto-apply-major", "device set": "boot-environment"} {
		usage := firmwareCommand(t, strings.Fields(path)...).Flags().FlagUsages()
		if !strings.Contains(usage, "--"+flag+" true|false") {
			t.Errorf("%s help does not show that --%s takes true or false:\n%s", path, flag, usage)
		}
	}
}
