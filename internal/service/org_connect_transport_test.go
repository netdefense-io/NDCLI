package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const connectTransportAnswer = `{"organization_name":"acme","connect_transport":"direct-peer","connect_relay":"eu",
	"relays":[{"id":"eu","label":"Europe","hostname":"relay-eu.example.net","derp_port":443,"stun_port":3478}],"direct_peer_available":true}`

// connectTransportServer answers the setting's route and records the last
// request's method and body.
func connectTransportServer(t *testing.T, status int, body string) (*Service, *[2]string) {
	t.Helper()
	var seen [2]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/connect-transport" {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		seen = [2]string{r.Method, string(raw)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv), &seen
}

func TestOrgConnectTransportReadsTheSetting(t *testing.T) {
	svc, _ := connectTransportServer(t, http.StatusOK, connectTransportAnswer)
	got, err := svc.OrgConnectTransport(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectTransport != "direct-peer" || got.ConnectRelay == nil || *got.ConnectRelay != "eu" || len(got.Relays) != 1 || !got.DirectPeerAvailable {
		t.Errorf("got %+v", got)
	}
}

func TestOrgSetConnectTransportSendsWhatWasAsked(t *testing.T) {
	for _, tc := range []struct {
		transport, relay string
		answer           string
		body             map[string]string
		wantErr          bool // the answer does not echo what was asked
	}{
		{"direct-peer", "eu", connectTransportAnswer, map[string]string{"connect_transport": "direct-peer", "connect_relay": "eu"}, false},
		// No relay named: the key is left out, so the stored relay is kept.
		{"direct-peer", "", connectTransportAnswer, map[string]string{"connect_transport": "direct-peer"}, false},
		{"relay-legacy", "", connectTransportAnswer, map[string]string{"connect_transport": "relay-legacy"}, true},
		{"direct-peer", "us", connectTransportAnswer, map[string]string{"connect_transport": "direct-peer", "connect_relay": "us"}, true},
	} {
		svc, seen := connectTransportServer(t, http.StatusOK, tc.answer)
		_, err := svc.OrgSetConnectTransport(context.Background(), "acme", tc.transport, tc.relay, false)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s/%s: err = %v, want error %v (the answer must echo what was asked)", tc.transport, tc.relay, err, tc.wantErr)
		}
		var body map[string]string
		_ = json.Unmarshal([]byte(seen[1]), &body)
		if seen[0] != http.MethodPut || len(body) != len(tc.body) || body["connect_transport"] != tc.body["connect_transport"] || body["connect_relay"] != tc.body["connect_relay"] {
			t.Errorf("%s/%s: sent %s %s", tc.transport, tc.relay, seen[0], seen[1])
		}
	}
}

func TestOrgSetConnectTransportRefusesAnUnknownTransport(t *testing.T) {
	svc, seen := connectTransportServer(t, http.StatusOK, connectTransportAnswer)
	_, err := svc.OrgSetConnectTransport(context.Background(), "acme", "fastest", "", false)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || seen[0] != "" {
		t.Errorf("err = %v, request %q; want INVALID_INPUT and nothing sent", err, seen[0])
	}
}

func TestOrgConnectTransportOnAServerWithoutTheRoute(t *testing.T) {
	ctx := context.Background()
	svc, _ := connectTransportServer(t, http.StatusNotFound, `{"detail":"Not Found"}`)
	if _, err := svc.OrgConnectTransport(ctx, "acme"); ErrorCode(err) != CodeServerTooOld {
		t.Errorf("read: %v, want SERVER_TOO_OLD", err)
	}
	if _, err := svc.OrgSetConnectTransport(ctx, "acme", "direct-peer", "eu", false); ErrorCode(err) != CodeServerTooOld {
		t.Errorf("write: %v, want SERVER_TOO_OLD", err)
	}
	svc, _ = connectTransportServer(t, http.StatusConflict, `{"error":"not available","code":"CONNECT_TRANSPORT_UNSUPPORTED"}`)
	if _, err := svc.OrgSetConnectTransport(ctx, "acme", "direct-peer", "eu", false); ErrorCode(err) != "CONNECT_TRANSPORT_UNSUPPORTED" {
		t.Errorf("a refusal lost its code: %v", err)
	}
}

func TestOrgSetConnectTransportClearsTheRelay(t *testing.T) {
	ctx := context.Background()
	cleared := `{"organization_name":"acme","connect_transport":"relay-legacy","connect_relay":null,"relays":[],"direct_peer_available":true}`
	svc, seen := connectTransportServer(t, http.StatusOK, cleared)
	if _, err := svc.OrgSetConnectTransport(ctx, "acme", "relay-legacy", "", true); err != nil {
		t.Fatal(err)
	}
	if seen[1] != `{"connect_relay":null,"connect_transport":"relay-legacy"}` {
		t.Errorf("sent %s, want an explicit null", seen[1])
	}
	svc, _ = connectTransportServer(t, http.StatusOK, connectTransportAnswer)
	if _, err := svc.OrgSetConnectTransport(ctx, "acme", "direct-peer", "", true); err == nil {
		t.Error("an answer that still names a relay was taken as cleared")
	}
	svc, seen = connectTransportServer(t, http.StatusOK, cleared)
	if _, err := svc.OrgSetConnectTransport(ctx, "acme", "relay-legacy", "eu", true); err == nil || seen[0] != "" {
		t.Errorf("a relay named and cleared at once: err %v, request %q", err, seen[0])
	}
}

func TestA404WithACodeIsARefusalOfTheTransportRoute(t *testing.T) {
	svc, _ := connectTransportServer(t, http.StatusNotFound, `{"error":"Organization not found","code":"ORG_NOT_FOUND"}`)
	if _, err := svc.OrgConnectTransport(context.Background(), "acme"); ErrorCode(err) != "ORG_NOT_FOUND" {
		t.Errorf("got %v, want ORG_NOT_FOUND", err)
	}
}
