package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// MCP parity for `ndcli device health` — see CLAUDE.md "MCP-parity policy".
// Read-only and cheap, so no confirm gate. (The org-level dashboard roll-up
// is no longer an MCP tool: it moved into the standalone `netdefense` TUI.)

type deviceHealthInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	Retained     bool   `json:"retained,omitempty"`
}

// deviceHealthOPNsense says what the OPNsense fields of ndcli.device.health
// hold.
const deviceHealthOPNsense = "opnsense_version is the installed OPNsense release and opnsense_version_source where it was read " +
	"(live telemetry, the device's facts, or last_known telemetry); opnsense_latest is the newest release of the installed series. " +
	"opnsense_update is minor (a newer release of the installed series), major (no newer release of it, but a newer series: " +
	"opnsense_newer_series), none (up to date) or unknown (cannot tell); opnsense_newer_series can be set beside minor too. " +
	"checked_at is when the device last completed a firmware check (Unix seconds)."

// deviceHealthRetained says what retained changes.
const deviceHealthRetained = "Without retained, snapshot is absent once the device has stopped sending heartbeats for a few minutes. " +
	"With retained=true the last-known snapshot is served instead and stale is true: it is not current, so say so; " +
	"snapshot_age_sec is its age and heavy_age_sec gives the age of each heavy block (services, updates, certs), absent when unknown. " +
	"online never comes from retained data. A server that predates retained telemetry is SERVER_TOO_OLD."

func (s *Server) registerDeviceHealthTool() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.device.health",
		Description: "Per-device drill-down with the full agent telemetry snapshot — uptime, load, mem/swap, disks per mountpoint, service running state, pending OS/plugin updates, certificate expiries. No history; this is the instant snapshot. " +
			"The device carries `online`: " + deviceOnlineMeaning + " " + deviceHealthOPNsense + " " + deviceHealthRetained,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name (defaults to configured org)"),
				"device":       stringProperty("Device name (exact)"),
				"retained":     boolProperty("Serve the last-known telemetry, marked stale, when the live one has expired (default false)"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceHealth)
}

func (s *Server) handleDeviceHealth(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[deviceHealthInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.DeviceHealth(apiCtx, org, input.Device, service.DeviceHealthOpts{Retained: input.Retained})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(result, "")
}
