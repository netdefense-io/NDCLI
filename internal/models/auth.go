package models

import "time"

// StoredOAuth2Config holds the OAuth2 configuration used during authentication
type StoredOAuth2Config struct {
	Domain   string `json:"domain"`
	ClientID string `json:"client_id"`
}

// StoredTokens represents the saved authentication tokens.
//
// Only what ndcli needs to call the API and refresh the session is persisted.
// The id_token in particular is deliberately absent: nothing reads it, and it
// is the single largest field in the bundle, which pushed the whole payload
// past the per-secret size cap of the macOS and Windows keyrings. Documents
// written by older releases still carry an "id_token" key; it is ignored on
// read and dropped the next time the session is written.
type StoredTokens struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token,omitempty"`
	TokenType    string              `json:"token_type"`
	ExpiresAt    time.Time           `json:"expires_at"`
	Scope        string              `json:"scope,omitempty"`
	UserInfo     *UserInfo           `json:"user_info,omitempty"`
	OAuth2Config *StoredOAuth2Config `json:"oauth2_config,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
}

// TokenResponse represents the OAuth2 token response from the provider
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}

// DeviceAuthResponse represents the device authorization response
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// UserInfo represents user information from the OAuth2 provider.
//
// Not every field survives into storage; see Persistable.
type UserInfo struct {
	Subject       string `json:"sub"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	Picture       string `json:"picture,omitempty"`
}

// Persistable returns a copy holding only the fields ndcli reads back out of
// storage: Subject, Email and Name. Dropping the rest keeps unbounded values
// -- a provider "picture" URL has no length limit -- out of a payload that has
// to fit inside the keyring's per-secret cap, and avoids storing profile data
// nothing uses.
func (u *UserInfo) Persistable() *UserInfo {
	if u == nil {
		return nil
	}
	return &UserInfo{
		Subject: u.Subject,
		Email:   u.Email,
		Name:    u.Name,
	}
}

// IsExpired checks if the token is expired (with 60-second buffer)
func (st *StoredTokens) IsExpired() bool {
	return time.Now().Add(60 * time.Second).After(st.ExpiresAt)
}

// AuthMe represents the authenticated user's profile from /api/v1/auth/me
type AuthMe struct {
	Email         string               `json:"email"`
	Name          *string              `json:"name"`
	Status        string               `json:"status"`
	CreatedAt     FlexibleTime         `json:"created_at"`
	UpdatedAt     FlexibleTime         `json:"updated_at"`
	Organizations []AuthMeOrganization `json:"organizations"`
}

// AuthMeOrganization represents an organization membership in the AuthMe response
type AuthMeOrganization struct {
	Name      string       `json:"name"`
	Role      string       `json:"role"`
	Status    string       `json:"status"`
	CreatedAt FlexibleTime `json:"created_at"`
	UpdatedAt FlexibleTime `json:"updated_at"`
}

// GetName returns the name or a default value if null
func (a *AuthMe) GetName() string {
	if a.Name != nil {
		return *a.Name
	}
	return ""
}

// AuthMeUpdateRequest represents the POST request to /api/v1/auth/me
type AuthMeUpdateRequest struct {
	Name string `json:"name,omitempty"`
}

// AuthMeUpdateResponse represents the POST response from /api/v1/auth/me
type AuthMeUpdateResponse struct {
	Message        string          `json:"message"`
	PendingInvites []PendingInvite `json:"pending_invites"`
}

// PendingInvite represents a pending organization invite
type PendingInvite struct {
	Organization string       `json:"organization"`
	Role         string       `json:"role"`
	InvitedBy    string       `json:"invited_by"`
	CreatedAt    FlexibleTime `json:"created_at"`
}
