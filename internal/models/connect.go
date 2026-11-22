package models

import "encoding/json"

// The transports a remote session can run on, as every request, answer and
// setting names them.
const (
	// ConnectTransportRelayLegacy is the websocket relay.
	ConnectTransportRelayLegacy = "relay-legacy"
	// ConnectTransportDirectPeer is direct-peer + encrypted relay.
	ConnectTransportDirectPeer = "direct-peer"
)

// ConnectTransports lists the transports, the default first.
var ConnectTransports = []string{ConnectTransportRelayLegacy, ConnectTransportDirectPeer}

// Why the server chose a session's transport.
const (
	// ConnectReasonOrgSetting: the organization's setting, followed.
	ConnectReasonOrgSetting = "org_setting"
	// The four below put a session of an organization set to direct-peer on
	// the websocket relay, before it starts.
	ConnectReasonDirectPeerDisabled = "direct_peer_disabled"
	ConnectReasonClientUnsupported  = "client_unsupported"
	ConnectReasonAgentTooOld        = "agent_too_old"
	ConnectReasonProtocolMismatch   = "protocol_mismatch"
)

// ConnectReasons lists the reasons the server gives for a transport.
var ConnectReasons = []string{
	ConnectReasonOrgSetting,
	ConnectReasonDirectPeerDisabled,
	ConnectReasonClientUnsupported,
	ConnectReasonAgentTooOld,
	ConnectReasonProtocolMismatch,
}

// The states of a direct-peer session in a connect-status answer.
const (
	// ConnectSessionPending: the device has not started listening.
	ConnectSessionPending = "pending"
	// ConnectSessionReady: the device listens; dial now.
	ConnectSessionReady = "ready"
	// ConnectSessionEnded: there is no session to dial, now or later.
	ConnectSessionEnded = "ended"
)

// ConnectTransportName is a transport as a user reads it. A transport the
// server did not name is the websocket relay; one this build does not know
// shows as sent.
func ConnectTransportName(transport string) string {
	switch transport {
	case "", ConnectTransportRelayLegacy:
		return "websocket relay"
	case ConnectTransportDirectPeer:
		return "direct-peer + encrypted relay"
	}
	return transport
}

// ConnectFallbackText says why a session of an organization set to
// direct-peer + encrypted relay runs on the websocket relay instead. It is
// the one wording every front-end shows. A reason this build does not know
// shows as sent.
func ConnectFallbackText(reason string) string {
	switch reason {
	case ConnectReasonDirectPeerDisabled:
		return "direct-peer + encrypted relay is switched off on this server"
	case ConnectReasonClientUnsupported:
		return "this program did not offer direct-peer + encrypted relay"
	case ConnectReasonAgentTooOld:
		return "the device's agent is too old for direct-peer + encrypted relay"
	case ConnectReasonProtocolMismatch:
		return "this program and the device's agent are on different generations of direct-peer + encrypted relay, and the older one needs an update"
	}
	return reason
}

// ConnectRelay is a relay as the server describes it.
type ConnectRelay struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	Hostname  string `json:"hostname"`
	RelayPort int    `json:"relay_port"`
	STUNPort  int    `json:"stun_port"`
}

// UnmarshalJSON reads the port from relay_port, or from derp_port when a
// server older than the rename sends only that.
func (r *ConnectRelay) UnmarshalJSON(b []byte) error {
	type plain ConnectRelay
	var v struct {
		plain
		RelayPort *int `json:"relay_port"`
		DERPPort  *int `json:"derp_port"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = ConnectRelay(v.plain)
	switch {
	case v.RelayPort != nil:
		r.RelayPort = *v.RelayPort
	case v.DERPPort != nil:
		r.RelayPort = *v.DERPPort
	}
	return nil
}

// ConnectInfoDevice is the device a session would reach: what the session's
// secret is sealed to and bound to.
type ConnectInfoDevice struct {
	UUID string `json:"uuid"`
	// Pubkey is the device's Ed25519 identity key, base64.
	Pubkey string `json:"pubkey"`
	// AgentVersion is null when the server cannot read it.
	AgentVersion *string `json:"agent_version"`
}

// ConnectLimits are the limits the device holds a direct-peer session to.
type ConnectLimits struct {
	MaxSeconds     int `json:"max_seconds"`
	IdleSeconds    int `json:"idle_seconds"`
	PairSeconds    int `json:"pair_seconds"`
	DeviceSessions int `json:"device_sessions"`
}

// ConnectInfo is the answer to GET .../devices/{device}/connect-info: the
// transport the server would give a session started now, and for direct-peer
// what the client needs to start one. Relay, ExpiresAt and Limits are null
// for the websocket relay.
type ConnectInfo struct {
	Transport string `json:"transport"`
	Reason    string `json:"reason"`
	// OrgTransport is the organization's setting. When it is direct-peer and
	// Transport is not, Reason says why this session falls back.
	OrgTransport string            `json:"org_transport"`
	ReadOnly     bool              `json:"read_only"`
	Device       ConnectInfoDevice `json:"device"`
	// Protocol is the agent's tunnel generation, null when it has none.
	Protocol *int          `json:"protocol"`
	Relay    *ConnectRelay `json:"relay"`
	// ExpiresAt is when the session's task will expire, in seconds since the
	// epoch. The client seals this value and sends it back unchanged.
	ExpiresAt *int64         `json:"expires_at"`
	Limits    *ConnectLimits `json:"limits"`
}

// FallsBack reports whether the organization is set to direct-peer and this
// session is not on it.
func (i ConnectInfo) FallsBack() bool {
	return i.OrgTransport == ConnectTransportDirectPeer && i.Transport != ConnectTransportDirectPeer
}

// ConnectAdmitKeys are a session's two public keys, lowercase hex, which the
// relay admits.
type ConnectAdmitKeys struct {
	Server string `json:"server"`
	Client string `json:"client"`
}

// ConnectDirectRequest is the body of POST .../devices/{device}/connect for a
// direct-peer session. The websocket relay's request has no body.
type ConnectDirectRequest struct {
	Transport string `json:"transport"`
	Protocol  int    `json:"protocol"`
	// Relay is the relay's id.
	Relay     string           `json:"relay"`
	ConnectID string           `json:"connect_id"`
	ExpiresAt int64            `json:"expires_at"`
	Sealed    string           `json:"sealed"`
	AdmitKeys ConnectAdmitKeys `json:"admit_keys"`
}

// OrgConnectTransport is an organization's remote-session transport setting,
// with the relays the server offers for direct-peer.
type OrgConnectTransport struct {
	OrganizationName string `json:"organization_name"`
	ConnectTransport string `json:"connect_transport"`
	// ConnectRelay is the id of the relay direct-peer sessions meet on; null
	// when none is chosen. It may stay set while the transport is the
	// websocket relay.
	ConnectRelay *string        `json:"connect_relay"`
	Relays       []ConnectRelay `json:"relays"`
	// DirectPeerAvailable is false when this server has direct-peer switched
	// off or offers no relay.
	DirectPeerAvailable bool `json:"direct_peer_available"`
}
