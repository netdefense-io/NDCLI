package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/output"
)

// config show, auth show and version honour -f: under json each prints one
// document, and the human formats print what they always did (#255).
func TestShowCommandsHonourTheFormat(t *testing.T) {
	noServer := func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing may be sent: %s %s", r.Method, r.URL.Path)
	}

	t.Run("config show", func(t *testing.T) {
		stdout, _ := runStreams(t, noServer, "json", "", func() error { return runConfigShow(configShowCmd, nil) })
		var doc map[string]interface{}
		decodeOne(t, stdout, &doc)
		for _, key := range []string{"config_file", "auth", "controlplane", "pathfinder", "organization", "output"} {
			if _, ok := doc[key]; !ok {
				t.Errorf("config show -f json lacks %s: %s", key, stdout)
			}
		}
		human, _ := runStreams(t, noServer, "table", "", func() error { return runConfigShow(configShowCmd, nil) })
		for _, want := range []string{"Configuration:", "Controlplane:", "Output:"} {
			if !strings.Contains(human, want) {
				t.Errorf("config show lacks %q:\n%s", want, human)
			}
		}
	})

	t.Run("auth show", func(t *testing.T) {
		origSvc, origClient, origManager, origFormatter := svc, apiClient, authManager, formatter
		t.Cleanup(func() { svc, apiClient, authManager, formatter = origSvc, origClient, origManager, origFormatter })
		stdout, _ := runStreams(t, noServer, "json", "", func() error {
			authManager = nil
			initStaticAuth(auth.NewStaticTokenProvider("ndpat_test123"))
			formatter = output.GetFormatter("json")
			return runAuthShow(authShowCmd, nil)
		})
		var doc map[string]interface{}
		decodeOne(t, stdout, &doc)
		if doc["state"] != "static_token" || doc["host"] == nil || doc["storage"] == nil {
			t.Errorf("auth show -f json = %v", doc)
		}
		if strings.Contains(stdout, "ndpat_test123") {
			t.Error("auth show printed the token")
		}
	})

	t.Run("version", func(t *testing.T) {
		origFmt := outputFmt
		t.Cleanup(func() { outputFmt = origFmt })
		// version builds its own formatter from -f (outputFmt); the one
		// runStreams sets up is not used.
		for _, tc := range []struct{ format, want string }{
			{"", "ndcli version " + config.Version + "\nBuild time: " + config.BuildTime + "\n"},
			{"json", ""},
		} {
			outputFmt = tc.format
			stdout, _ := runStreams(t, noServer, "table", "", func() error { return versionCmd.RunE(versionCmd, nil) })
			if tc.want != "" {
				if stdout != tc.want {
					t.Errorf("version = %q, want %q", stdout, tc.want)
				}
				continue
			}
			var doc map[string]string
			decodeOne(t, stdout, &doc)
			if doc["version"] != config.Version || doc["build_time"] != config.BuildTime {
				t.Errorf("version -f json = %v", doc)
			}
		}
	})
}
