package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
	"github.com/netdefense-io/NDCLI/internal/service"
)

func TestConsoleOpenNamesTheTransport(t *testing.T) {
	fallback := &models.ConnectInfo{Transport: "relay-legacy", OrgTransport: "direct-peer", Reason: "agent_too_old"}
	legacy := &models.ConnectInfo{Transport: "relay-legacy", OrgTransport: "relay-legacy", Reason: "org_setting"}
	for _, tc := range []struct {
		name      string
		transport string
		direct    *bool
		info      *models.ConnectInfo
		want      map[string]interface{}
		message   string
	}{
		{"a server that predates the choice", "relay-legacy", nil, nil, map[string]interface{}{"transport": "relay-legacy"}, "opened"},
		{"an organization on the websocket relay", "relay-legacy", nil, legacy, map[string]interface{}{"transport": "relay-legacy"}, "opened"},
		{"a fallback", "relay-legacy", nil, fallback, map[string]interface{}{"transport": "relay-legacy", "transport_fallback_reason": "agent_too_old"}, "opened on the websocket relay, not on direct-peer + encrypted relay as the organization is set to: the device's agent is too old for direct-peer + encrypted relay"},
		{"direct-peer, path not known yet", "direct-peer", nil, nil, map[string]interface{}{"transport": "direct-peer", "path": nil}, "opened over direct-peer + encrypted relay"},
		{"direct-peer, peer to peer", "direct-peer", ptr(true), nil, map[string]interface{}{"transport": "direct-peer", "path": "direct"}, "opened over direct-peer + encrypted relay"},
		{"direct-peer, through the relay", "direct-peer", ptr(false), nil, map[string]interface{}{"transport": "direct-peer", "path": "relayed"}, "opened over direct-peer + encrypted relay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := &consoleSession{transport: tc.transport}
			if tc.direct != nil {
				sess.setPath(*tc.direct)
			}
			data := map[string]interface{}{}
			msg := addConsoleTransport(data, "opened", sess, tc.info)
			got, _ := json.Marshal(data)
			want, _ := json.Marshal(tc.want)
			if string(got) != string(want) {
				t.Errorf("data = %s, want %s", got, want)
			}
			if msg != tc.message {
				t.Errorf("message = %q, want %q", msg, tc.message)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// A direct-peer session that cannot be set up says which part failed, also
// when the service wrapped the failure.
func TestADirectPeerFailureHasItsOwnCode(t *testing.T) {
	s := &Server{}
	for failure, code := range map[pathfinder.DirectFailure]string{
		pathfinder.DirectCompanionMissing:  "COMPANION_UNAVAILABLE",
		pathfinder.DirectCompanionMismatch: "COMPANION_UNAVAILABLE",
		pathfinder.DirectCompanionFailed:   "COMPANION_UNAVAILABLE",
		pathfinder.DirectRelayUnreachable:  "RELAY_UNREACHABLE",
		pathfinder.DirectNoAnswer:          "DEVICE_NO_ANSWER",
	} {
		cause := &pathfinder.DirectError{Failure: failure, Message: "what failed"}
		for _, err := range []error{cause, &service.Error{Code: service.CodeAPIError, Message: cause.Error(), Err: cause}} {
			result, _ := s.directConnectErrorResult(err)
			raw, _ := json.Marshal(result)
			if !strings.Contains(string(raw), code) || !strings.Contains(string(raw), "what failed") {
				t.Errorf("%s: result %s lacks %s", failure, raw, code)
			}
		}
	}
	result, _ := s.directConnectErrorResult(&service.Error{Code: service.CodeReadOnlySession, Message: "read-only"})
	if raw, _ := json.Marshal(result); !strings.Contains(string(raw), "READ_ONLY_SESSION") {
		t.Errorf("result %s lost the service's code", raw)
	}
	result, _ = s.directConnectErrorResult(errors.New("plain"))
	if raw, _ := json.Marshal(result); !strings.Contains(string(raw), "API_ERROR") {
		t.Errorf("result %s", raw)
	}
}
