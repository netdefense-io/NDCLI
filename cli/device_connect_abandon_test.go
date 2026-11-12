package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// deviceConnectAgainst runs device connect against a control plane that keeps
// the CONNECT task at status, with ctx standing in for the user's interrupt,
// and counts the cancels it receives.
func deviceConnectAgainst(t *testing.T, status string, timeout string, ctx context.Context) (int, error) {
	t.Helper()
	return deviceConnectAgainstStatus(t, map[string]any{"task": "Cx123456", "status": status, "message": "device refused"}, timeout, ctx)
}

// deviceConnectAgainstCompleted is deviceConnectAgainst with the task COMPLETED
// and a relay session to dial.
func deviceConnectAgainstCompleted(t *testing.T, timeout string) (int, error) {
	t.Helper()
	return deviceConnectAgainstStatus(t, map[string]any{"task": "Cx123456", "status": "COMPLETED",
		"payload": `{"jti":"j1","pathfinder_session":"sess-abc"}`}, timeout, context.Background())
}

func deviceConnectAgainstStatus(t *testing.T, status map[string]any, timeout string, ctx context.Context) (int, error) {
	t.Helper()
	var mu sync.Mutex
	cancels := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"task": "Cx123456", "status": "PENDING"})
		case http.MethodPut:
			mu.Lock()
			cancels++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Task cancelled successfully"})
		default:
			_ = json.NewEncoder(w).Encode(status)
		}
	}))
	defer srv.Close()

	origAPI, origSvc, origOrg, origTerms, origSignals := apiClient, svc, orgName, connectTerminals, connectSignalContext
	t.Cleanup(func() {
		apiClient, svc, orgName, connectTerminals, connectSignalContext = origAPI, origSvc, origOrg, origTerms, origSignals
	})
	apiClient = api.NewClient(srv.URL, false, &noopAuthProvider{})
	svc = service.New(apiClient, nil, nil)
	orgName = "acme"
	connectTerminals = func() (bool, bool) { return true, true }
	connectSignalContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(ctx) }

	cmd := findCommand(t, "device", "connect")
	if err := cmd.Flags().Set("timeout", timeout); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f := cmd.Flags().Lookup("timeout")
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	err := runDeviceConnect(cmd, []string{"fw-01"})
	mu.Lock()
	defer mu.Unlock()
	return cancels, err
}

// A device connect given up before the session is the user's cancels the
// task, so a device that picks it up later does not wait on the relay for
// nobody: a timeout and an interrupt both do. A task that ended on its own is
// left alone.
func TestDeviceConnectCancelsATaskItGivesUp(t *testing.T) {
	if cancels, err := deviceConnectAgainst(t, "PENDING", "1ms", context.Background()); err == nil || cancels != 1 {
		t.Errorf("timeout: err %v, cancels %d; want an error and one cancel", err, cancels)
	}

	interrupt, stop := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, stop)
	cancels, err := deviceConnectAgainst(t, "PENDING", "1m", interrupt)
	if err == nil || err.Error() != "connect interrupted" || cancels != 1 {
		t.Errorf("interrupt: err %v, cancels %d; want \"connect interrupted\" and one cancel", err, cancels)
	}

	if cancels, err := deviceConnectAgainst(t, "FAILED", "1m", context.Background()); err == nil || cancels != 0 {
		t.Errorf("failed task: err %v, cancels %d; want an error and no cancel", err, cancels)
	}
}

// After the device pairs, an error is the session's own: stopping the
// interrupt handling at pairing also cancels its context, and that must not
// turn a tunnel that cannot listen into "connect interrupted".
func TestDeviceConnectReportsAnErrorAfterPairingAsItIs(t *testing.T) {
	up := websocket.Upgrader{}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{"type": "registered", "payload": map[string]any{"peer_online": true}})
		_ = conn.WriteJSON(map[string]any{"type": "paired"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer relay.Close()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	origHost := config.Get().Pathfinder.Host
	config.Get().Pathfinder.Host = relay.URL
	t.Cleanup(func() { config.Get().Pathfinder.Host = origHost })

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
	cancels, err := deviceConnectAgainstCompleted(t, "1m")
	if err == nil || !strings.Contains(err.Error(), "webadmin tunnel did not start") {
		t.Errorf("err = %v, want the tunnel's own error", err)
	}
	if cancels != 1 {
		t.Errorf("cancels = %d, want 1 (the session served nothing)", cancels)
	}
}
