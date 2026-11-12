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
	"time"
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

// The control plane's transport is Go's default one, so a stalled dial or TLS
// handshake times out and HTTP/2 is used where the server offers it, but with
// no proxy: ndcli connects directly (#371).
func TestControlPlaneTransportIsHardenedAndDirect(t *testing.T) {
	for name, tr := range map[string]*http.Transport{
		"api client": NewClient("https://control.example.test", true, nil).httpClient.Transport.(*http.Transport),
		"cli config": newCLIConfigClient(true).Transport.(*http.Transport),
	} {
		if tr.Proxy != nil {
			t.Errorf("%s: Proxy is set; ndcli must connect directly", name)
		}
		if tr.DialContext == nil || tr.TLSHandshakeTimeout != 10*time.Second || tr.IdleConnTimeout != 90*time.Second {
			t.Errorf("%s: want a dial timeout, a 10s TLS handshake timeout and a 90s idle timeout, got handshake %v idle %v", name, tr.TLSHandshakeTimeout, tr.IdleConnTimeout)
		}
		if !tr.ForceAttemptHTTP2 {
			t.Errorf("%s: HTTP/2 is off", name)
		}
		if tr.HTTP2 == nil || tr.HTTP2.SendPingTimeout != h2PingTimeout || tr.HTTP2.PingTimeout != h2PingTimeout {
			t.Errorf("%s: HTTP/2 health-check PINGs are off: %+v", name, tr.HTTP2)
		}
		if tr.TLSClientConfig == nil || tr.TLSClientConfig.InsecureSkipVerify {
			t.Errorf("%s: TLS verification must stay on with sslVerify", name)
		}
	}
	if tr := NewClient("https://control.example.test", true, nil).httpClient.Transport.(*http.Transport); tr.MaxIdleConnsPerHost != 16 {
		t.Errorf("api client: MaxIdleConnsPerHost = %d, want 16", tr.MaxIdleConnsPerHost)
	}
	if !newTransport(false).TLSClientConfig.InsecureSkipVerify {
		t.Error("sslVerify=false must skip verification, as before")
	}
}

// HTTP/2 is negotiated with ndcli's own TLS config, on the JSON client and on
// rawClient, which has a transport of its own.
func TestControlPlaneClientsNegotiateHTTP2(t *testing.T) {
	var mu sync.Mutex
	var protos []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		protos = append(protos, r.Proto)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	c := NewClient(srv.URL, false, nil)
	if c.httpClient.Transport == c.rawClient.Transport {
		t.Error("rawClient shares the JSON client's transport; a transfer would share its HTTP/2 connection")
	}
	resp, err := c.GetValues(context.Background(), "/api/v1/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	stream, err := c.GetStream(context.Background(), "/api/v1/y")
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(protos) != 2 || protos[0] != "HTTP/2.0" || protos[1] != "HTTP/2.0" {
		t.Errorf("protocols = %v, want HTTP/2.0 for the JSON call and the stream", protos)
	}
}

// Something may wrap http.DefaultTransport; the client then builds Go's
// defaults itself instead of panicking on the type assertion.
func TestNewTransportSurvivesAReplacedDefaultTransport(t *testing.T) {
	tr := defaultTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }))
	if tr.TLSHandshakeTimeout != 10*time.Second || !tr.ForceAttemptHTTP2 || tr.DialContext == nil {
		t.Errorf("fallback transport lacks Go's defaults: %+v", tr)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
