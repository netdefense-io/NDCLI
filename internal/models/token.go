package models

// PersonalAccessToken represents a PAT returned by the list API.
type PersonalAccessToken struct {
	Name        string        `json:"name"`
	TokenPrefix string        `json:"token_prefix"`
	Scope       string        `json:"scope"`
	Org         *string       `json:"org"`
	ExpiresAt   *FlexibleTime `json:"expires_at"`
	LastUsedAt  *FlexibleTime `json:"last_used_at"`
	LastUsedIP  *string       `json:"last_used_ip"`
	CreatedAt   FlexibleTime  `json:"created_at"`
	IsExpired   bool          `json:"is_expired"`
	IsRevoked   bool          `json:"is_revoked"`
}

// Status returns a human-readable status string.
func (p PersonalAccessToken) Status() string {
	if p.IsRevoked {
		return "revoked"
	}
	if p.IsExpired {
		return "expired"
	}
	return "active"
}

// TokenCreateRequest is the request body for POST /api/v1/auth/tokens
type TokenCreateRequest struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Org       string `json:"org,omitempty"`
	ExpiresIn string `json:"expires_in,omitempty"`
}

// TokenCreateResponse is the flat response from POST /api/v1/auth/tokens.
// The raw token is returned only on creation and cannot be retrieved again.
type TokenCreateResponse struct {
	Token     string        `json:"token"`
	Name      string        `json:"name"`
	Scope     string        `json:"scope"`
	Org       *string       `json:"org"`
	ExpiresAt *FlexibleTime `json:"expires_at"`
	CreatedAt FlexibleTime  `json:"created_at"`
}
