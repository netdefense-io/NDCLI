package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// One rule for the PING target on every surface: surrounding spaces are
// trimmed, and whatever NDAgent's ping handler would refuse is refused here.
func TestCheckPingHost(t *testing.T) {
	longest := strings.Repeat("a", PingHostMaxLength)
	for in, want := range map[string]string{
		"8.8.8.8":            "8.8.8.8",
		" 8.8.8.8 ":          "8.8.8.8",
		"fw.example.test":    "fw.example.test",
		"FW-01.Example.test": "FW-01.Example.test",
		longest:              longest,
	} {
		if got, err := CheckPingHost(in, "--host"); err != nil || got != want {
			t.Errorf("CheckPingHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	const (
		charset = "--host must be one IPv4 address or hostname, in letters, digits, dots and hyphens"
		ipv6    = "--host must be an IPv4 address or a hostname: devices do not ping IPv6 addresses yet"
	)
	for in, msg := range map[string]string{
		"":                 "--host is required for ping",
		"   ":              "--host is required for ping",
		"2001:db8::1":      ipv6,
		"[2001:db8::1]":    ipv6,
		"fe80::1%em0":      ipv6,
		"::ffff:1.2.3.4":   ipv6 + "; give 1.2.3.4",
		"8.8.8.8 1.1.1.1":  charset,
		"8.8.8.8\t1.1.1.1": charset,
		"fw\x1b[2J":        charset,
		"fw\u200b.example": charset,
		"\ufeff8.8.8.8":    charset,
		"\u202efw.example": charset,
		"fw_01":            charset,
		"fw.exämple.test":  charset,
		"fw:8080":          charset,
		"1.1.1.1;reboot":   charset,
		"-f":               "--host must not start with a hyphen",
		"-c4":              "--host must not start with a hyphen",
		longest + "a":      "--host must be at most 253 characters",
	} {
		_, err := CheckPingHost(in, "--host")
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != msg {
			t.Errorf("CheckPingHost(%q) = %v, want INVALID_INPUT %q", in, err, msg)
		}
	}
}

// ndagentPingTarget is a literal copy of NDAgent's ValidatePingTarget
// (internal/security/validation.go at NDAgent-go d639612): its pattern and
// MaxHostnameLength.
// NDCLI-go's CI cannot read that repository, so a change there means editing
// this copy and CheckPingHost together.
var ndagentPingTarget = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

const ndagentMaxHostnameLength = 253

// CheckPingHost sends a trimmed target exactly when NDAgent would run it,
// except a leading hyphen, which NDAgent passes to ping as an option.
func TestCheckPingHostMatchesNDAgent(t *testing.T) {
	inputs := []string{
		"8.8.8.8", "fw.example.test", "a-b.c", "...", "1.2.3.4.5", "-f", "-",
		"2001:db8::1", "fw_01", "fw 01", "fw\x00", "ä", "fw/01", "fw@host",
		strings.Repeat("a", ndagentMaxHostnameLength), strings.Repeat("a", ndagentMaxHostnameLength+1),
	}
	for r := rune(0); r < 0x80; r++ {
		inputs = append(inputs, "fw"+string(r)+"01")
	}
	for _, in := range inputs {
		agentRuns := ndagentPingTarget.MatchString(in) && len(in) <= ndagentMaxHostnameLength && in[0] != '-'
		got, err := CheckPingHost(in, "host")
		sent := err == nil && got == in
		if sent != agentRuns {
			t.Errorf("CheckPingHost(%q): sent %v, NDAgent runs it %v", in, sent, agentRuns)
		}
	}
}

// Run and RunRegisterSpec refuse a PING target CheckPingHost would not send,
// so a caller that skips its front-end's check cannot reach the endpoint.
func TestRunRefusesAPingTargetCheckPingHostWouldNotSend(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&posts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"type": "PING", "total": 1, "tasks": []interface{}{}})
	}))
	defer srv.Close()
	svc := newTestService(t, srv)
	for _, schedule := range []string{"", "nightly"} {
		for _, target := range []interface{}{nil, "", "  ", " 1.1.1.1", "1.1.1.1 8.8.8.8", "2001:db8::1", "-f", 42} {
			payload := map[string]interface{}{}
			if target != nil {
				payload["target"] = target
			}
			opts := RunOpts{Type: models.TaskTypePing, Devices: []string{"fw-a"}, Payload: payload, Schedule: schedule}
			var err error
			if schedule != "" {
				_, err = svc.RunRegisterSpec(context.Background(), "acme", opts)
			} else {
				_, err = svc.Run(context.Background(), "acme", opts)
			}
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
				t.Errorf("schedule %q, target %#v: err = %v, want INVALID_INPUT", schedule, target, err)
			}
		}
	}
	if n := atomic.LoadInt32(&posts); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}
}
