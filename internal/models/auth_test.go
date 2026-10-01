package models

import "testing"

// TestSessionStatusUsable pins which stored-credential states let the next
// authenticated call succeed without a new login. The refreshable row is the
// one that matters: an expired access token with a refresh token is a working
// session, and treating it as signed out was the bug.
func TestSessionStatusUsable(t *testing.T) {
	tests := []struct {
		name string
		st   *SessionStatus
		want bool
	}{
		{"nil status", nil, false},
		{"none", &SessionStatus{State: SessionNone}, false},
		{"valid", &SessionStatus{State: SessionValid}, true},
		{"refreshable", &SessionStatus{State: SessionRefreshable, HasRefresh: true}, true},
		{"expired without a refresh token", &SessionStatus{State: SessionExpired}, false},
		{"static token", &SessionStatus{State: SessionStatic}, true},
		{"zero value", &SessionStatus{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.st.Usable(); got != tt.want {
				t.Errorf("Usable() = %v, want %v", got, tt.want)
			}
		})
	}
}
