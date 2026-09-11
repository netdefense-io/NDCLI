package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// provisionStub is a stand-in NDManager for the three endpoints
// NetworkPrefixProvision touches. Each field decides how that endpoint behaves;
// the recorder fields let a test assert what was actually written.
type provisionStub struct {
	// orgVar / deviceVar hold the value each scope already has. An empty
	// string means the variable does not exist and GET returns 404.
	orgVar    string
	deviceVar string
	// prefixExists makes the POST return NDManager's ALREADY_EXISTS conflict.
	prefixExists bool
	// prefixConflictCode overrides the code on that conflict, to stand for a
	// 409 that means something other than "already there".
	prefixConflictCode string
	// prefixPublish is the flag the existing prefix carries, returned by the
	// list endpoint the already-exists path reads back.
	prefixPublish bool
	// listBroken makes the read-back fail, so the publish flag is unknowable.
	listBroken bool

	orgCreates    []string
	deviceCreates []string
	prefixPosts   int
}

func (p *provisionStub) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		switch {
		// Org-scope variable.
		case path == "/api/v1/organizations/acme/variables/lan" && r.Method == http.MethodGet:
			if p.orgVar == "" {
				writeAPIError(w, 404, "NOT_FOUND", "variable not found")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"name": "lan", "value": p.orgVar})

		case path == "/api/v1/organizations/acme/variables" && r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			p.orgCreates = append(p.orgCreates, fmt.Sprint(body["value"]))
			p.orgVar = fmt.Sprint(body["value"])
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"name": "lan", "value": p.orgVar})

		// Device-scope override.
		case path == "/api/v1/organizations/acme/devices/fw01/variables/lan" && r.Method == http.MethodGet:
			if p.deviceVar == "" {
				writeAPIError(w, 404, "NOT_FOUND", "variable not found")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"name": "lan", "value": p.deviceVar})

		case path == "/api/v1/organizations/acme/devices/fw01/variables" && r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			p.deviceCreates = append(p.deviceCreates, fmt.Sprint(body["value"]))
			p.deviceVar = fmt.Sprint(body["value"])
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"name": "lan", "value": p.deviceVar})

		// Prefix.
		case strings.HasSuffix(path, "/members/fw01/prefixes") && r.Method == http.MethodPost:
			p.prefixPosts++
			if p.prefixExists {
				code := p.prefixConflictCode
				if code == "" {
					code = "ALREADY_EXISTS"
				}
				writeAPIError(w, 409, code, "conflict")
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{
				"vpn_network": "hq", "device_name": "fw01",
				"variable_name": "lan", "publish": true,
			})

		case strings.HasSuffix(path, "/members/fw01/prefixes") && r.Method == http.MethodGet:
			if p.listBroken {
				writeAPIError(w, 500, "INTERNAL_ERROR", "boom")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"vpn_network": "hq", "device_name": "fw01",
					"variable_name": "lan", "publish": p.prefixPublish,
				}},
				"total": 1, "page": 1, "per_page": prefixScanPerPage,
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			writeAPIError(w, 500, "INTERNAL_ERROR", "unexpected")
		}
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": msg, "code": code})
}

func runProvision(t *testing.T, stub *provisionStub, cidrs []string) (*NetworkPrefixProvisionResult, error) {
	t.Helper()
	srv := httptest.NewServer(stub.handler(t))
	t.Cleanup(srv.Close)
	svc := newTestService(t, srv)
	return svc.NetworkPrefixProvision(context.Background(), "acme", "hq", "fw01", "lan", cidrs, nil)
}

// TestNetworkPrefixProvisionHappyPath: nothing exists, all three writes happen.
func TestNetworkPrefixProvisionHappyPath(t *testing.T) {
	stub := &provisionStub{}

	result, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err != nil {
		t.Fatalf("NetworkPrefixProvision: %v", err)
	}

	if !result.OrgVariableCreated || !result.DeviceVariableCreated || !result.PrefixCreated {
		t.Errorf("expected all three steps to be created, got %+v", result)
	}
	if result.Value != "10.20.0.0/24" {
		t.Errorf("value = %q", result.Value)
	}
	if len(stub.orgCreates) != 1 || stub.orgCreates[0] != "10.20.0.0/24" {
		t.Errorf("org creates = %v", stub.orgCreates)
	}
	if len(stub.deviceCreates) != 1 || stub.deviceCreates[0] != "10.20.0.0/24" {
		t.Errorf("device creates = %v", stub.deviceCreates)
	}
}

// TestNetworkPrefixProvisionIsResumable is the load-bearing claim of the
// design: re-running after a partial failure must skip the steps that already
// hold the right value and finish the rest, writing nothing twice.
func TestNetworkPrefixProvisionIsResumable(t *testing.T) {
	stub := &provisionStub{orgVar: "10.20.0.0/24", deviceVar: "10.20.0.0/24"}

	result, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err != nil {
		t.Fatalf("a re-run over matching values must succeed: %v", err)
	}

	if result.OrgVariableCreated || result.DeviceVariableCreated {
		t.Errorf("existing matching variables must not be reported as created: %+v", result)
	}
	if !result.PrefixCreated {
		t.Error("the one remaining step should have run")
	}
	if len(stub.orgCreates) != 0 || len(stub.deviceCreates) != 0 {
		t.Errorf("nothing should have been written twice: org=%v device=%v", stub.orgCreates, stub.deviceCreates)
	}
}

func TestNetworkPrefixProvisionRefusesOrgMismatch(t *testing.T) {
	stub := &provisionStub{orgVar: "10.99.0.0/24"}

	_, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err == nil {
		t.Fatal("an org variable holding a different value must be refused")
	}

	msg := err.Error()
	for _, want := range []string{"10.99.0.0/24", "10.20.0.0/24", "organization scope", "will not overwrite"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if stub.prefixPosts != 0 || len(stub.deviceCreates) != 0 {
		t.Error("nothing downstream should have been attempted after the refusal")
	}
}

func TestNetworkPrefixProvisionRefusesDeviceMismatch(t *testing.T) {
	stub := &provisionStub{orgVar: "10.20.0.0/24", deviceVar: "10.99.0.0/24"}

	_, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err == nil {
		t.Fatal("a device override holding a different value must be refused")
	}

	msg := err.Error()
	if !strings.Contains(msg, `device "fw01"`) {
		t.Errorf("the refusal should name the device:\n%s", msg)
	}
	// Step 1 found a matching value, so nothing was created and the error must
	// not claim otherwise.
	if strings.Contains(msg, "Already completed") {
		t.Errorf("nothing was created; the message should not list completed steps:\n%s", msg)
	}
	if stub.prefixPosts != 0 {
		t.Error("the prefix should not have been posted after the refusal")
	}
}

// TestNetworkPrefixProvisionShortCircuitsOnAlreadyPublished: the terminal
// re-run, where every step including the prefix is already in place.
func TestNetworkPrefixProvisionShortCircuitsOnAlreadyPublished(t *testing.T) {
	stub := &provisionStub{
		orgVar: "10.20.0.0/24", deviceVar: "10.20.0.0/24",
		prefixExists: true, prefixPublish: false,
	}

	result, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err != nil {
		t.Fatalf("an already-published prefix is not a failure: %v", err)
	}
	if result.PrefixCreated {
		t.Error("the prefix already existed and must not be reported as created")
	}
	if result.Prefix == nil {
		t.Fatal("the existing prefix should have been read back")
	}
	// The flag it really carries, not the one this invocation asked for.
	if result.Prefix.Publish {
		t.Error("expected the stored publish=false to be reported")
	}
}

// TestNetworkPrefixProvisionLeavesPublishUnknownWhenReadBackFails: if the
// prefix exists but cannot be read, the caller must be able to tell that the
// publish flag was never observed. Reporting a state nobody confirmed is the
// failure this guards.
func TestNetworkPrefixProvisionLeavesPublishUnknownWhenReadBackFails(t *testing.T) {
	stub := &provisionStub{
		orgVar: "10.20.0.0/24", deviceVar: "10.20.0.0/24",
		prefixExists: true, listBroken: true,
	}

	result, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err != nil {
		t.Fatalf("a failed read-back should not fail the operation: %v", err)
	}
	if result.Prefix != nil {
		t.Error("the read-back failed, so there is no prefix to report")
	}
	if result.PrefixCreated {
		t.Error("the prefix already existed")
	}
}

// TestNetworkPrefixProvisionValidatesBeforeWriting: a bad CIDR must not reach
// the server at all.
func TestNetworkPrefixProvisionValidatesBeforeWriting(t *testing.T) {
	stub := &provisionStub{}

	if _, err := runProvision(t, stub, []string{"10.20.0.5/24"}); err == nil {
		t.Fatal("host bits must be rejected")
	}
	if len(stub.orgCreates) != 0 || len(stub.deviceCreates) != 0 || stub.prefixPosts != 0 {
		t.Error("validation must happen before any write")
	}
}

// TestNetworkPrefixProvisionDoesNotSwallowOtherConflicts: a 409 that is not
// ALREADY_EXISTS is a real failure. Treating any conflict as "already done"
// would report success for a write that did not happen — the same failure the
// tri-state Published exists to prevent, arriving by a different door.
func TestNetworkPrefixProvisionDoesNotSwallowOtherConflicts(t *testing.T) {
	stub := &provisionStub{
		orgVar: "10.20.0.0/24", deviceVar: "10.20.0.0/24",
		prefixExists: true, prefixConflictCode: "VARIABLE_CONFLICT",
	}

	result, err := runProvision(t, stub, []string{"10.20.0.0/24"})
	if err == nil {
		t.Fatalf("a non-ALREADY_EXISTS 409 must surface, got result %+v", result)
	}
	if !strings.Contains(err.Error(), "publishing the prefix") {
		t.Errorf("the failure should name the step it happened in:\n%s", err.Error())
	}
}

// TestNetworkPrefixProvisionResumesAcrossIPv6Spellings is the reason
// NormalizePrefixCIDRs stores the canonical form rather than the typed text.
//
// The stored value came from one spelling; the re-run supplies another that
// denotes the same network. Comparing the raw text would call that a mismatch
// and refuse — turning the documented re-run recovery into a dead end for any
// IPv6 user who copy-pasted from something that canonicalises.
func TestNetworkPrefixProvisionResumesAcrossIPv6Spellings(t *testing.T) {
	stub := &provisionStub{orgVar: "2001:db8::/32", deviceVar: "2001:db8::/32"}

	result, err := runProvision(t, stub, []string{"2001:0DB8:0:0:0:0:0:0/32"})
	if err != nil {
		t.Fatalf("a differently-spelled but identical value must resume, not refuse: %v", err)
	}

	if result.OrgVariableCreated || result.DeviceVariableCreated {
		t.Errorf("both variables already held this network: %+v", result)
	}
	if len(stub.orgCreates) != 0 || len(stub.deviceCreates) != 0 {
		t.Errorf("nothing should have been rewritten: org=%v device=%v", stub.orgCreates, stub.deviceCreates)
	}
	if result.Value != "2001:db8::/32" {
		t.Errorf("value = %q, want the canonical form", result.Value)
	}
}
