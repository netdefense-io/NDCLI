package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP parity for `ndcli dashboard` + `ndcli device health` — see
// CLAUDE.md "MCP-parity policy". Both calls are read-only and cheap, so
// no confirm gate.

type dashboardInput struct {
	Organization string `json:"organization,omitempty"`
}

type deviceHealthInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
}

func (s *Server) registerDashboardTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.dashboard",
		Description: "Org-level dashboard roll-up: device/sync/tasks-24h counters, agent-version histogram, and the compact fleet table with per-row attention summary (services down, pending updates, certs expiring ≤30d). Organization defaults to the configured org.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name (defaults to configured org)"),
			},
		},
	}, s.handleDashboard)

	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.device.health",
		Description: "Per-device drill-down with the full agent telemetry snapshot — uptime, load, mem/swap, disks per mountpoint, service running state, pending OS/plugin updates, certificate expiries. No history; this is the instant snapshot.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": stringProperty("Organization name (defaults to configured org)"),
				"device":       stringProperty("Device name (exact)"),
			},
			"required": []string{"device"},
		},
	}, s.handleDeviceHealth)
}

func (s *Server) handleDashboard(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[dashboardInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.Dashboard(apiCtx, org)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(result, "")
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

	result, err := s.svc.DeviceHealth(apiCtx, org, input.Device)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(result, "")
}
