package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Shared input for every `ndcli.run.*` tool. Per-command fields land in
// the same struct because every tool has identical target+scheduling
// surface; the handler picks out the fields it cares about.
//
// At and Schedule are mutually exclusive: At defers a one-shot run;
// Schedule registers a recurring spec. The server enforces exclusion (422).
type runInput struct {
	Organization string   `json:"organization,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	OUs          []string `json:"ous,omitempty"`
	Org          bool     `json:"org,omitempty"`
	At           string   `json:"at,omitempty"`
	Schedule     string   `json:"schedule,omitempty"` // recurring spec registration
	// PING
	Host  string `json:"host,omitempty"`
	Count int    `json:"count,omitempty"`
	// PLUGIN_INSTALL
	Version string `json:"version,omitempty"`
	// FIRMWARE_UPGRADE
	Mode       string `json:"mode,omitempty"`        // "minor" | "major"
	Reboot     *bool  `json:"reboot,omitempty"`      // default true; nil = use default
	CheckFirst *bool  `json:"check_first,omitempty"` // default true; nil = use default
	DryRun     bool   `json:"dry_run,omitempty"`
	// Common
	Confirm bool `json:"confirm,omitempty"`
}

// registerRunTools registers the `ndcli run` MCP tools — the
// LLM-facing twin of the CLI surface in cli/run.go.
func (s *Server) registerRunTools() {
	targetingProps := map[string]interface{}{
		"organization": organizationProperty(),
		"devices":      stringArrayProperty("Target device names (repeatable)"),
		"ous":          stringArrayProperty("Target OU names; expands to enabled members"),
		"org":          boolProperty("Target every enabled device in the current org"),
		"at":           stringProperty("Defer execution. Accepts a relative offset (30m, 2h, 3d, 1w), a bare timestamp interpreted in NDCLI's configured timezone (2026-05-12 03:00), or RFC3339 with explicit tz (2026-05-12T03:00:00Z). Omit for immediate run. Mutually exclusive with schedule."),
		"schedule":     stringProperty("Register as a recurring spec on this named schedule instead of running immediately. Mutually exclusive with at."),
		"confirm":      confirmProperty(),
	}

	// ndcli.run.ping
	pingProps := mergeProps(targetingProps, map[string]interface{}{
		"host":  stringProperty("Target IP or hostname to ping (required)"),
		"count": intProperty("Number of ping packets (default 4)", 4),
	})
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.run.ping",
		Description: "Ping a target IP or hostname from one or more devices. `host` is required. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": pingProps,
			"required":   []string{"host"},
		},
	}, s.makeRunHandler("ping", models.TaskTypePing, func(in *runInput) map[string]interface{} {
		p := map[string]interface{}{"target": in.Host}
		if in.Count > 0 && in.Count != 4 {
			p["count"] = in.Count
		}
		return p
	}))

	// ndcli.run.poweroff
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.run.poweroff",
		Description: "Power off one or more devices. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": targetingProps,
		},
	}, s.makeRunHandler("poweroff", models.TaskTypeShutdown, nil))

	// ndcli.run.restart
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.run.restart",
		Description: "Restart (reboot) one or more devices. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": targetingProps,
		},
	}, s.makeRunHandler("restart", models.TaskTypeReboot, nil))

	// ndcli.run.plugin-install
	pluginInstallProps := mergeProps(targetingProps, map[string]interface{}{
		"version": stringProperty("Semver to pin install to (empty = upgrade to latest in the device's installed channel)"),
	})
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.run.plugin_install",
		Description: "(Re)install the NDAgent OPNsense plugin pkg on one or more devices. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": pluginInstallProps,
		},
	}, s.makeRunHandler("plugin-install", models.TaskTypePluginInstall, func(in *runInput) map[string]interface{} {
		p := map[string]interface{}{}
		if in.Version != "" {
			p["target_version"] = in.Version
		}
		return p
	}))

	// ndcli.run.plugin-reload
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.run.plugin_reload",
		Description: "Reload (restart) the NDAgent service on one or more devices. Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": targetingProps,
		},
	}, s.makeRunHandler("plugin-reload", models.TaskTypeRestart, nil))

	// ndcli.run.firmware_upgrade
	firmwareUpgradeProps := mergeProps(targetingProps, map[string]interface{}{
		"mode":        map[string]interface{}{"type": "string", "description": `Upgrade mode: "minor" (point release within current series) or "major" (series upgrade). Required.`, "enum": []string{"minor", "major"}},
		"version":     stringProperty(`Target version (optional). For minor: a point release such as "26.1.9". For major: a series such as "26.7".`),
		"reboot":      boolProperty("Reboot after applying the upgrade (default true). Set to false to apply packages only, leaving base/kernel deferred — the device will enter a mixed state. Not allowed when mode=major."),
		"check_first": boolProperty("Run a firmware availability check before applying (default true). Set to false to skip the pre-upgrade check."),
		"dry_run":     boolProperty("Report what would be applied without making any changes (default false)."),
	})
	s.mcpServer.AddTool(&mcp.Tool{
		Name: "ndcli.run.firmware_upgrade",
		Description: "Upgrade OPNsense firmware on one or more devices. " +
			"mode=minor applies a point release; mode=major upgrades the full series. " +
			"DESTRUCTIVE: triggers an upgrade and (by default) reboots the firewall. " +
			"major+reboot=false is rejected by the server (422). " +
			"Requires confirm=true.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": firmwareUpgradeProps,
			"required":   []string{"mode"},
		},
	}, s.handleFirmwareUpgrade)
}

// handleFirmwareUpgrade is the MCP handler for ndcli.run.firmware_upgrade.
// It mirrors makeRunHandler but adds client-side mode/reboot validation so
// LLM agents get a fast, clear rejection instead of a round-trip 422.
func (s *Server) handleFirmwareUpgrade(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	input, err := parseInput[runInput](argsJSON)
	if err != nil {
		return s.errorResult(err)
	}

	// Client-side validation: mode must be minor or major.
	if input.Mode != "minor" && input.Mode != "major" {
		return s.errorResult(fmt.Errorf(`mode must be "minor" or "major", got %q`, input.Mode))
	}

	// Resolve reboot/check_first with defaults (true).
	reboot := true
	if input.Reboot != nil {
		reboot = *input.Reboot
	}
	checkFirst := true
	if input.CheckFirst != nil {
		checkFirst = *input.CheckFirst
	}

	// Client-side guard: major + no-reboot is invalid.
	if input.Mode == "major" && !reboot {
		return s.errorResult(fmt.Errorf("major firmware upgrades require a reboot (reboot=false is not allowed with mode=major)"))
	}

	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	opts := service.RunOpts{
		Type:        models.TaskTypeFirmwareUpgrade,
		Devices:     input.Devices,
		OUs:         input.OUs,
		AllDevices:  input.Org,
		ScheduledAt: input.At,
		Schedule:    input.Schedule,
	}
	payload := map[string]interface{}{
		"mode":        input.Mode,
		"reboot":      reboot,
		"check_first": checkFirst,
		"dry_run":     input.DryRun,
	}
	if input.Version != "" {
		payload["target_version"] = input.Version
	}
	opts.Payload = payload

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// When --schedule is set, register a recurring spec. No confirm gate needed.
	if input.Schedule != "" {
		spec, err := s.svc.RunRegisterSpec(apiCtx, org, opts)
		if err != nil {
			return s.errorResult(err)
		}
		return s.successResult(spec, fmt.Sprintf("Registered %s spec %s on schedule %q", models.TaskTypeFirmwareUpgrade, spec.Code, spec.ScheduleName))
	}

	if !input.Confirm {
		scope := runScopeDescription(input)
		return s.previewResult("run firmware-upgrade on", scope)
	}

	result, err := s.svc.Run(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}

	tasks := make([]map[string]interface{}, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		tasks = append(tasks, map[string]interface{}{
			"task":        t.Task,
			"device":      t.DeviceName,
			"device_uuid": t.DeviceUUID,
			"status":      t.Status,
			"expires_at":  t.ExpiresAt,
		})
	}
	summary := fmt.Sprintf("%d %s task(s) created", result.Total, models.TaskTypeFirmwareUpgrade)
	if result.ScheduledAt != "" {
		summary = fmt.Sprintf("%d %s task(s) scheduled for %s", result.Total, models.TaskTypeFirmwareUpgrade, result.ScheduledAt)
	}
	return s.successResult(map[string]interface{}{
		"type":         result.Type,
		"organization": result.Organization,
		"scheduled_at": result.ScheduledAt,
		"total":        result.Total,
		"tasks":        tasks,
	}, summary)
}

func mergeProps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// makeRunHandler builds the handler for one `ndcli.run.*` tool. `friendly`
// is the user-facing name (used in preview messages), `taskType` is the
// internal NDDataModels enum string sent to NDManager, and `payloadFn`
// extracts command-specific payload from the input (nil for commands
// that take no payload params).
func (s *Server) makeRunHandler(friendly, taskType string, payloadFn func(*runInput) map[string]interface{}) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := s.svc.RequireAuth(); err != nil {
			return s.errorResult(err)
		}
		argsJSON, _ := json.Marshal(req.Params.Arguments)
		input, err := parseInput[runInput](argsJSON)
		if err != nil {
			return s.errorResult(err)
		}
		org, err := s.svc.ResolveOrg(input.Organization)
		if err != nil {
			return s.errorResult(err)
		}
		opts := service.RunOpts{
			Type:        taskType,
			Devices:     input.Devices,
			OUs:         input.OUs,
			AllDevices:  input.Org,
			ScheduledAt: input.At,
			Schedule:    input.Schedule,
		}
		if payloadFn != nil {
			opts.Payload = payloadFn(input)
		}

		apiCtx, cancel := contextWithTimeout()
		defer cancel()

		// When --schedule is set, register a recurring spec. No confirm gate
		// needed (no immediate side-effects).
		if input.Schedule != "" {
			spec, err := s.svc.RunRegisterSpec(apiCtx, org, opts)
			if err != nil {
				return s.errorResult(err)
			}
			return s.successResult(spec, fmt.Sprintf("Registered %s spec %s on schedule %q", taskType, spec.Code, spec.ScheduleName))
		}

		if !input.Confirm {
			scope := runScopeDescription(input)
			return s.previewResult(fmt.Sprintf("run %s on", friendly), scope)
		}

		result, err := s.svc.Run(apiCtx, org, opts)
		if err != nil {
			return s.errorResult(err)
		}

		tasks := make([]map[string]interface{}, 0, len(result.Tasks))
		for _, t := range result.Tasks {
			tasks = append(tasks, map[string]interface{}{
				"task":        t.Task,
				"device":      t.DeviceName,
				"device_uuid": t.DeviceUUID,
				"status":      t.Status,
				"expires_at":  t.ExpiresAt,
			})
		}
		summary := fmt.Sprintf("%d %s task(s) created", result.Total, taskType)
		if result.ScheduledAt != "" {
			summary = fmt.Sprintf("%d %s task(s) scheduled for %s", result.Total, taskType, result.ScheduledAt)
		}
		return s.successResult(map[string]interface{}{
			"type":         result.Type,
			"organization": result.Organization,
			"scheduled_at": result.ScheduledAt,
			"total":        result.Total,
			"tasks":        tasks,
		}, summary)
	}
}

func runScopeDescription(in *runInput) string {
	if in.Org {
		return "every device in org"
	}
	if len(in.Devices) > 0 && len(in.OUs) > 0 {
		return fmt.Sprintf("%d device(s) + %d OU(s)", len(in.Devices), len(in.OUs))
	}
	if len(in.Devices) > 0 {
		if len(in.Devices) == 1 {
			return in.Devices[0]
		}
		return fmt.Sprintf("%d devices", len(in.Devices))
	}
	if len(in.OUs) > 0 {
		if len(in.OUs) == 1 {
			return fmt.Sprintf("OU %s", in.OUs[0])
		}
		return fmt.Sprintf("%d OUs", len(in.OUs))
	}
	return "(no target)"
}
