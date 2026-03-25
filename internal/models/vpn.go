package models

// VpnNetwork represents a VPN network
type VpnNetwork struct {
	Name              string       `json:"name"`
	OverlayCIDRv4     string       `json:"overlay_cidr_v4"`
	AutoConnectHubs   bool         `json:"auto_connect_hubs"`
	ListenPortDefault int          `json:"listen_port_default"`
	MTUDefault        *int         `json:"mtu_default"`
	KeepaliveDefault  *int         `json:"keepalive_default"`
	MemberCount       int          `json:"member_count"`
	LinkCount         int          `json:"link_count"`
	Organization      string       `json:"organization,omitempty"`
	CreatedAt         FlexibleTime `json:"created_at"`
	UpdatedAt         FlexibleTime `json:"updated_at"`
}

// VpnNetworkListResponse represents the paginated list of VPN networks
type VpnNetworkListResponse struct {
	Items   []VpnNetwork `json:"items"`
	Total   int          `json:"total"`
	Page    int          `json:"page"`
	PerPage int          `json:"per_page"`
	Quota   *Quota       `json:"quota,omitempty"`
}

// VpnMember represents a VPN network member
type VpnMember struct {
	VpnNetwork   string       `json:"vpn_network"`
	DeviceName   string       `json:"device_name"`
	Role         string       `json:"role"`
	Enabled      bool         `json:"enabled"`
	WgPublicKey  string       `json:"wg_public_key"`
	OverlayIPv4  string       `json:"overlay_ip_v4"`
	EndpointHost *string      `json:"endpoint_host"`
	EndpointPort *int         `json:"endpoint_port"`
	ListenPort   *int         `json:"listen_port"`
	MTU          *int         `json:"mtu"`
	Keepalive    *int         `json:"keepalive"`
	TransitViaHub *string     `json:"transit_via_hub"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
}

// VpnMemberListResponse represents the paginated list of VPN members
type VpnMemberListResponse struct {
	Items   []VpnMember `json:"items"`
	Total   int         `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
}

// VpnLink represents a VPN link between two members
type VpnLink struct {
	VpnNetwork  string       `json:"vpn_network"`
	DeviceAName string       `json:"device_a_name"`
	DeviceBName string       `json:"device_b_name"`
	Enabled     bool         `json:"enabled"`
	HasPSK      bool         `json:"has_psk"`
	CreatedAt   FlexibleTime `json:"created_at"`
	UpdatedAt   FlexibleTime `json:"updated_at"`
}

// VpnLinkListResponse represents the paginated list of VPN links
type VpnLinkListResponse struct {
	Items   []VpnLink `json:"items"`
	Total   int       `json:"total"`
	Page    int       `json:"page"`
	PerPage int       `json:"per_page"`
}

// VpnMemberPrefix represents a published prefix on a VPN member
type VpnMemberPrefix struct {
	VpnNetwork   string       `json:"vpn_network"`
	DeviceName   string       `json:"device_name"`
	VariableName string       `json:"variable_name"`
	Publish      bool         `json:"publish"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
}

// VpnMemberPrefixListResponse represents the paginated list of VPN member prefixes
type VpnMemberPrefixListResponse struct {
	Items   []VpnMemberPrefix `json:"items"`
	Total   int               `json:"total"`
	Page    int               `json:"page"`
	PerPage int               `json:"per_page"`
}

// EffectiveConnection represents a computed VPN connection between two devices
type EffectiveConnection struct {
	DeviceA     string `json:"device_a"`
	DeviceB     string `json:"device_b"`
	RoleA       string `json:"role_a"`
	RoleB       string `json:"role_b"`
	PairType    string `json:"pair_type"`
	Source      string `json:"source"`
	Active      bool   `json:"active"`
	HasOverride bool   `json:"has_override"`
	HasPSK      bool   `json:"has_psk"`
	VpnNetwork  string `json:"vpn_network"`
}

// VpnDeleteResponse represents a delete operation response
type VpnDeleteResponse struct {
	Message string `json:"message"`
}
