package oauth2

import (
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// What the identity provider sends is bounded before it becomes a duration:
// a negative interval used to reach time.NewTicker, which panics on it, and a
// huge one wrapped to a short or negative wait.
func TestProviderDurationsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		n, def, max int
		want        time.Duration
	}{
		{5, 5, 300, 5 * time.Second},
		{0, 5, 300, 5 * time.Second},
		{-1, 5, 300, 5 * time.Second},
		{301, 5, 300, 300 * time.Second},
		{1 << 62, 5, 300, 300 * time.Second},
		{1 << 62, 900, 86400, 24 * time.Hour},
	} {
		if got := providerDuration(tc.n, tc.def, tc.max); got != tc.want {
			t.Errorf("providerDuration(%d, %d, %d) = %v, want %v", tc.n, tc.def, tc.max, got, tc.want)
		}
	}

	ia := NewInteractiveAuth(&models.DeviceAuthResponse{Interval: -3, ExpiresIn: 0}, nil)
	if ia.Interval != 5*time.Second || ia.ExpiresIn != 15*time.Minute {
		t.Errorf("interactive interval %v, expires in %v; want the 5 s and 15 min defaults", ia.Interval, ia.ExpiresIn)
	}
}

// A token's expires_in is capped, so a huge one cannot wrap the expiry.
func TestTokenExpiryIsBounded(t *testing.T) {
	now := time.Now()
	if got := tokenExpiry(3600); got.Sub(now) < 59*time.Minute || got.Sub(now) > 61*time.Minute {
		t.Errorf("tokenExpiry(3600) is %v from now", got.Sub(now))
	}
	if got := tokenExpiry(1 << 62); got.Before(now) || got.Sub(now) > maxTokenLifetime+time.Minute {
		t.Errorf("tokenExpiry(huge) is %v from now, want at most %v", got.Sub(now), maxTokenLifetime)
	}
	if got := tokenExpiry(-10); got.After(now.Add(time.Second)) {
		t.Errorf("tokenExpiry(-10) is %v from now, want now (expired)", got.Sub(now))
	}
}
