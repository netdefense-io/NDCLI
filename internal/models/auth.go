package models

import "time"

// StoredOAuth2Config holds the OAuth2 configuration used during authentication
type StoredOAuth2Config struct {
	Domain   string `json:"domain"`
	ClientID string `json:"client_id"`
}

// StoredTokens represents the saved authentication tokens
type StoredTokens struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token,omitempty"`
	IDToken      string              `json:"id_token,omitempty"`
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

// UserInfo represents user information from the OAuth2 provider
type UserInfo struct {
	Subject       string `json:"sub"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	Picture       string `json:"picture,omitempty"`
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
