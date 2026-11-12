package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// Concurrent pollers must reuse their connections. With the transport's
// default of 2 idle connections per host, six pollers dialled a new
// connection on most polls (#326).
func TestConcurrentPollersReuseConnections(t *testing.T) {
	var dials int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			atomic.AddInt32(&dials, 1)
		}
	}
	srv.Start()
	defer srv.Close()
	c := NewClient(srv.URL, false, nil)

	const pollers, polls = 6, 50
	var wg sync.WaitGroup
	for range pollers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range polls {
				resp, err := c.GetValues(context.Background(), "/api/v1/tasks/t/connect-status", nil)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	if n := atomic.LoadInt32(&dials); n > 3*pollers {
		t.Errorf("%d pollers making %d polls each opened %d connections, want at most %d", pollers, polls, n, 3*pollers)
	}
}
