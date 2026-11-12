package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The friendly network message must keep the transport error underneath, so
// a caller can tell a cancel or a timeout from a dropped connection.
func TestNetworkErrorKeepsItsCause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url0 := srv.URL
	srv.Close()
	_, err := NewClient(url0, false, nil).Get(context.Background(), "/api/v1/x", nil)
	var urlErr *url.Error
	if err == nil || !errors.As(err, &urlErr) {
		t.Fatalf("err = %v, want a *url.Error underneath", err)
	}
	if !strings.HasPrefix(err.Error(), "cannot connect to controlplane at ") {
		t.Errorf("message changed: %q", err.Error())
	}

	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer blocked.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = NewClient(blocked.URL, false, nil).Get(ctx, "/api/v1/x", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to match context.DeadlineExceeded", err)
	}
}

func TestIsNetworkErrorMatchesOnlyAFailedControlPlaneRequest(t *testing.T) {
	transit := &url.Error{Op: "Get", URL: "https://x", Err: errors.New("connection refused")}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"a failed request", &networkError{msg: "cannot connect", err: transit}, true},
		{"wrapped again", fmt.Errorf("poll: %w", &networkError{msg: "cannot connect", err: transit}), true},
		{"a refresh that failed in transit", fmt.Errorf("failed to get access token: %w", fmt.Errorf("failed to refresh token: %w", transit)), false},
		{"an HTTP error answer", &APIError{StatusCode: 503}, false},
		{"nil", nil, false},
	} {
		if got := IsNetworkError(tc.err); got != tc.want {
			t.Errorf("%s: IsNetworkError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsAPIErrorMatchesOnlyAnHTTPErrorAnswer(t *testing.T) {
	transit := &url.Error{Op: "Get", URL: "https://x", Err: errors.New("connection refused")}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"an HTTP error answer", &APIError{StatusCode: 429}, true},
		{"wrapped", fmt.Errorf("initiate connect: %w", &APIError{StatusCode: 503}), true},
		{"a request that failed in transit", &networkError{msg: "cannot connect", err: transit}, false},
		{"an answer that did not decode", errors.New("invalid character 'n'"), false},
		{"nil", nil, false},
	} {
		if got := IsAPIError(tc.err); got != tc.want {
			t.Errorf("%s: IsAPIError = %v, want %v", tc.name, got, tc.want)
		}
	}
}
