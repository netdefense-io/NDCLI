package models

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The transports and the reasons are shared with every program that takes
// part in a session; the vector they are all tested against lists them.
func TestConnectVocabularyIsTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../directpeer/testdata/connect-direct-peer-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Lists struct {
			Transports       []string `json:"transports"`
			TransportReasons []string `json:"transport_reasons"`
		} `json:"lists"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ConnectTransports, vector.Lists.Transports) {
		t.Errorf("transports = %q, the vector has %q", ConnectTransports, vector.Lists.Transports)
	}
	if !reflect.DeepEqual(ConnectReasons, vector.Lists.TransportReasons) {
		t.Errorf("reasons = %q, the vector has %q", ConnectReasons, vector.Lists.TransportReasons)
	}
	if ConnectTransportRelayLegacy != "relay-legacy" || ConnectTransportDirectPeer != "direct-peer" {
		t.Errorf("transports are %q and %q on the wire, want relay-legacy and direct-peer", ConnectTransportRelayLegacy, ConnectTransportDirectPeer)
	}
}

func TestConnectInfoDecodes(t *testing.T) {
	const direct = `{
	  "transport": "direct-peer",
	  "reason": "org_setting",
	  "org_transport": "direct-peer",
	  "read_only": false,
	  "device": {"uuid": "3f2b8c1e-6a4d-4e0b-9c57-d1a2b3c4e5f6", "pubkey": "GNYj+6IySqyqrU9AAhGW4DqyIfBRDzEKgc8THotleiE=", "agent_version": "1.32.0"},
	  "protocol": 1,
	  "relay": {"id": "eu", "label": "Europe", "hostname": "relay-eu.example.net", "relay_port": 443, "stun_port": 3478},
	  "expires_at": 1794700800,
	  "limits": {"max_seconds": 28800, "idle_seconds": 1800, "pair_seconds": 300, "device_sessions": 4}
	}`
	var info ConnectInfo
	if err := json.Unmarshal([]byte(direct), &info); err != nil {
		t.Fatal(err)
	}
	version, protocol, expires := "1.32.0", 1, int64(1794700800)
	want := ConnectInfo{
		Transport:    "direct-peer",
		Reason:       "org_setting",
		OrgTransport: "direct-peer",
		Device:       ConnectInfoDevice{UUID: "3f2b8c1e-6a4d-4e0b-9c57-d1a2b3c4e5f6", Pubkey: "GNYj+6IySqyqrU9AAhGW4DqyIfBRDzEKgc8THotleiE=", AgentVersion: &version},
		Protocol:     &protocol,
		Relay:        &ConnectRelay{ID: "eu", Label: "Europe", Hostname: "relay-eu.example.net", RelayPort: 443, STUNPort: 3478},
		ExpiresAt:    &expires,
		Limits:       &ConnectLimits{MaxSeconds: 28800, IdleSeconds: 1800, PairSeconds: 300, DeviceSessions: 4},
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("decoded %+v\n   want %+v", info, want)
	}
	if info.FallsBack() {
		t.Error("a direct-peer session reads as a fallback")
	}

	// The websocket relay's answer: what only a direct-peer session needs is
	// null, and an agent whose version cannot be read has none.
	const legacy = `{"transport":"relay-legacy","reason":"agent_too_old","org_transport":"direct-peer","read_only":true,
	  "device":{"uuid":"3f2b8c1e-6a4d-4e0b-9c57-d1a2b3c4e5f6","pubkey":"GNYj+6IySqyqrU9AAhGW4DqyIfBRDzEKgc8THotleiE=","agent_version":null},
	  "protocol":null,"relay":null,"expires_at":null,"limits":null}`
	info = ConnectInfo{}
	if err := json.Unmarshal([]byte(legacy), &info); err != nil {
		t.Fatal(err)
	}
	if info.Transport != ConnectTransportRelayLegacy || info.Reason != ConnectReasonAgentTooOld || !info.ReadOnly ||
		info.Device.AgentVersion != nil || info.Protocol != nil || info.Relay != nil || info.ExpiresAt != nil || info.Limits != nil {
		t.Errorf("decoded %+v, want the websocket relay with every direct-peer field null", info)
	}
	if !info.FallsBack() {
		t.Error("an organization set to direct-peer whose session is on the websocket relay does not read as a fallback")
	}

	for _, tc := range []struct {
		org, transport string
		fallsBack      bool
	}{
		{"relay-legacy", "relay-legacy", false},
		{"direct-peer", "direct-peer", false},
		{"direct-peer", "relay-legacy", true},
		{"", "relay-legacy", false},
	} {
		if got := (ConnectInfo{OrgTransport: tc.org, Transport: tc.transport}).FallsBack(); got != tc.fallsBack {
			t.Errorf("organization %q, session %q: FallsBack = %v, want %v", tc.org, tc.transport, got, tc.fallsBack)
		}
	}
}

// A server that predates the choice of transport answers as it always has.
// Its answers decode with nothing in the new fields, and encode again without
// them, so what a script reads from -f json does not change.
func TestAnOlderServersConnectAnswersAreUnchanged(t *testing.T) {
	const initial = `{"task":"tQSd9RCV","status":"PENDING","message":"Poll GET /api/v1/tasks/tQSd9RCV/connect-status"}`
	var created ConnectInitResponse
	if err := json.Unmarshal([]byte(initial), &created); err != nil {
		t.Fatal(err)
	}
	if created.Transport != "" || created.TransportReason != "" || created.ConnectID != "" {
		t.Errorf("an older server's answer filled a new field: %+v", created)
	}
	if again, _ := json.Marshal(created); string(again) != initial {
		t.Errorf("encoded again as %s, want %s", again, initial)
	}

	const status = `{"task":"tQSd9RCV","status":"COMPLETED","payload":"{\"jti\":\"j\",\"pathfinder_session\":\"s\"}","message":"Session ready","read_only":false}`
	var polled ConnectStatusResponse
	if err := json.Unmarshal([]byte(status), &polled); err != nil {
		t.Fatal(err)
	}
	if polled.Transport != "" || polled.ConnectID != "" || polled.Relay != nil || polled.Session != "" {
		t.Errorf("an older server's status filled a new field: %+v", polled)
	}
	if again, _ := json.Marshal(polled); string(again) != status {
		t.Errorf("encoded again as %s, want %s", again, status)
	}
}

func TestADirectPeerStatusDecodes(t *testing.T) {
	const status = `{"task":"tQSd9RCV","status":"COMPLETED","message":"Session ready","read_only":false,
	  "transport":"direct-peer","connect_id":"eff97f3e3ad593d08e250b5a5e492449","session":"ready",
	  "relay":{"id":"eu","label":"Europe","hostname":"relay-eu.example.net","relay_port":443,"stun_port":3478}}`
	var polled ConnectStatusResponse
	if err := json.Unmarshal([]byte(status), &polled); err != nil {
		t.Fatal(err)
	}
	wantRelay := ConnectRelay{ID: "eu", Label: "Europe", Hostname: "relay-eu.example.net", RelayPort: 443, STUNPort: 3478}
	if polled.Transport != ConnectTransportDirectPeer || polled.ConnectID != "eff97f3e3ad593d08e250b5a5e492449" ||
		polled.Session != ConnectSessionReady || polled.Relay == nil || *polled.Relay != wantRelay {
		t.Errorf("decoded %+v", polled)
	}
	if ConnectSessionPending != "pending" || ConnectSessionReady != "ready" || ConnectSessionEnded != "ended" {
		t.Error("the session states are pending, ready and ended on the wire")
	}
}

// The request for a direct-peer session has exactly these seven keys; the
// server refuses a body with any other.
func TestTheDirectPeerRequestBody(t *testing.T) {
	body, err := json.Marshal(ConnectDirectRequest{
		Transport: ConnectTransportDirectPeer,
		Protocol:  1,
		Relay:     "eu",
		ConnectID: "eff97f3e3ad593d08e250b5a5e492449",
		ExpiresAt: 1794700800,
		Sealed:    "c2VhbGVk",
		AdmitKeys: ConnectAdmitKeys{Server: strings.Repeat("0a", 32), Client: strings.Repeat("0b", 32)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"transport":"direct-peer","protocol":1,"relay":"eu","connect_id":"eff97f3e3ad593d08e250b5a5e492449","expires_at":1794700800,"sealed":"c2VhbGVk",` +
		`"admit_keys":{"server":"` + strings.Repeat("0a", 32) + `","client":"` + strings.Repeat("0b", 32) + `"}}`
	if string(body) != want {
		t.Errorf("body:\n got %s\nwant %s", body, want)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"admit_keys", "connect_id", "expires_at", "protocol", "relay", "sealed", "transport"}; !reflect.DeepEqual(names, want) {
		t.Errorf("keys = %q, want %q", names, want)
	}
}

func TestConnectTransportName(t *testing.T) {
	for in, want := range map[string]string{
		"relay-legacy": "websocket relay",
		"":             "websocket relay",
		"direct-peer":  "direct-peer + encrypted relay",
		"some-future":  "some-future",
	} {
		if got := ConnectTransportName(in); got != want {
			t.Errorf("ConnectTransportName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConnectFallbackText(t *testing.T) {
	for in, want := range map[string]string{
		"direct_peer_disabled": "direct-peer + encrypted relay is switched off on this server",
		"client_unsupported":   "this program did not offer direct-peer + encrypted relay",
		"agent_too_old":        "the device's agent is too old for direct-peer + encrypted relay",
		"protocol_mismatch":    "this program and the device's agent are on different generations of direct-peer + encrypted relay, and the older one needs an update",
		"a_reason_from_later":  "a_reason_from_later",
	} {
		if got := ConnectFallbackText(in); got != want {
			t.Errorf("ConnectFallbackText(%q) = %q, want %q", in, got, want)
		}
	}
	// Every reason that is a fallback has a sentence of its own, and none of
	// them names what the transport is built on.
	for _, reason := range ConnectReasons {
		if reason == ConnectReasonOrgSetting {
			continue
		}
		text := ConnectFallbackText(reason)
		if text == reason {
			t.Errorf("the reason %q has no sentence", reason)
		}
		for _, word := range []string{"tailcat", "tailscale", "derp", "wireguard"} {
			if strings.Contains(strings.ToLower(text), word) {
				t.Errorf("the sentence for %q names %q: %s", reason, word, text)
			}
		}
	}
}

func TestACancelThatLeavesASessionClosing(t *testing.T) {
	r := TaskCancelResult{Task: "tQSd9RCV", Outcome: TaskCancelOutcomeSessionClosing, Message: "Remote session closing", CloseTask: "c1o2s3e4"}
	want := "Remote session closing: tQSd9RCV (the task stays COMPLETED). The relay no longer admits it and the device has been told to close (task c1o2s3e4)"
	if got := r.Summary(); got != want {
		t.Errorf("Summary = %q\nwant %q", got, want)
	}
}

// The port is read from relay_port, or from derp_port when a server older
// than the rename sends only that, and is always written as relay_port.
func TestTheRelayPortKey(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"relay_port", `{"id":"eu","hostname":"h","relay_port":443,"stun_port":3478}`, 443},
		{"an older server's derp_port", `{"id":"eu","hostname":"h","derp_port":8443,"stun_port":3478}`, 8443},
		{"both, relay_port first", `{"id":"eu","hostname":"h","relay_port":443,"derp_port":8443,"stun_port":3478}`, 443},
		{"both, derp_port first", `{"id":"eu","hostname":"h","derp_port":8443,"relay_port":443,"stun_port":3478}`, 443},
		{"neither", `{"id":"eu","hostname":"h","stun_port":3478}`, 0},
	} {
		var r ConnectRelay
		if err := json.Unmarshal([]byte(tc.body), &r); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if r.RelayPort != tc.want || r.ID != "eu" || r.Hostname != "h" || r.STUNPort != 3478 {
			t.Errorf("%s: decoded %+v, want port %d", tc.name, r, tc.want)
		}
	}
	var r ConnectRelay
	if err := json.Unmarshal([]byte(`{"id":"eu","hostname":"h","relay_port":"443"}`), &r); err == nil {
		t.Error("a port that is not a number decoded")
	}
	out, err := json.Marshal(ConnectRelay{ID: "eu", Hostname: "h", RelayPort: 443, STUNPort: 3478})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"id":"eu","hostname":"h","relay_port":443,"stun_port":3478}` {
		t.Errorf("encoded as %s", out)
	}
}
