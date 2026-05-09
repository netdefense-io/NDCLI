package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/vpn"
)

// =============================================================================
// VPN networks
// =============================================================================

// NetworkListResult mirrors the paginated VPN-network list with resolved
// pagination defaults.
type NetworkListResult struct {
	Networks []models.VpnNetwork
	Total    int
	Page     int
	PerPage  int
	Quota    *models.Quota
}

// NetworkList returns a paginated list of VPN networks.
func (s *Service) NetworkList(ctx context.Context, org string, page, perPage int) (*NetworkListResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks", url.PathEscape(org)), map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VpnNetworkListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &NetworkListResult{
		Networks: result.Items,
		Total:    result.Total,
		Page:     page,
		PerPage:  perPage,
		Quota:    result.Quota,
	}, nil
}

// NetworkGet returns a single VPN network.
func (s *Service) NetworkGet(ctx context.Context, org, name string) (*models.VpnNetwork, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(name)), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var n models.VpnNetwork
	if err := api.ParseResponse(resp, &n); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &n, nil
}

// NetworkCreateOpts holds the fields needed to create a VPN network.
//
// Pointer fields are optional: nil means "use server default". Set them to
// take effect.
type NetworkCreateOpts struct {
	Name              string
	OverlayCIDRv4     string
	AutoConnectHubs   *bool
	AutoFirewallRules *bool
	ListenPortDefault *int
	MTUDefault        *int
	KeepaliveDefault  *int
}

// NetworkCreate creates a new VPN network.
func (s *Service) NetworkCreate(ctx context.Context, org string, opts NetworkCreateOpts) (*models.VpnNetwork, error) {
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network name is required"}
	}
	if opts.OverlayCIDRv4 == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "overlay CIDR is required (e.g. 10.100.0.0/24)"}
	}
	payload := map[string]interface{}{
		"name":            opts.Name,
		"overlay_cidr_v4": opts.OverlayCIDRv4,
	}
	if opts.AutoConnectHubs != nil {
		payload["auto_connect_hubs"] = *opts.AutoConnectHubs
	}
	if opts.AutoFirewallRules != nil {
		payload["auto_firewall_rules"] = *opts.AutoFirewallRules
	}
	if opts.ListenPortDefault != nil {
		payload["listen_port_default"] = *opts.ListenPortDefault
	}
	if opts.MTUDefault != nil {
		payload["mtu_default"] = *opts.MTUDefault
	}
	if opts.KeepaliveDefault != nil {
		payload["keepalive_default"] = *opts.KeepaliveDefault
	}

	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks", url.PathEscape(org)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var n models.VpnNetwork
	if err := api.ParseResponse(resp, &n); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &n, nil
}

// NetworkUpdateOpts collects the patch-able fields of a VPN network.
//
// Convention for each *T pointer: nil = leave field alone; non-nil = include
// in PATCH payload. For ints, the server treats 0 as "clear" (sets the
// underlying column to NULL); the same applies here when *MTUDefault==0.
type NetworkUpdateOpts struct {
	Name              *string
	AutoConnectHubs   *bool
	AutoFirewallRules *bool
	ListenPortDefault *int
	MTUDefault        *int // pointer to 0 → clear (server NULL)
	KeepaliveDefault  *int // pointer to 0 → clear (server NULL)
}

// NetworkUpdate patches a VPN network.
func (s *Service) NetworkUpdate(ctx context.Context, org, name string, opts NetworkUpdateOpts) (*models.VpnNetwork, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network name is required"}
	}
	payload := map[string]interface{}{}
	if opts.Name != nil {
		payload["name"] = *opts.Name
	}
	if opts.AutoConnectHubs != nil {
		payload["auto_connect_hubs"] = *opts.AutoConnectHubs
	}
	if opts.AutoFirewallRules != nil {
		payload["auto_firewall_rules"] = *opts.AutoFirewallRules
	}
	if opts.ListenPortDefault != nil {
		payload["listen_port_default"] = *opts.ListenPortDefault
	}
	if opts.MTUDefault != nil {
		if *opts.MTUDefault == 0 {
			payload["mtu_default"] = nil
		} else {
			payload["mtu_default"] = *opts.MTUDefault
		}
	}
	if opts.KeepaliveDefault != nil {
		if *opts.KeepaliveDefault == 0 {
			payload["keepalive_default"] = nil
		} else {
			payload["keepalive_default"] = *opts.KeepaliveDefault
		}
	}
	if len(payload) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "no update fields provided"}
	}

	resp, err := s.api.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(name)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var n models.VpnNetwork
	if err := api.ParseResponse(resp, &n); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &n, nil
}

// NetworkDelete removes a VPN network.
func (s *Service) NetworkDelete(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "vpn network name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s", url.PathEscape(org), url.PathEscape(name)))
	if err != nil {
		return wrapAPI("%v", err)
	}
	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// =============================================================================
// VPN members
// =============================================================================

// MemberListResult mirrors the paginated member list.
type MemberListResult struct {
	Members []models.VpnMember
	Total   int
	Page    int
	PerPage int
}

// NetworkMemberList returns a paginated list of members in a VPN network.
func (s *Service) NetworkMemberList(ctx context.Context, org, vpnName string, page, perPage int) (*MemberListResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members",
		url.PathEscape(org), url.PathEscape(vpnName)), map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VpnMemberListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &MemberListResult{Members: result.Items, Total: result.Total, Page: page, PerPage: perPage}, nil
}

// NetworkMemberGet returns a single member.
func (s *Service) NetworkMemberGet(ctx context.Context, org, vpnName, deviceName string) (*models.VpnMember, error) {
	if vpnName == "" || deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network and device names are required"}
	}
	return vpn.FetchMember(ctx, s.api, org, vpnName, deviceName)
}

// NetworkMemberAddOpts holds the fields for adding a member.
type NetworkMemberAddOpts struct {
	Role          string
	Enabled       *bool
	OverlayIPv4   string
	EndpointHost  string
	EndpointPort  int
	ListenPort    int
	MTU           int
	Keepalive     int
	TransitViaHub string
}

// NetworkMemberAdd adds a device to a VPN network.
func (s *Service) NetworkMemberAdd(ctx context.Context, org, vpnName, deviceName string, opts NetworkMemberAddOpts) (*models.VpnMember, error) {
	if vpnName == "" || deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network and device names are required"}
	}
	payload := map[string]interface{}{"device_name": deviceName}
	if opts.Role != "" {
		payload["role"] = opts.Role
	}
	if opts.Enabled != nil {
		payload["enabled"] = *opts.Enabled
	}
	if opts.OverlayIPv4 != "" {
		payload["overlay_ip_v4"] = opts.OverlayIPv4
	}
	if opts.EndpointHost != "" {
		payload["endpoint_host"] = opts.EndpointHost
	}
	if opts.EndpointPort > 0 {
		payload["endpoint_port"] = opts.EndpointPort
	}
	if opts.ListenPort > 0 {
		payload["listen_port"] = opts.ListenPort
	}
	if opts.MTU > 0 {
		payload["mtu"] = opts.MTU
	}
	if opts.Keepalive > 0 {
		payload["keepalive"] = opts.Keepalive
	}
	if opts.TransitViaHub != "" {
		payload["transit_via_hub"] = opts.TransitViaHub
	}

	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members",
		url.PathEscape(org), url.PathEscape(vpnName)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var m models.VpnMember
	if err := api.ParseResponse(resp, &m); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &m, nil
}

// NetworkMemberUpdateOpts collects the patch-able member fields.
//
// String pointers: nil = no change, non-nil empty string = clear (server
// NULL), non-empty = set.
// Int pointers: nil = no change, ptr to 0 = clear, positive = set.
// Bool pointers: nil = no change.
type NetworkMemberUpdateOpts struct {
	Role           *string
	Enabled        *bool
	EndpointHost   *string
	EndpointPort   *int
	ListenPort     *int
	MTU            *int
	Keepalive      *int
	TransitViaHub  *string
	RegenerateKeys *bool
}

// NetworkMemberUpdate patches a member.
func (s *Service) NetworkMemberUpdate(ctx context.Context, org, vpnName, deviceName string, opts NetworkMemberUpdateOpts) (*models.VpnMember, error) {
	if vpnName == "" || deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network and device names are required"}
	}
	payload := map[string]interface{}{}
	if opts.Role != nil {
		payload["role"] = *opts.Role
	}
	if opts.Enabled != nil {
		payload["enabled"] = *opts.Enabled
	}
	if opts.EndpointHost != nil {
		if *opts.EndpointHost == "" {
			payload["endpoint_host"] = nil
		} else {
			payload["endpoint_host"] = *opts.EndpointHost
		}
	}
	if opts.EndpointPort != nil {
		if *opts.EndpointPort == 0 {
			payload["endpoint_port"] = nil
		} else {
			payload["endpoint_port"] = *opts.EndpointPort
		}
	}
	if opts.ListenPort != nil {
		if *opts.ListenPort == 0 {
			payload["listen_port"] = nil
		} else {
			payload["listen_port"] = *opts.ListenPort
		}
	}
	if opts.MTU != nil {
		if *opts.MTU == 0 {
			payload["mtu"] = nil
		} else {
			payload["mtu"] = *opts.MTU
		}
	}
	if opts.Keepalive != nil {
		if *opts.Keepalive == 0 {
			payload["keepalive"] = nil
		} else {
			payload["keepalive"] = *opts.Keepalive
		}
	}
	if opts.TransitViaHub != nil {
		if *opts.TransitViaHub == "" {
			payload["transit_via_hub"] = nil
		} else {
			payload["transit_via_hub"] = *opts.TransitViaHub
		}
	}
	if opts.RegenerateKeys != nil {
		payload["regenerate_keys"] = *opts.RegenerateKeys
	}
	if len(payload) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "no update fields provided"}
	}

	resp, err := s.api.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var m models.VpnMember
	if err := api.ParseResponse(resp, &m); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &m, nil
}

// NetworkMemberRemove deletes a member from a VPN network.
func (s *Service) NetworkMemberRemove(ctx context.Context, org, vpnName, deviceName string) error {
	if vpnName == "" || deviceName == "" {
		return &Error{Code: CodeInvalidInput, Message: "vpn network and device names are required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)))
	if err != nil {
		return wrapAPI("%v", err)
	}
	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// =============================================================================
// VPN links
// =============================================================================

// LinkListResult mirrors the paginated raw-link list.
type LinkListResult struct {
	Links   []models.VpnLink
	Total   int
	Page    int
	PerPage int
}

// NetworkLinkListRaw returns the raw VPN link database rows (overrides/explicit
// links). Use NetworkLinkListEffective for the computed connection view that
// includes implicit hub-spoke / hub-hub pairs.
func (s *Service) NetworkLinkListRaw(ctx context.Context, org, vpnName string, page, perPage int) (*LinkListResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
		url.PathEscape(org), url.PathEscape(vpnName)), map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VpnLinkListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &LinkListResult{Links: result.Items, Total: result.Total, Page: page, PerPage: perPage}, nil
}

// NetworkLinkListEffective returns every effective connection in the VPN
// network (implicit + explicit). deviceFilter is optional.
func (s *Service) NetworkLinkListEffective(ctx context.Context, org, vpnName, deviceFilter string) ([]models.EffectiveConnection, error) {
	network, err := vpn.FetchNetwork(ctx, s.api, org, vpnName)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	members, err := vpn.FetchAllMembers(ctx, s.api, org, vpnName)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	links, err := vpn.FetchAllLinks(ctx, s.api, org, vpnName)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	connections := vpn.ComputeEffectiveConnections(network, members, links)
	if deviceFilter != "" {
		connections = vpn.FilterByDevice(connections, deviceFilter)
	}
	return connections, nil
}

// NetworkLinkCreateOpts holds the fields for creating a link override.
type NetworkLinkCreateOpts struct {
	Enabled     *bool
	GeneratePSK *bool
}

// NetworkLinkCreate creates an explicit link / override between two members.
func (s *Service) NetworkLinkCreate(ctx context.Context, org, vpnName, deviceA, deviceB string, opts NetworkLinkCreateOpts) (*models.VpnLink, error) {
	if vpnName == "" || deviceA == "" || deviceB == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network and both device names are required"}
	}
	payload := map[string]interface{}{
		"device_a_name": deviceA,
		"device_b_name": deviceB,
	}
	if opts.Enabled != nil {
		payload["enabled"] = *opts.Enabled
	}
	if opts.GeneratePSK != nil {
		payload["generate_psk"] = *opts.GeneratePSK
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
		url.PathEscape(org), url.PathEscape(vpnName)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var link models.VpnLink
	if err := api.ParseResponse(resp, &link); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &link, nil
}

// NetworkLinkGet returns a single explicit link by its endpoints. Not all
// effective connections have a backing link row; this returns nil and a
// CodeAPIError (NotFound) for purely-implicit pairs.
func (s *Service) NetworkLinkGet(ctx context.Context, org, vpnName, deviceA, deviceB string) (*models.VpnLink, error) {
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var link models.VpnLink
	if err := api.ParseResponse(resp, &link); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &link, nil
}

// NetworkLinkUpdateOpts collects the patch-able link fields.
type NetworkLinkUpdateOpts struct {
	Enabled       *bool
	RegeneratePSK *bool
}

// NetworkLinkUpdate patches a link override.
func (s *Service) NetworkLinkUpdate(ctx context.Context, org, vpnName, deviceA, deviceB string, opts NetworkLinkUpdateOpts) (*models.VpnLink, error) {
	payload := map[string]interface{}{}
	if opts.Enabled != nil {
		payload["enabled"] = *opts.Enabled
	}
	if opts.RegeneratePSK != nil {
		payload["regenerate_psk"] = *opts.RegeneratePSK
	}
	if len(payload) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "no update fields provided"}
	}
	resp, err := s.api.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var link models.VpnLink
	if err := api.ParseResponse(resp, &link); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &link, nil
}

// NetworkLinkDelete removes a link override.
func (s *Service) NetworkLinkDelete(ctx context.Context, org, vpnName, deviceA, deviceB string) error {
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links/%s/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceA), url.PathEscape(deviceB)))
	if err != nil {
		return wrapAPI("%v", err)
	}
	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// =============================================================================
// VPN member prefixes
// =============================================================================

// PrefixListResult mirrors the paginated prefix list.
type PrefixListResult struct {
	Prefixes []models.VpnMemberPrefix
	Total    int
	Page     int
	PerPage  int
}

// NetworkPrefixList returns the prefixes a member is publishing.
func (s *Service) NetworkPrefixList(ctx context.Context, org, vpnName, deviceName string, page, perPage int) (*PrefixListResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.VpnMemberPrefixListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &PrefixListResult{Prefixes: result.Items, Total: result.Total, Page: page, PerPage: perPage}, nil
}

// NetworkPrefixAdd publishes a prefix on a member. publish=nil defaults to
// true on the server.
func (s *Service) NetworkPrefixAdd(ctx context.Context, org, vpnName, deviceName, variableName string, publish *bool) (*models.VpnMemberPrefix, error) {
	if vpnName == "" || deviceName == "" || variableName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network, device, and variable names are required"}
	}
	payload := map[string]interface{}{"variable_name": variableName}
	if publish != nil {
		payload["publish"] = *publish
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var p models.VpnMemberPrefix
	if err := api.ParseResponse(resp, &p); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &p, nil
}

// NetworkPrefixUpdate updates a prefix's publish flag.
func (s *Service) NetworkPrefixUpdate(ctx context.Context, org, vpnName, deviceName, variableName string, publish *bool) (*models.VpnMemberPrefix, error) {
	if publish == nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "publish field must be provided"}
	}
	payload := map[string]interface{}{"publish": *publish}
	resp, err := s.api.Patch(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName), url.PathEscape(variableName)), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var p models.VpnMemberPrefix
	if err := api.ParseResponse(resp, &p); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &p, nil
}

// NetworkPrefixRemove unpublishes a prefix.
func (s *Service) NetworkPrefixRemove(ctx context.Context, org, vpnName, deviceName, variableName string) error {
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s/prefixes/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(deviceName), url.PathEscape(variableName)))
	if err != nil {
		return wrapAPI("%v", err)
	}
	var result models.VpnDeleteResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}
