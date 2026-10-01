package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// canceledTaskServer answers the pull and connect flows the way NDManager
// answers for a task somebody cancelled: the status is CANCELED, one L. The
// wait loops compared against CANCELLED, so they never recognised it and
// polled until their timeout.
//
// Only the first status poll says CANCELED. A loop that misses it goes on to
// the next poll, which says FAILED, so a regression ends the loop with the
// wrong message instead of hanging the suite for a minute.
func canceledTaskServer(t *testing.T) *httptest.Server {
	t.Helper()
	var polls atomic.Int32
	status := func() string {
		if polls.Add(1) == 1 {
			return "CANCELED"
		}
		return "FAILED"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pull"):
			json.NewEncoder(w).Encode(map[string]any{"task": "T1", "name": "key", "config_type": "USER", "status": "PENDING"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect"):
			json.NewEncoder(w).Encode(map[string]any{"task": "T1", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/T1":
			json.NewEncoder(w).Encode(map[string]any{"task": "T1", "type": "PULL", "status": status()})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/T1/connect-status":
			json.NewEncoder(w).Encode(map[string]any{"task": "T1", "status": status()})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestSnippetPullWaitStopsWhenTheTaskIsCanceled(t *testing.T) {
	prepareRootCommand()
	srv := canceledTaskServer(t)
	defer srv.Close()

	origSvc, origOrg := svc, orgName
	svc = service.New(api.NewClient(srv.URL, false, &noopAuthProvider{}), nil, nil)
	orgName = "acme"
	t.Cleanup(func() { svc, orgName = origSvc, origOrg })

	cmd, _, err := rootCmd.Find([]string{"snippet", "pull"})
	if err != nil {
		t.Fatalf("find snippet pull: %v", err)
	}
	if err := cmd.Flags().Set("wait", "true"); err != nil {
		t.Fatalf("set --wait: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("wait", "false") })

	err = runSnippetPull(cmd, []string{"fw01", "key"})
	if err == nil || err.Error() != "pull task was cancelled" {
		t.Errorf("err = %v, want \"pull task was cancelled\"", err)
	}
}

func TestDeviceConnectStopsWhenTheTaskIsCanceled(t *testing.T) {
	prepareRootCommand()
	srv := canceledTaskServer(t)
	defer srv.Close()

	origClient, origOrg := apiClient, orgName
	apiClient = api.NewClient(srv.URL, false, &noopAuthProvider{})
	orgName = "acme"
	t.Cleanup(func() { apiClient, orgName = origClient, origOrg })

	cmd, _, err := rootCmd.Find([]string{"device", "connect"})
	if err != nil {
		t.Fatalf("find device connect: %v", err)
	}

	err = runDeviceConnect(cmd, []string{"fw01"})
	if err == nil || err.Error() != "connection was cancelled" {
		t.Errorf("err = %v, want \"connection was cancelled\"", err)
	}
}
