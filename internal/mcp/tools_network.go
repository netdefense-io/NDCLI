package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// =============================================================================
// Input types
// =============================================================================

type netListInput struct {
	Organization string `json:"organization,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}
type netNameInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Confirm      bool   `json:"confirm,omitempty"`
}
type netCreateInput struct {
	Organization      string `json:"organization,omitempty"`
	Name              string `json:"name"`
	CIDR              string `json:"cidr"`
	AutoConnectHubs   *bool  `json:"auto_connect_hubs,omitempty"`
	AutoFirewallRules *bool  `json:"auto_firewall_rules,omitempty"`
	ListenPort        *int   `json:"listen_port,omitempty"`
	MTU               *int   `json:"mtu,omitempty"`
	Keepalive         *int   `json:"keepalive,omitempty"`
}
type netUpdateInput struct {
	Organization      string  `json:"organization,omitempty"`
	Network           string  `json:"network"`
	NewName           *string `json:"new_name,omitempty"`
	AutoConnectHubs   *bool   `json:"auto_connect_hubs,omitempty"`
	AutoFirewallRules *bool   `json:"auto_firewall_rules,omitempty"`
	ListenPort        *int    `json:"listen_port,omitempty"`
	MTU               *int    `json:"mtu,omitempty"`
	ClearMTU          bool    `json:"clear_mtu,omitempty"`
	Keepalive         *int    `json:"keepalive,omitempty"`
	ClearKeepalive    bool    `json:"clear_keepalive,omitempty"`
	Confirm           bool    `json:"confirm,omitempty"`
}
type netMemberListInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}
type netMemberKeyInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Device       string `json:"device"`
	Confirm      bool   `json:"confirm,omitempty"`
}
type netMemberAddInput struct {
	Organization  string `json:"organization,omitempty"`
	Network       string `json:"network"`
	Device        string `json:"device"`
	Role          string `json:"role,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
	OverlayIPv4   string `json:"overlay_ip_v4,omitempty"`
	EndpointHost  string `json:"endpoint_host,omitempty"`
	EndpointPort  int    `json:"endpoint_port,omitempty"`
	ListenPort    int    `json:"listen_port,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
	Keepalive     int    `json:"keepalive,omitempty"`
	TransitViaHub string `json:"transit_via_hub,omitempty"`
}
type netMemberUpdateInput struct {
	Organization      string  `json:"organization,omitempty"`
	Network           string  `json:"network"`
	Device            string  `json:"device"`
	Role              *string `json:"role,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
	EndpointHost      *string `json:"endpoint_host,omitempty"`
	ClearEndpointHost bool    `json:"clear_endpoint_host,omitempty"`
	EndpointPort      *int    `json:"endpoint_port,omitempty"`
	ClearEndpointPort bool    `json:"clear_endpoint_port,omitempty"`
	ListenPort        *int    `json:"listen_port,omitempty"`
	ClearListenPort   bool    `json:"clear_listen_port,omitempty"`
	MTU               *int    `json:"mtu,omitempty"`
	ClearMTU          bool    `json:"clear_mtu,omitempty"`
	Keepalive         *int    `json:"keepalive,omitempty"`
	ClearKeepalive    bool    `json:"clear_keepalive,omitempty"`
	TransitViaHub     *string `json:"transit_via_hub,omitempty"`
	ClearTransit      bool    `json:"clear_transit_via_hub,omitempty"`
	RegenerateKeys    *bool   `json:"regenerate_keys,omitempty"`
	Confirm           bool    `json:"confirm,omitempty"`
}
type netLinkListInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Raw          bool   `json:"raw,omitempty"`
	Device       string `json:"device,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}
type netLinkKeyInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	DeviceA      string `json:"device_a"`
	DeviceB      string `json:"device_b"`
	Confirm      bool   `json:"confirm,omitempty"`
}
type netLinkCreateInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	DeviceA      string `json:"device_a"`
	DeviceB      string `json:"device_b"`
	Enabled      *bool  `json:"enabled,omitempty"`
	GeneratePSK  *bool  `json:"generate_psk,omitempty"`
}
type netLinkUpdateInput struct {
	Organization  string `json:"organization,omitempty"`
	Network       string `json:"network"`
	DeviceA       string `json:"device_a"`
	DeviceB       string `json:"device_b"`
	Enabled       *bool  `json:"enabled,omitempty"`
	RegeneratePSK *bool  `json:"regenerate_psk,omitempty"`
	Confirm       bool   `json:"confirm,omitempty"`
}
type netPrefixListInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Device       string `json:"device"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}
type netPrefixKeyInput struct {
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network"`
	Device       string `json:"device"`
	Variable     string `json:"variable"`
	Publish      *bool  `json:"publish,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// applyClear maps a (value, clear) pair back to the service-layer pointer
// convention: nil = no change, ptr to zero = clear, ptr to value = set.
func applyClearString(value *string, clear bool) *string {
	if clear {
		empty := ""
		return &empty
	}
	return value
}
func applyClearInt(value *int, clear bool) *int {
	if clear {
		zero := 0
		return &zero
	}
	return value
}

// =============================================================================
// Registration
// =============================================================================

// registerNetworkTools registers every VPN network/member/link/prefix tool.
func (s *Server) registerNetworkTools() {
	// --- Networks ---
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.list",
		Description: "List VPN networks in an organization.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 30),
			},
		},
	}, s.handleNetworkList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.describe",
		Description: "Get a VPN network's details.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
			},
			"required": []string{"network"},
		},
	}, s.handleNetworkDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.create",
		Description: "Create a VPN network.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":         organizationProperty(),
				"name":                 stringProperty("VPN network name"),
				"cidr":                 stringProperty("Overlay IPv4 CIDR (e.g. 10.100.0.0/24)"),
				"auto_connect_hubs":    boolProperty("Auto-create links between HUB members"),
				"auto_firewall_rules":  boolProperty("Auto-generate OPNsense pass rules on the wireguard interface group"),
				"listen_port":          intProperty("Default WireGuard listen port (default 51820)", 0),
				"mtu":                  intProperty("Default MTU (1280-9000)", 0),
				"keepalive":            intProperty("Default keepalive interval (1-65535)", 0),
			},
			"required": []string{"name", "cidr"},
		},
	}, s.handleNetworkCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.update",
		Description: "Update a VPN network. Use clear_mtu / clear_keepalive to set those fields back to NULL on the server. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":         organizationProperty(),
				"network":              stringProperty("VPN network name"),
				"new_name":             stringProperty("Rename to"),
				"auto_connect_hubs":    boolProperty("Toggle auto-connect-hubs"),
				"auto_firewall_rules":  boolProperty("Toggle auto-firewall-rules"),
				"listen_port":          intProperty("Default WireGuard listen port", 0),
				"mtu":                  intProperty("Default MTU", 0),
				"clear_mtu":            boolProperty("Clear default MTU (sets server NULL)"),
				"keepalive":            intProperty("Default keepalive interval", 0),
				"clear_keepalive":      boolProperty("Clear default keepalive (sets server NULL)"),
				"confirm":              confirmProperty(),
			},
			"required": []string{"network"},
		},
	}, s.handleNetworkUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.delete",
		Description: "Delete a VPN network. Requires confirm=true. Removes all members, links, and prefixes.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"network"},
		},
	}, s.handleNetworkDelete)

	// --- Members ---
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.member_list",
		Description: "List members of a VPN network.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 30),
			},
			"required": []string{"network"},
		},
	}, s.handleNetworkMemberList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.member_describe",
		Description: "Get a VPN member's details (including endpoint, keys, transit hub).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
			},
			"required": []string{"network", "device"},
		},
	}, s.handleNetworkMemberDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.member_add",
		Description: "Attach a device to a VPN network as a HUB or SPOKE.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":     organizationProperty(),
				"network":          stringProperty("VPN network name"),
				"device":           stringProperty("Device name"),
				"role":             stringEnumProperty("Member role (default SPOKE)", []string{"HUB", "SPOKE"}),
				"enabled":          boolProperty("Whether the member is enabled"),
				"overlay_ip_v4":    stringProperty("Overlay IPv4 (auto-allocated if empty)"),
				"endpoint_host":    stringProperty("Public hostname/IP"),
				"endpoint_port":    intProperty("Public endpoint port", 0),
				"listen_port":      intProperty("WireGuard listen port override", 0),
				"mtu":              intProperty("MTU override", 0),
				"keepalive":        intProperty("Keepalive interval override", 0),
				"transit_via_hub":  stringProperty("HUB device name to route through"),
			},
			"required": []string{"network", "device"},
		},
	}, s.handleNetworkMemberAdd)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.member_update",
		Description: "Update a VPN member. Use clear_<field> booleans to set string/int fields back to NULL on the server (e.g. clear endpoint_host, transit_via_hub). Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":            organizationProperty(),
				"network":                 stringProperty("VPN network name"),
				"device":                  stringProperty("Device name"),
				"role":                    stringEnumProperty("New role", []string{"HUB", "SPOKE"}),
				"enabled":                 boolProperty("Enable/disable the member"),
				"endpoint_host":           stringProperty("Set public hostname/IP"),
				"clear_endpoint_host":     boolProperty("Clear public hostname/IP"),
				"endpoint_port":           intProperty("Set public endpoint port", 0),
				"clear_endpoint_port":     boolProperty("Clear public endpoint port"),
				"listen_port":             intProperty("Set WireGuard listen port", 0),
				"clear_listen_port":       boolProperty("Clear WireGuard listen port"),
				"mtu":                     intProperty("Set MTU", 0),
				"clear_mtu":               boolProperty("Clear MTU"),
				"keepalive":               intProperty("Set keepalive", 0),
				"clear_keepalive":         boolProperty("Clear keepalive"),
				"transit_via_hub":         stringProperty("Set transit-via HUB"),
				"clear_transit_via_hub":   boolProperty("Clear transit-via HUB"),
				"regenerate_keys":         boolProperty("Regenerate WireGuard keypair"),
				"confirm":                 confirmProperty(),
			},
			"required": []string{"network", "device"},
		},
	}, s.handleNetworkMemberUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.member_remove",
		Description: "Remove a member from a VPN network. Requires confirm=true. Hub removal disconnects every spoke that depended on it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"network", "device"},
		},
	}, s.handleNetworkMemberRemove)

	// --- Links ---
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.link_list",
		Description: "List effective VPN connections (implicit hub-spoke + hub-hub + explicit links). Set raw=true to return only the link database rows; otherwise returns the computed connection view, optionally filtered to a single device.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"raw":          boolProperty("Return raw link rows instead of effective connections"),
				"device":       stringProperty("Filter effective connections to those involving this device"),
				"page":         intProperty("Page number (raw mode only)", 1),
				"per_page":     intProperty("Items per page (raw mode only)", 30),
			},
			"required": []string{"network"},
		},
	}, s.handleNetworkLinkList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.link_create",
		Description: "Create a VPN link / override between two members. For implicit hub-spoke pairs this acts as an override (e.g. to disable the auto connection or attach a PSK).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device_a":     stringProperty("Device A name"),
				"device_b":     stringProperty("Device B name"),
				"enabled":      boolProperty("Whether the link is enabled (default true)"),
				"generate_psk": boolProperty("Generate a WireGuard pre-shared key"),
			},
			"required": []string{"network", "device_a", "device_b"},
		},
	}, s.handleNetworkLinkCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.link_describe",
		Description: "Describe an effective VPN connection between two devices. Works for both explicit links and implicit hub/spoke pairs (no row in the link table).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device_a":     stringProperty("Device A name"),
				"device_b":     stringProperty("Device B name"),
			},
			"required": []string{"network", "device_a", "device_b"},
		},
	}, s.handleNetworkLinkDescribe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.link_update",
		Description: "Update an explicit VPN link. Requires confirm=true. (For implicit pairs without an existing override, use link_create instead.)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":   organizationProperty(),
				"network":        stringProperty("VPN network name"),
				"device_a":       stringProperty("Device A name"),
				"device_b":       stringProperty("Device B name"),
				"enabled":        boolProperty("Enable/disable the link"),
				"regenerate_psk": boolProperty("Regenerate the pre-shared key"),
				"confirm":        confirmProperty(),
			},
			"required": []string{"network", "device_a", "device_b"},
		},
	}, s.handleNetworkLinkUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.network.link_delete",
		Description: "Delete a VPN link. For explicit links this disconnects the pair; for implicit overrides this restores the automatic connection. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device_a":     stringProperty("Device A name"),
				"device_b":     stringProperty("Device B name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"network", "device_a", "device_b"},
		},
	}, s.handleNetworkLinkDelete)

	// --- Prefixes ---
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.prefix_list",
		Description: "List the prefixes a VPN member is publishing.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
				"page":         intProperty("Page number", 1),
				"per_page":     intProperty("Items per page", 30),
			},
			"required": []string{"network", "device"},
		},
	}, s.handleNetworkPrefixList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.prefix_add",
		Description: "Publish a prefix (named variable) on a VPN member.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
				"variable":     stringProperty("Variable name to publish"),
				"publish":      boolProperty("Whether to advertise the prefix to peers (default true)"),
			},
			"required": []string{"network", "device", "variable"},
		},
	}, s.handleNetworkPrefixAdd)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.prefix_update",
		Description: "Toggle the publish flag on a member prefix. Requires both publish and confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
				"variable":     stringProperty("Variable name"),
				"publish":      boolProperty("Whether to advertise the prefix to peers"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"network", "device", "variable", "publish"},
		},
	}, s.handleNetworkPrefixUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.network.prefix_remove",
		Description: "Remove a published prefix from a member. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"network":      stringProperty("VPN network name"),
				"device":       stringProperty("Device name"),
				"variable":     stringProperty("Variable name"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"network", "device", "variable"},
		},
	}, s.handleNetworkPrefixRemove)
}

// =============================================================================
// Handlers
// =============================================================================

func (s *Server) handleNetworkList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.NetworkList(apiCtx, org, input.Page, input.PerPage)
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Networks))
	for _, n := range result.Networks {
		items = append(items, networkSummary(&n))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"networks": items,
		"quota":    result.Quota,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleNetworkDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	n, err := s.svc.NetworkGet(apiCtx, org, input.Network)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"network": networkSummary(n)}, "")
}

func (s *Server) handleNetworkCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	opts := service.NetworkCreateOpts{
		Name:              input.Name,
		OverlayCIDRv4:     input.CIDR,
		AutoConnectHubs:   input.AutoConnectHubs,
		AutoFirewallRules: input.AutoFirewallRules,
		ListenPortDefault: input.ListenPort,
		MTUDefault:        input.MTU,
		KeepaliveDefault:  input.Keepalive,
	}
	n, err := s.svc.NetworkCreate(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network": networkSummary(n),
		"action":  "created",
	}, fmt.Sprintf("VPN network '%s' created", input.Name))
}

func (s *Server) handleNetworkUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netUpdateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update VPN network", input.Network)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	opts := service.NetworkUpdateOpts{
		Name:              input.NewName,
		AutoConnectHubs:   input.AutoConnectHubs,
		AutoFirewallRules: input.AutoFirewallRules,
		ListenPortDefault: input.ListenPort,
		MTUDefault:        applyClearInt(input.MTU, input.ClearMTU),
		KeepaliveDefault:  applyClearInt(input.Keepalive, input.ClearKeepalive),
	}
	n, err := s.svc.NetworkUpdate(apiCtx, org, input.Network, opts)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network": networkSummary(n),
		"action":  "updated",
	}, fmt.Sprintf("VPN network '%s' updated", n.Name))
}

func (s *Server) handleNetworkDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netNameInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete VPN network", input.Network)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.NetworkDelete(apiCtx, org, input.Network); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network": input.Network,
		"action":  "deleted",
	}, fmt.Sprintf("VPN network '%s' deleted", input.Network))
}

func (s *Server) handleNetworkMemberList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netMemberListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.NetworkMemberList(apiCtx, org, input.Network, input.Page, input.PerPage)
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Members))
	for _, m := range result.Members {
		items = append(items, vpnMemberSummary(&m))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"network": input.Network,
		"members": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleNetworkMemberDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netMemberKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	m, err := s.svc.NetworkMemberGet(apiCtx, org, input.Network, input.Device)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"member": vpnMemberFull(m),
	}, "")
}

func (s *Server) handleNetworkMemberAdd(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netMemberAddInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	m, err := s.svc.NetworkMemberAdd(apiCtx, org, input.Network, input.Device, service.NetworkMemberAddOpts{
		Role:          input.Role,
		Enabled:       input.Enabled,
		OverlayIPv4:   input.OverlayIPv4,
		EndpointHost:  input.EndpointHost,
		EndpointPort:  input.EndpointPort,
		ListenPort:    input.ListenPort,
		MTU:           input.MTU,
		Keepalive:     input.Keepalive,
		TransitViaHub: input.TransitViaHub,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"member": vpnMemberSummary(m),
		"action": "added",
	}, fmt.Sprintf("Member '%s' added to '%s'", input.Device, input.Network))
}

func (s *Server) handleNetworkMemberUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netMemberUpdateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update VPN member", fmt.Sprintf("%s in %s", input.Device, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	opts := service.NetworkMemberUpdateOpts{
		Role:           input.Role,
		Enabled:        input.Enabled,
		EndpointHost:   applyClearString(input.EndpointHost, input.ClearEndpointHost),
		EndpointPort:   applyClearInt(input.EndpointPort, input.ClearEndpointPort),
		ListenPort:     applyClearInt(input.ListenPort, input.ClearListenPort),
		MTU:            applyClearInt(input.MTU, input.ClearMTU),
		Keepalive:      applyClearInt(input.Keepalive, input.ClearKeepalive),
		TransitViaHub:  applyClearString(input.TransitViaHub, input.ClearTransit),
		RegenerateKeys: input.RegenerateKeys,
	}
	m, err := s.svc.NetworkMemberUpdate(apiCtx, org, input.Network, input.Device, opts)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"member": vpnMemberFull(m),
		"action": "updated",
	}, fmt.Sprintf("Member '%s' in '%s' updated", input.Device, input.Network))
}

func (s *Server) handleNetworkMemberRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netMemberKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove VPN member", fmt.Sprintf("%s from %s", input.Device, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.NetworkMemberRemove(apiCtx, org, input.Network, input.Device); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network": input.Network,
		"device":  input.Device,
		"action":  "removed",
	}, fmt.Sprintf("Member '%s' removed from '%s'", input.Device, input.Network))
}

func (s *Server) handleNetworkLinkList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netLinkListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if input.Raw {
		result, err := s.svc.NetworkLinkListRaw(apiCtx, org, input.Network, input.Page, input.PerPage)
		if err != nil {
			return s.errorResult(err)
		}
		items := make([]map[string]interface{}, 0, len(result.Links))
		for _, l := range result.Links {
			items = append(items, vpnLinkSummary(&l))
		}
		return s.successResultWithPagination(map[string]interface{}{
			"network": input.Network,
			"links":   items,
		}, result.Page, result.PerPage, result.Total)
	}

	connections, err := s.svc.NetworkLinkListEffective(apiCtx, org, input.Network, input.Device)
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(connections))
	automatic, manualLinks, overrides := 0, 0, 0
	for _, c := range connections {
		items = append(items, vpnConnectionSummary(&c))
		if c.Source == "implicit" {
			automatic++
			if c.HasOverride {
				overrides++
			}
		} else {
			manualLinks++
		}
	}
	return s.successResult(map[string]interface{}{
		"network":     input.Network,
		"connections": items,
		"summary": map[string]interface{}{
			"total":     len(connections),
			"automatic": automatic,
			"manual":    manualLinks,
			"overrides": overrides,
		},
	}, "")
}

func (s *Server) handleNetworkLinkCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netLinkCreateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	link, err := s.svc.NetworkLinkCreate(apiCtx, org, input.Network, input.DeviceA, input.DeviceB, service.NetworkLinkCreateOpts{
		Enabled:     input.Enabled,
		GeneratePSK: input.GeneratePSK,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"link":   vpnLinkSummary(link),
		"action": "created",
	}, fmt.Sprintf("Link %s ↔ %s created in %s", input.DeviceA, input.DeviceB, input.Network))
}

func (s *Server) handleNetworkLinkDescribe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netLinkKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	link, err := s.svc.NetworkLinkGet(apiCtx, org, input.Network, input.DeviceA, input.DeviceB)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"link": vpnLinkSummary(link),
	}, "")
}

func (s *Server) handleNetworkLinkUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netLinkUpdateInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update VPN link", fmt.Sprintf("%s ↔ %s in %s", input.DeviceA, input.DeviceB, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	link, err := s.svc.NetworkLinkUpdate(apiCtx, org, input.Network, input.DeviceA, input.DeviceB, service.NetworkLinkUpdateOpts{
		Enabled:       input.Enabled,
		RegeneratePSK: input.RegeneratePSK,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"link":   vpnLinkSummary(link),
		"action": "updated",
	}, fmt.Sprintf("Link %s ↔ %s in %s updated", input.DeviceA, input.DeviceB, input.Network))
}

func (s *Server) handleNetworkLinkDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netLinkKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("delete VPN link", fmt.Sprintf("%s ↔ %s in %s", input.DeviceA, input.DeviceB, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.NetworkLinkDelete(apiCtx, org, input.Network, input.DeviceA, input.DeviceB); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network":  input.Network,
		"device_a": input.DeviceA,
		"device_b": input.DeviceB,
		"action":   "deleted",
	}, fmt.Sprintf("Link %s ↔ %s in %s deleted", input.DeviceA, input.DeviceB, input.Network))
}

func (s *Server) handleNetworkPrefixList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netPrefixListInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.NetworkPrefixList(apiCtx, org, input.Network, input.Device, input.Page, input.PerPage)
	if err != nil {
		return s.errorResult(err)
	}
	items := make([]map[string]interface{}, 0, len(result.Prefixes))
	for _, p := range result.Prefixes {
		items = append(items, vpnPrefixSummary(&p))
	}
	return s.successResultWithPagination(map[string]interface{}{
		"network":  input.Network,
		"device":   input.Device,
		"prefixes": items,
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleNetworkPrefixAdd(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netPrefixKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	p, err := s.svc.NetworkPrefixAdd(apiCtx, org, input.Network, input.Device, input.Variable, input.Publish)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"prefix": vpnPrefixSummary(p),
		"action": "added",
	}, fmt.Sprintf("Prefix '%s' added to %s in %s", input.Variable, input.Device, input.Network))
}

func (s *Server) handleNetworkPrefixUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netPrefixKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("update VPN prefix", fmt.Sprintf("%s on %s in %s", input.Variable, input.Device, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	p, err := s.svc.NetworkPrefixUpdate(apiCtx, org, input.Network, input.Device, input.Variable, input.Publish)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"prefix": vpnPrefixSummary(p),
		"action": "updated",
	}, fmt.Sprintf("Prefix '%s' on %s in %s updated", input.Variable, input.Device, input.Network))
}

func (s *Server) handleNetworkPrefixRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[netPrefixKeyInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	if !input.Confirm {
		return s.previewResult("remove VPN prefix", fmt.Sprintf("%s from %s in %s", input.Variable, input.Device, input.Network))
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.NetworkPrefixRemove(apiCtx, org, input.Network, input.Device, input.Variable); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"network":  input.Network,
		"device":   input.Device,
		"variable": input.Variable,
		"action":   "removed",
	}, fmt.Sprintf("Prefix '%s' removed from %s in %s", input.Variable, input.Device, input.Network))
}

// =============================================================================
// Summaries
// =============================================================================

func networkSummary(n *models.VpnNetwork) map[string]interface{} {
	return map[string]interface{}{
		"name":                n.Name,
		"overlay_cidr_v4":     n.OverlayCIDRv4,
		"auto_connect_hubs":   n.AutoConnectHubs,
		"auto_firewall_rules": n.AutoFirewallRules,
		"listen_port_default": n.ListenPortDefault,
		"mtu_default":         n.MTUDefault,
		"keepalive_default":   n.KeepaliveDefault,
		"member_count":        n.MemberCount,
		"link_count":          n.LinkCount,
		"organization":        n.Organization,
		"created_at":          n.CreatedAt,
		"updated_at":          n.UpdatedAt,
	}
}

func vpnMemberSummary(m *models.VpnMember) map[string]interface{} {
	return map[string]interface{}{
		"vpn_network":   m.VpnNetwork,
		"device_name":   m.DeviceName,
		"role":          m.Role,
		"enabled":       m.Enabled,
		"overlay_ip_v4": m.OverlayIPv4,
		"endpoint_host": m.EndpointHost,
		"endpoint_port": m.EndpointPort,
	}
}

func vpnMemberFull(m *models.VpnMember) map[string]interface{} {
	full := vpnMemberSummary(m)
	full["wg_public_key"] = m.WgPublicKey
	full["listen_port"] = m.ListenPort
	full["mtu"] = m.MTU
	full["keepalive"] = m.Keepalive
	full["transit_via_hub"] = m.TransitViaHub
	full["created_at"] = m.CreatedAt
	full["updated_at"] = m.UpdatedAt
	return full
}

func vpnLinkSummary(l *models.VpnLink) map[string]interface{} {
	return map[string]interface{}{
		"vpn_network":   l.VpnNetwork,
		"device_a_name": l.DeviceAName,
		"device_b_name": l.DeviceBName,
		"enabled":       l.Enabled,
		"has_psk":       l.HasPSK,
		"created_at":    l.CreatedAt,
		"updated_at":    l.UpdatedAt,
	}
}

func vpnConnectionSummary(c *models.EffectiveConnection) map[string]interface{} {
	return map[string]interface{}{
		"device_a":     c.DeviceA,
		"device_b":     c.DeviceB,
		"role_a":       c.RoleA,
		"role_b":       c.RoleB,
		"pair_type":    c.PairType,
		"source":       c.Source,
		"active":       c.Active,
		"has_override": c.HasOverride,
		"has_psk":      c.HasPSK,
		"vpn_network":  c.VpnNetwork,
	}
}

func vpnPrefixSummary(p *models.VpnMemberPrefix) map[string]interface{} {
	return map[string]interface{}{
		"vpn_network":   p.VpnNetwork,
		"device_name":   p.DeviceName,
		"variable_name": p.VariableName,
		"publish":       p.Publish,
		"created_at":    p.CreatedAt,
		"updated_at":    p.UpdatedAt,
	}
}
