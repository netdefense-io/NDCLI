package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// all targets every enabled device; org, the name it replaces, still does.
func TestRunTools_AllAndDeprecatedOrgTargetEveryDevice(t *testing.T) {
	want := map[string]any{"devices": []any{}, "ous": []any{}, "all": true}
	for name, input := range map[string]*runInput{
		"all": {All: true, Confirm: true},
		"org": {Org: true, Confirm: true},
	} {
		t.Run(name, func(t *testing.T) {
			body := captureRunRequest(t, func(s *Server) {
				if _, err := s.runCommand(context.Background(), "restart", models.TaskTypeReboot, nil, input); err != nil {
					t.Fatalf("unexpected transport error: %v", err)
				}
			})
			var targets map[string]any
			if err := json.Unmarshal(body["targets"], &targets); err != nil {
				t.Fatalf("targets: %v", err)
			}
			if !reflect.DeepEqual(targets, want) {
				t.Errorf("targets = %v, want %v", targets, want)
			}
			if got := runScopeDescription(input); got != "every device in org" {
				t.Errorf("preview scope = %q", got)
			}
		})
	}
}

func TestRunTools_DescribeOrgAsDeprecatedAll(t *testing.T) {
	for _, tool := range []string{"ndcli.run.restart", "ndcli.run.firmware_upgrade", "ndcli.run.backup"} {
		t.Run(tool, func(t *testing.T) {
			props, _ := registeredToolSchema(t, tool)["properties"].(map[string]any)
			all, _ := props["all"].(map[string]any)
			org, _ := props["org"].(map[string]any)
			if all["type"] != "boolean" || !strings.HasSuffix(fmt.Sprint(all["description"]), "(excludes devices and ous)") {
				t.Errorf("all = %v, want a boolean that says it excludes devices and ous", all)
			}
			if org["type"] != "boolean" || org["description"] != "Deprecated: same as all, kept for existing callers. Use all." {
				t.Errorf("org = %v", org)
			}
		})
	}
}

func TestRunBackup_AllTargetsTheNamedOrganization(t *testing.T) {
	call := callRunBackup(t, http.StatusCreated, mcpBackupFixture, `{"organization":"other-org","all":true,"confirm":true}`)

	if call.isError || !call.resp.Success {
		t.Fatalf("failed: %+v", call.resp.Error)
	}
	if call.path != "/api/v1/organizations/other-org/backups/run" || call.body != `{"targets":{"all":true}}` {
		t.Errorf("sent %s %s", call.path, call.body)
	}
}
