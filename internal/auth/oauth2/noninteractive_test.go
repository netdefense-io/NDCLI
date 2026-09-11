package oauth2

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/auth/oauth2/providers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// fakeProvider is the whole providers.Provider surface, driven by pollResults.
type fakeProvider struct {
	polls   atomic.Int32
	results []pollResult
}

type pollResult struct {
	token *models.TokenResponse
	err   error
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) RequestDeviceAuthorization(string) (*models.DeviceAuthResponse, error) {
	return deviceAuthResponse(), nil
}

func (f *fakeProvider) PollForToken(string, int) (*models.TokenResponse, error) {
	i := int(f.polls.Add(1)) - 1
	if i >= len(f.results) {
		i = len(f.results) - 1
	}
	return f.results[i].token, f.results[i].err
}

func (f *fakeProvider) RefreshToken(string) (*models.TokenResponse, error) { return nil, nil }
func (f *fakeProvider) RevokeToken(string, string) error                   { return nil }
func (f *fakeProvider) GetUserInfo(string) (*models.UserInfo, error)       { return nil, nil }
func (f *fakeProvider) Close()                                             {}

// fastPolling shrinks the provider's "seconds" to milliseconds so a poll loop
// under test runs in a few milliseconds instead of a few seconds.
func fastPolling(t *testing.T) {
	t.Helper()
	orig := pollIntervalUnit
	pollIntervalUnit = time.Millisecond
	t.Cleanup(func() { pollIntervalUnit = orig })
}

func deviceAuthResponse() *models.DeviceAuthResponse {
	return &models.DeviceAuthResponse{
		DeviceCode:              "device-code",
		UserCode:                "ABCD-EFGH",
		VerificationURIComplete: "https://login.example.test/activate?user_code=ABCD-EFGH",
		Interval:                1,
		ExpiresIn:               600,
	}
}

// captureStdout swaps the real file descriptor, not just a writer: the code
// under test prints with fmt.Print*, which goes straight to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	os.Stdout = orig
	w.Close()
	out := <-done
	r.Close()
	return out
}

// assertNoTerminalControl is the regression assertion for the non-TTY spam:
// the full-screen renderer clears the screen and repaints a countdown once a
// second, which is what a redirected stdout used to collect.
func assertNoTerminalControl(t *testing.T, out string) {
	t.Helper()
	for _, forbidden := range []string{"\033[2J", "\033[H", "\rTime remaining:", "NetDefense Authentication"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("non-interactive output contains terminal control sequence %q:\n%q", forbidden, out)
		}
	}
}

func TestPollNonInteractiveIsQuiet(t *testing.T) {
	fastPolling(t)

	c := &Client{}
	authResp := deviceAuthResponse()

	calls := 0
	pollFunc := func() (*models.TokenResponse, error) {
		calls++
		if calls < 2 {
			return nil, providers.ErrAuthorizationPending
		}
		return &models.TokenResponse{AccessToken: "at"}, nil
	}

	var token *models.TokenResponse
	var result AuthResult
	var err error
	out := captureStdout(t, func() {
		token, result, err = c.pollNonInteractive(context.Background(), authResp, pollFunc)
	})

	if result != AuthSuccess {
		t.Fatalf("result = %v, want AuthSuccess (err %v)", result, err)
	}
	if token == nil || token.AccessToken != "at" {
		t.Fatalf("token = %+v, want the polled token", token)
	}
	if out != "" {
		t.Errorf("the poll loop must print nothing; got:\n%q", out)
	}
	assertNoTerminalControl(t, out)
}

func TestPollNonInteractiveBacksOffOnSlowDown(t *testing.T) {
	fastPolling(t)

	c := &Client{}

	calls := 0
	pollFunc := func() (*models.TokenResponse, error) {
		calls++
		if calls < 2 {
			return nil, providers.ErrSlowDown
		}
		return &models.TokenResponse{AccessToken: "at"}, nil
	}

	_, result, err := c.pollNonInteractive(context.Background(), deviceAuthResponse(), pollFunc)
	if result != AuthSuccess {
		t.Fatalf("result = %v, want AuthSuccess (err %v)", result, err)
	}
}

func TestPollNonInteractiveReturnsPollError(t *testing.T) {
	fastPolling(t)

	c := &Client{}
	want := errors.New("authorization denied by user")

	_, result, err := c.pollNonInteractive(context.Background(), deviceAuthResponse(), func() (*models.TokenResponse, error) {
		return nil, want
	})

	if result != AuthError {
		t.Fatalf("result = %v, want AuthError", result)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to carry %v", err, want)
	}
}

func TestPollNonInteractiveHonoursContextCancel(t *testing.T) {
	fastPolling(t)

	c := &Client{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, result, err := c.pollNonInteractive(ctx, deviceAuthResponse(), func() (*models.TokenResponse, error) {
		return nil, providers.ErrAuthorizationPending
	})
	if result != AuthCancelled {
		t.Fatalf("result = %v (err %v), want AuthCancelled", result, err)
	}
}

func TestPollNonInteractiveTimesOutAtDeviceCodeExpiry(t *testing.T) {
	fastPolling(t)

	c := &Client{}
	authResp := deviceAuthResponse()
	authResp.ExpiresIn = 3 // 3 "seconds" == 3ms under fastPolling

	_, result, err := c.pollNonInteractive(context.Background(), authResp, func() (*models.TokenResponse, error) {
		return nil, providers.ErrAuthorizationPending
	})
	if result != AuthTimeout {
		t.Fatalf("result = %v (err %v), want AuthTimeout", result, err)
	}
}

// TestPollNonInteractiveFallsBackWhenProviderOmitsTimings exercises the
// interval<=0 / expiresIn<=0 branches. They are expressed in the provider's
// own unit so the test hook scales them too — the point of this test is that
// it finishes in milliseconds rather than waiting a real 5 s poll interval.
func TestPollNonInteractiveFallsBackWhenProviderOmitsTimings(t *testing.T) {
	fastPolling(t)

	c := &Client{}
	authResp := deviceAuthResponse()
	authResp.Interval = 0
	authResp.ExpiresIn = 0

	start := time.Now()
	_, result, err := c.pollNonInteractive(context.Background(), authResp, func() (*models.TokenResponse, error) {
		return &models.TokenResponse{AccessToken: "at"}, nil
	})
	if result != AuthSuccess {
		t.Fatalf("result = %v (err %v), want AuthSuccess", result, err)
	}
	// The default interval is 5 units; unscaled that would be 5 seconds.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("fallback interval did not scale with the test hook: took %v", elapsed)
	}
}

// TestLoginNonInteractiveOutput covers the whole non-interactive branch of
// Login: the verification URL and user code are printed exactly once, and
// nothing else reaches stdout. The poll fails so the test never touches real
// token storage.
func TestLoginNonInteractiveOutput(t *testing.T) {
	fastPolling(t)

	provider := &fakeProvider{results: []pollResult{{err: errors.New("device code expired")}}}
	c := &Client{
		domain:   "login.example.test",
		clientID: "client-id",
		provider: provider,
	}

	var err error
	out := captureStdout(t, func() {
		_, err = c.Login(context.Background(), "openid profile", false)
	})

	if err == nil {
		t.Fatal("expected the poll error to be returned")
	}
	if !strings.Contains(err.Error(), "device code expired") {
		t.Errorf("error should carry the provider's message, got: %v", err)
	}

	assertNoTerminalControl(t, out)
	if strings.Count(out, "Or enter code: ABCD-EFGH") != 1 {
		t.Errorf("user code should be printed exactly once, got:\n%q", out)
	}
	if !strings.Contains(out, "https://login.example.test/activate?user_code=ABCD-EFGH") {
		t.Errorf("verification URL missing from output:\n%q", out)
	}
	if lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; lines != 2 {
		t.Errorf("expected exactly 2 lines of output, got %d:\n%q", lines, out)
	}
}

// TestSupportsInteractiveFalseWhenStdoutIsNotATerminal is the gate that routes
// a redirected login to the quiet path in the first place.
func TestSupportsInteractiveFalseWhenStdoutIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	if SupportsInteractive() {
		t.Error("a pipe is not a terminal; the interactive renderer must not be selected")
	}
}
