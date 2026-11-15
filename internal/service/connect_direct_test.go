package service

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/directpeer"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

const (
	directInfoRoute   = "GET /api/v1/organizations/acme/devices/fw-01/connect-info"
	directPostRoute   = "POST /api/v1/organizations/acme/devices/fw-01/connect"
	directStatusRoute = "GET /api/v1/tasks/tQSd9RCV/connect-status"
	directCancelRoute = "PUT /api/v1/tasks/tQSd9RCV/cancel"

	// The device identity key of the shared test vector.
	directDevicePubkey = "GNYj+6IySqyqrU9AAhGW4DqyIfBRDzEKgc8THotleiE="

	directInfoBody = `{"transport":"direct-peer","reason":"org_setting","org_transport":"direct-peer","read_only":false,
		"device":{"uuid":"3f2b8c1e-6a4d-4e0b-9c57-d1a2b3c4e5f6","pubkey":"` + directDevicePubkey + `","agent_version":"1.32.0"},
		"protocol":1,"relay":{"id":"eu","label":"Europe","hostname":"relay-eu.example.net","derp_port":443,"stun_port":3478},
		"expires_at":1794700800,"limits":{"max_seconds":28800,"idle_seconds":1800,"pair_seconds":300,"device_sessions":4}}`
	legacyInfoBody   = `{"transport":"relay-legacy","reason":"org_setting","org_transport":"relay-legacy","read_only":false,"device":{"uuid":"u","pubkey":"k","agent_version":null},"protocol":null,"relay":null,"expires_at":null,"limits":null}`
	fallbackInfoBody = `{"transport":"relay-legacy","reason":"agent_too_old","org_transport":"direct-peer","read_only":false,"device":{"uuid":"u","pubkey":"k","agent_version":"1.20.0"},"protocol":null,"relay":null,"expires_at":null,"limits":null}`
	directAccepted   = `{"task":"tQSd9RCV","status":"PENDING","message":"Poll","transport":"direct-peer"}`
	directPending    = `{"task":"tQSd9RCV","status":"PENDING","transport":"direct-peer","session":"pending"}`
	directInProgress = `{"task":"tQSd9RCV","status":"IN_PROGRESS","transport":"direct-peer","session":"pending"}`
	directReady      = `{"task":"tQSd9RCV","status":"COMPLETED","transport":"direct-peer","session":"ready","read_only":false,"message":"Session ready"}`
)

type directRequest struct {
	route string
	query string
	body  string
}

// directServer answers each route with its scripted answers in turn, the
// last one again once they run out, and records every request. A route with
// no script answers the bare 404 of a server that does not have it.
type directServer struct {
	mu     sync.Mutex
	script map[string][]answer
	seen   []directRequest
	srv    *httptest.Server
}

func newDirectServer(t *testing.T, script map[string][]answer) *directServer {
	t.Helper()
	d := &directServer{script: script}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := r.Method + " " + r.URL.Path
		d.mu.Lock()
		d.seen = append(d.seen, directRequest{route, r.URL.RawQuery, string(body)})
		answers := d.script[route]
		a := answer{status: http.StatusNotFound, raw: `{"detail":"Not Found"}`}
		if len(answers) > 0 {
			a = answers[0]
			if len(answers) > 1 {
				d.script[route] = answers[1:]
			}
		}
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.raw)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *directServer) routes() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, r := range d.seen {
		out = append(out, r.route)
	}
	return out
}

func (d *directServer) count(route string) int {
	n := 0
	for _, r := range d.routes() {
		if r == route {
			n++
		}
	}
	return n
}

func (d *directServer) bodies(route string) []models.ConnectDirectRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []models.ConnectDirectRequest
	for _, r := range d.seen {
		if r.route == route {
			var b models.ConnectDirectRequest
			_ = json.Unmarshal([]byte(r.body), &b)
			out = append(out, b)
		}
	}
	return out
}

func ok(raw string) []answer { return []answer{{status: http.StatusOK, raw: raw}} }

func refusal(status int, code string) answer {
	return answer{status: status, raw: `{"error":"refused","code":"` + code + `"}`}
}

// directFixture stands in for the companion and records each secret made.
func directFixture(t *testing.T) *[]directpeer.Secret {
	t.Helper()
	recordWaits(t)
	var secrets []directpeer.Secret
	origCheck, origNew := checkCompanion, newDirectSecret
	checkCompanion = func() error { return nil }
	newDirectSecret = func() (*directpeer.Secret, error) {
		s, err := directpeer.New()
		if err == nil {
			secrets = append(secrets, *s)
		}
		return s, err
	}
	t.Cleanup(func() { checkCompanion, newDirectSecret = origCheck, origNew })
	return &secrets
}

func directConnect(t *testing.T, d *directServer, opts DirectConnectOpts) (*DirectConnect, *models.ConnectInfo, error) {
	t.Helper()
	return newTestService(t, d.srv).DirectConnect(context.Background(), "acme", "fw-01", opts)
}

func TestAServerWithoutThePreflightGetsTheWebsocketRelay(t *testing.T) {
	directFixture(t)
	d := newDirectServer(t, nil)
	session, info, err := directConnect(t, d, DirectConnectOpts{})
	if session != nil || info != nil || err != nil {
		t.Fatalf("got %v, %v, %v; want nothing: the caller goes on as before", session, info, err)
	}
	if got := d.routes(); len(got) != 1 || got[0] != directInfoRoute {
		t.Fatalf("requests = %v, want the preflight alone", got)
	}
	// Both transports and the tunnel generation, or a server would read this
	// program as one that cannot run direct-peer.
	if q := d.seen[0].query; q != "protocol=1&transport=direct-peer&transport=relay-legacy" {
		t.Errorf("query = %q", q)
	}
}

func TestTheServersAnswerDecidesTheTransport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		fallsBack bool
	}{
		{"an organization on the websocket relay", legacyInfoBody, false},
		{"a fallback before the session", fallbackInfoBody, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directFixture(t)
			d := newDirectServer(t, map[string][]answer{directInfoRoute: ok(tc.body)})
			session, info, err := directConnect(t, d, DirectConnectOpts{})
			if err != nil || session != nil || info == nil {
				t.Fatalf("got %v, %v, %v; want the answer alone", session, info, err)
			}
			if info.FallsBack() != tc.fallsBack {
				t.Errorf("FallsBack = %v, want %v", info.FallsBack(), tc.fallsBack)
			}
			if n := d.count(directPostRoute); n != 0 {
				t.Errorf("%d connect requests; the caller's own flow sends it", n)
			}
		})
	}
}

func TestADirectPeerSessionIsOpenedAndHandedOver(t *testing.T) {
	secrets := directFixture(t)
	d := newDirectServer(t, map[string][]answer{
		directInfoRoute:   ok(directInfoBody),
		directPostRoute:   {{status: http.StatusAccepted, raw: directAccepted}},
		directStatusRoute: {{status: 200, raw: directPending}, {status: 200, raw: directInProgress}, {status: 200, raw: directReady}},
	})
	var steps []string
	session, info, err := directConnect(t, d, DirectConnectOpts{Progress: func(m string) { steps = append(steps, m) }})
	if err != nil || session == nil {
		t.Fatalf("got %v, %v", session, err)
	}
	if info == nil || info.Transport != models.ConnectTransportDirectPeer {
		t.Errorf("info = %+v", info)
	}
	// IN_PROGRESS is not ready on this transport: three reads, not two.
	if n := d.count(directStatusRoute); n != 3 {
		t.Errorf("%d status reads, want 3", n)
	}
	if n := d.count(directCancelRoute); n != 0 {
		t.Errorf("a session handed over was cancelled %d times", n)
	}

	secret := (*secrets)[0]
	body := d.bodies(directPostRoute)[0]
	want := models.ConnectDirectRequest{
		Transport: "direct-peer", Protocol: 1, Relay: "eu", ConnectID: secret.ConnectID, ExpiresAt: 1794700800,
		Sealed: body.Sealed,
		// The device runs the server's key.
		AdmitKeys: models.ConnectAdmitKeys{Server: hex.EncodeToString(secret.DevicePublic[:]), Client: hex.EncodeToString(secret.ClientPublic[:])},
	}
	if body != want {
		t.Errorf("request body = %+v\nwant %+v", body, want)
	}
	if raw, err := base64.StdEncoding.DecodeString(body.Sealed); err != nil || len(raw) == 0 || len(raw) > 1024 {
		t.Errorf("sealed: %d bytes, %v", len(raw), err)
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal([]byte(d.seen[1].body), &keys)
	if len(keys) != 7 {
		t.Errorf("the request has %d keys, want exactly 7: %s", len(keys), d.seen[1].body)
	}

	got := session.Session
	if got.RelayHost != "relay-eu.example.net" || got.RelayPort != 443 || got.RelaySTUNPort != 3478 {
		t.Errorf("relay = %s %d %d", got.RelayHost, got.RelayPort, got.RelaySTUNPort)
	}
	if got.ClientKey != secret.ClientKey || got.DeviceKey != secret.DeviceKey || got.PresharedKey != secret.PresharedKey {
		t.Error("the session's keys are not the secret's")
	}
	if session.Task != "tQSd9RCV" || session.Relay.ID != "eu" || session.ReadOnly {
		t.Errorf("session = %+v", session)
	}
	if len(steps) == 0 {
		t.Error("no progress was reported")
	}
}

func directWiped(s *pathfinder.DirectSession) bool {
	var zero [32]byte
	return s.ClientKey == zero && s.DeviceKey == zero && s.PresharedKey == zero
}

func TestADirectPeerSessionThatDoesNotStart(t *testing.T) {
	accepted := []answer{{status: http.StatusAccepted, raw: directAccepted}}
	for _, tc := range []struct {
		name     string
		post     []answer
		status   []answer
		contains string
		cancels  int
	}{
		{"the server started another transport", []answer{{status: 202, raw: `{"task":"tQSd9RCV","status":"PENDING"}`}}, nil, "did not start", 1},
		{"the server names another session", []answer{{status: 202, raw: `{"task":"tQSd9RCV","transport":"direct-peer","connect_id":"00000000000000000000000000000000"}`}}, nil, "did not start", 1},
		{"the session ended before it was joined", accepted, ok(`{"status":"COMPLETED","transport":"direct-peer","session":"ended","message":"Remote session ended: no client connected in time"}`), "no client connected in time", 1},
		{"a completed task with no session state", accepted, ok(`{"status":"COMPLETED","transport":"direct-peer"}`), "ended before", 1},
		{"the status names no transport", accepted, ok(`{"status":"COMPLETED","session":"ready","payload":"{}"}`), "no longer reports", 1},
		{"the device refused", accepted, ok(`{"status":"FAILED","transport":"direct-peer","session":"ended","message":"Remote session refused: key_mismatch"}`), "Remote session refused: key_mismatch", 0},
		{"the task expired", accepted, ok(`{"status":"EXPIRED","transport":"direct-peer","session":"ended"}`), "expired", 0},
		{"the task was cancelled", accepted, ok(`{"status":"CANCELED","transport":"direct-peer","session":"ended"}`), "cancelled", 0},
		{"the status read is refused", accepted, []answer{refusal(403, "ACCESS_DENIED")}, "refused", 1},
		{"the session limit", []answer{refusal(409, "CONNECT_SESSION_LIMIT")}, nil, "refused", 0},
		{"the relay is gone", []answer{refusal(409, "CONNECT_RELAY_UNAVAILABLE")}, nil, "refused", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directFixture(t)
			d := newDirectServer(t, map[string][]answer{
				directInfoRoute:   ok(directInfoBody),
				directPostRoute:   tc.post,
				directStatusRoute: tc.status,
				directCancelRoute: ok(`{"outcome":"SESSION_CLOSING","close_task":"c1"}`),
			})
			session, _, err := directConnect(t, d, DirectConnectOpts{})
			if session != nil || err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("got %v, %v; want an error with %q", session, err, tc.contains)
			}
			if n := d.count(directCancelRoute); n != tc.cancels {
				t.Errorf("%d cancels, want %d", n, tc.cancels)
			}
			if n := d.count(directPostRoute); n != 1 {
				t.Errorf("%d connect requests, want 1", n)
			}
		})
	}
}

func TestARefusalANewStartAnswers(t *testing.T) {
	accepted := answer{status: http.StatusAccepted, raw: directAccepted}
	for _, code := range []string{APICodeConnectInfoStale, APICodeConnectIDConflict, APICodeConnectTransportUnsupported} {
		t.Run(code+" once", func(t *testing.T) {
			secrets := directFixture(t)
			d := newDirectServer(t, map[string][]answer{
				directInfoRoute:   ok(directInfoBody),
				directPostRoute:   {refusal(409, code), accepted},
				directStatusRoute: ok(directReady),
			})
			session, _, err := directConnect(t, d, DirectConnectOpts{})
			if err != nil || session == nil {
				t.Fatalf("got %v, %v", session, err)
			}
			bodies := d.bodies(directPostRoute)
			if len(bodies) != 2 || d.count(directInfoRoute) != 2 {
				t.Fatalf("%d requests after %d answers, want 2 and 2", len(bodies), d.count(directInfoRoute))
			}
			if bodies[0].ConnectID == bodies[1].ConnectID || bodies[0].Sealed == bodies[1].Sealed || bodies[0].AdmitKeys == bodies[1].AdmitKeys {
				t.Error("the second request reused the first one's secret")
			}
			if session.Session.ClientKey != (*secrets)[1].ClientKey {
				t.Error("the session's keys are not the accepted request's")
			}
		})
		t.Run(code+" every time", func(t *testing.T) {
			directFixture(t)
			d := newDirectServer(t, map[string][]answer{
				directInfoRoute: ok(directInfoBody),
				directPostRoute: {refusal(409, code)},
			})
			session, _, err := directConnect(t, d, DirectConnectOpts{})
			if session != nil || api.GetErrorCode(err) != code {
				t.Fatalf("got %v, %v; want the refusal", session, err)
			}
			if n := d.count(directPostRoute); n != directConnectStarts {
				t.Errorf("%d connect requests, want %d", n, directConnectStarts)
			}
		})
	}
}

// Only the server naming the websocket relay puts a session there.
func TestATransportThisBuildDoesNotKnowIsRefused(t *testing.T) {
	for _, body := range []string{`{"transport":"other","org_transport":"other"}`, `{"org_transport":"direct-peer"}`} {
		directFixture(t)
		d := newDirectServer(t, map[string][]answer{directInfoRoute: ok(body)})
		session, info, err := directConnect(t, d, DirectConnectOpts{})
		if session != nil || info != nil || err == nil {
			t.Errorf("%s: got %v, %v, %v; want an error", body, session, info, err)
		}
	}
}

// A 429 comes before anything is created: the connect waits and starts anew,
// without spending one of its starts.
func TestARateLimitedRequestStartsAnew(t *testing.T) {
	directFixture(t)
	limited := answer{status: http.StatusTooManyRequests, raw: `{"error":"slow down","code":"RATE_LIMITED"}`}
	d := newDirectServer(t, map[string][]answer{
		directInfoRoute:   ok(directInfoBody),
		directPostRoute:   {limited, limited, {status: http.StatusAccepted, raw: directAccepted}},
		directStatusRoute: ok(directReady),
	})
	session, _, err := directConnect(t, d, DirectConnectOpts{})
	if err != nil || session == nil {
		t.Fatalf("got %v, %v", session, err)
	}
	bodies := d.bodies(directPostRoute)
	if len(bodies) != 3 || d.count(directInfoRoute) != 3 || bodies[0].ConnectID == bodies[2].ConnectID {
		t.Errorf("%d requests, %d preflights; want 3 and 3, each with its own secret", len(bodies), d.count(directInfoRoute))
	}
}

// A request that got no answer may have created the session, so it is never
// sent again.
func TestARequestWithoutAnAnswerIsNotRepeated(t *testing.T) {
	directFixture(t)
	d := newDirectServer(t, map[string][]answer{
		directInfoRoute: ok(directInfoBody),
		directPostRoute: {{status: http.StatusBadGateway, raw: `<html>bad gateway</html>`}},
	})
	session, _, err := directConnect(t, d, DirectConnectOpts{})
	if session != nil || err == nil {
		t.Fatalf("got %v, %v", session, err)
	}
	if n := d.count(directPostRoute); n != 1 {
		t.Errorf("%d connect requests, want 1", n)
	}
}

// The server may change its answer between the preflight and the request.
// Only its second answer puts the session on the websocket relay.
func TestTheServerChangesItsAnswer(t *testing.T) {
	directFixture(t)
	d := newDirectServer(t, map[string][]answer{
		directInfoRoute: {{status: 200, raw: directInfoBody}, {status: 200, raw: fallbackInfoBody}},
		directPostRoute: {refusal(409, APICodeConnectTransportUnsupported)},
	})
	session, info, err := directConnect(t, d, DirectConnectOpts{})
	if err != nil || session != nil || info == nil || !info.FallsBack() || info.Reason != models.ConnectReasonAgentTooOld {
		t.Fatalf("got %v, %+v, %v; want the fallback the server answered", session, info, err)
	}
	if n := d.count(directPostRoute); n != 1 {
		t.Errorf("%d connect requests, want 1", n)
	}
}

func TestNothingIsAskedOfTheDeviceWhenTheSessionCannotRun(t *testing.T) {
	missing := &pathfinder.DirectError{Failure: pathfinder.DirectCompanionMissing, Message: "netdefense-connect is missing"}
	for _, tc := range []struct {
		name  string
		info  string
		opts  DirectConnectOpts
		check error
		code  string
	}{
		{"the companion is missing", directInfoBody, DirectConnectOpts{}, missing, CodeAPIError},
		{"a read-only session the caller cannot use", strings.Replace(directInfoBody, `"read_only":false`, `"read_only":true`, 1), DirectConnectOpts{RefuseReadOnly: true}, nil, CodeReadOnlySession},
		{"no relay", strings.Replace(directInfoBody, `"relay":{"id":"eu","label":"Europe","hostname":"relay-eu.example.net","derp_port":443,"stun_port":3478}`, `"relay":null`, 1), DirectConnectOpts{}, nil, CodeAPIError},
		{"no expiry", strings.Replace(directInfoBody, `"expires_at":1794700800`, `"expires_at":null`, 1), DirectConnectOpts{}, nil, CodeAPIError},
		{"no device key", strings.Replace(directInfoBody, directDevicePubkey, "", 1), DirectConnectOpts{}, nil, CodeAPIError},
		{"a device key that is not one", strings.Replace(directInfoBody, directDevicePubkey, "AAAA", 1), DirectConnectOpts{}, nil, CodeAPIError},
		{"another tunnel generation", strings.Replace(directInfoBody, `"protocol":1`, `"protocol":2`, 1), DirectConnectOpts{}, nil, CodeAPIError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directFixture(t)
			checkCompanion = func() error { return tc.check }
			d := newDirectServer(t, map[string][]answer{directInfoRoute: ok(tc.info), directPostRoute: {{status: 202, raw: directAccepted}}})
			session, info, err := directConnect(t, d, DirectConnectOpts{RefuseReadOnly: tc.opts.RefuseReadOnly})
			var svcErr *Error
			if session != nil || info != nil || !errors.As(err, &svcErr) || svcErr.Code != tc.code {
				t.Fatalf("got %v, %v, %v; want %s and no fallback", session, info, err, tc.code)
			}
			if tc.check != nil {
				var direct *pathfinder.DirectError
				if !errors.As(err, &direct) || direct.Failure != pathfinder.DirectCompanionMissing {
					t.Errorf("the companion's failure is lost: %v", err)
				}
			}
			if n := d.count(directPostRoute); n != 0 {
				t.Errorf("%d connect requests, want none", n)
			}
		})
	}
}

// A legacy organization never depends on the companion being installed.
func TestTheCompanionIsCheckedOnlyForADirectPeerSession(t *testing.T) {
	directFixture(t)
	checkCompanion = func() error { t.Error("the companion was checked"); return nil }
	d := newDirectServer(t, map[string][]answer{directInfoRoute: ok(legacyInfoBody)})
	if _, _, err := directConnect(t, d, DirectConnectOpts{}); err != nil {
		t.Fatal(err)
	}
}

func TestThePreflightsRefusals(t *testing.T) {
	t.Run("a refusal keeps its code", func(t *testing.T) {
		directFixture(t)
		d := newDirectServer(t, map[string][]answer{directInfoRoute: {refusal(409, APICodeConnectAgentTooOld)}})
		_, info, err := directConnect(t, d, DirectConnectOpts{})
		if info != nil || api.GetErrorCode(err) != APICodeConnectAgentTooOld {
			t.Fatalf("got %v, %v", info, err)
		}
		if n := d.count(directInfoRoute); n != 1 {
			t.Errorf("%d reads, want 1", n)
		}
	})
	t.Run("a 404 with a code is not an old server", func(t *testing.T) {
		directFixture(t)
		d := newDirectServer(t, map[string][]answer{directInfoRoute: {refusal(404, "DEVICE_NOT_FOUND")}})
		_, _, err := directConnect(t, d, DirectConnectOpts{})
		if api.GetErrorCode(err) != "DEVICE_NOT_FOUND" {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a failing server is asked again", func(t *testing.T) {
		directFixture(t)
		d := newDirectServer(t, map[string][]answer{directInfoRoute: {{status: 502, raw: `{}`}, {status: 200, raw: legacyInfoBody}}})
		_, info, err := directConnect(t, d, DirectConnectOpts{})
		if err != nil || info == nil {
			t.Fatalf("got %v, %v", info, err)
		}
	})
	t.Run("and never taken for an old one", func(t *testing.T) {
		directFixture(t)
		d := newDirectServer(t, map[string][]answer{directInfoRoute: {{status: 502, raw: `{}`}}})
		_, info, err := directConnect(t, d, DirectConnectOpts{})
		if err == nil || info != nil {
			t.Fatalf("got %v, %v; want the error", info, err)
		}
	})
}

func TestAConnectGivenUpIsCancelledAndItsKeysCleared(t *testing.T) {
	directFixture(t)
	d := newDirectServer(t, map[string][]answer{
		directStatusRoute: ok(directPending),
		directCancelRoute: ok(`{"outcome":"CANCELED"}`),
	})
	ctx, cancel := context.WithCancel(context.Background())
	polls := 0
	consoleWait = func(ctx context.Context, _ time.Duration) error {
		if polls++; polls == 3 {
			cancel()
		}
		return ctx.Err()
	}
	held := &pathfinder.DirectSession{ClientKey: [32]byte{1}, DeviceKey: [32]byte{2}, PresharedKey: [32]byte{3}}
	far := time.Now().Add(time.Hour)
	session, _, err := newTestService(t, d.srv).awaitDirect(ctx, "fw-01", &models.ConnectInfo{}, &directOpened{task: "tQSd9RCV", session: held}, far, far, func(string) {})
	if session != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, %v", session, err)
	}
	if n := d.count(directCancelRoute); n != 1 {
		t.Errorf("%d cancels, want 1", n)
	}
	if !directWiped(held) {
		t.Error("the keys of a session nobody will run were kept")
	}
}
