package models

import "time"

// AuthShow is what `ndcli auth show` reports: the stored session as
// classified locally (no network call, no refresh), the control plane's host
// and the storage backend.
type AuthShow struct {
	State            SessionState `json:"state"`
	Email            string       `json:"email,omitempty"`
	Name             string       `json:"name,omitempty"`
	Subject          string       `json:"subject,omitempty"`
	ExpiresAt        *time.Time   `json:"expires_at,omitempty"`
	RefreshAvailable bool         `json:"refresh_available"`
	Host             string       `json:"host"`
	Storage          string       `json:"storage"`
}

// NewAuthShow builds the report from the session status, nil reading as
// nothing stored.
func NewAuthShow(st *SessionStatus, host, storage string) *AuthShow {
	a := &AuthShow{State: SessionNone, Host: host, Storage: storage}
	if st == nil {
		return a
	}
	a.State, a.Email, a.Name, a.Subject, a.RefreshAvailable = st.State, st.Email, st.Name, st.Subject, st.HasRefresh
	if !st.ExpiresAt.IsZero() {
		expires := st.ExpiresAt
		a.ExpiresAt = &expires
	}
	return a
}

// ConfigView is what `ndcli config show` reports. It holds no secret: the
// auth account is the email@host key of the stored session, not a credential.
type ConfigView struct {
	ConfigFile   string           `json:"config_file"`
	Auth         ConfigViewAuth   `json:"auth"`
	Controlplane ConfigViewHost   `json:"controlplane"`
	Pathfinder   ConfigViewHost   `json:"pathfinder"`
	Organization ConfigViewOrg    `json:"organization"`
	Output       ConfigViewOutput `json:"output"`
}

type ConfigViewAuth struct {
	Storage string `json:"storage"`
	File    string `json:"file,omitempty"`    // the auth file, when storage is file
	Account string `json:"account,omitempty"` // email@host of the stored session
}

type ConfigViewHost struct {
	Host      string `json:"host"`
	SSLVerify bool   `json:"ssl_verify"`
}

type ConfigViewOrg struct {
	Name string `json:"name"`
}

type ConfigViewOutput struct {
	Format   string `json:"format"`
	Timezone string `json:"timezone"`
}

// VersionInfo is what `ndcli version` reports.
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
}
