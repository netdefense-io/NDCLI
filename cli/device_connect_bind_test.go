package cli

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// --webadmin-port is bound before POST /connect: a port in use is refused up
// front when no shell will run, and left to the tunnel when one will. The
// bound listener is the one the tunnel serves on.
func TestBindWebAdminPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port

	_, err = bindWebAdminPort(port, false, true, true, true)
	var svcErr *service.Error
	if !errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput || svcErr.Message == "" {
		t.Errorf("busy port, --webadmin-only: %v, want INVALID_INPUT", err)
	}
	if _, err := bindWebAdminPort(port, false, false, false, false); err == nil {
		t.Error("busy port, no terminal: no refusal")
	}
	if ln, err := bindWebAdminPort(port, false, false, true, true); ln != nil || err != nil {
		t.Errorf("busy port, shell to run: %v, %v; want it left to the tunnel", ln, err)
	}
	if ln, err := bindWebAdminPort(port, true, true, true, true); ln != nil || err != nil {
		t.Errorf("--no-webadmin: %v, %v; want nothing bound", ln, err)
	}
	if ln, err := bindWebAdminPort(0, false, true, true, true); ln != nil || err != nil {
		t.Errorf("auto-assigned port: %v, %v; want it left to the tunnel", ln, err)
	}

	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	ln, err := bindWebAdminPort(freePort, false, true, true, true)
	if err != nil || ln == nil || ln.Addr().(*net.TCPAddr).Port != freePort {
		t.Fatalf("free port: %v, %v; want it bound", ln, err)
	}
	_ = ln.Close()
}

// device connect with a busy --webadmin-port and no shell to run is refused
// before anything is sent: no CONNECT task exists to cancel.
func TestDeviceConnectRefusesABusyWebAdminPortUpFront(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	origAPI, origSvc, origOrg, origTerms := apiClient, svc, orgName, connectTerminals
	t.Cleanup(func() { apiClient, svc, orgName, connectTerminals = origAPI, origSvc, origOrg, origTerms })
	apiClient = api.NewClient(srv.URL, false, &noopAuthProvider{})
	svc = service.New(apiClient, nil, nil)
	orgName = "acme"
	connectTerminals = func() (bool, bool) { return true, true }

	cmd := findCommand(t, "device", "connect")
	for name, value := range map[string]string{"webadmin-only": "true", "webadmin-port": strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			f := cmd.Flags().Lookup(name)
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	err = runDeviceConnect(cmd, []string{"fw-01"})
	var svcErr *service.Error
	if !errors.As(err, &svcErr) || svcErr.Code != service.CodeInvalidInput {
		t.Errorf("err = %v, want INVALID_INPUT", err)
	}
	if requests != 0 {
		t.Errorf("%d request(s) sent, want none", requests)
	}
}
