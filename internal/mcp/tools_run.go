package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
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
	Timezone     string   `json:"timezone,omitempty"` // IANA name; interprets a bare At timestamp
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

// atParamDescription and timezoneParamDescription are the single source of
// truth for how the scheduling parameters are advertised: the MCPB manifest
// and the Smithery server card are both generated from the live tool registry
// (scripts/gen-mcpb-manifest.py), so this text is what every catalog shows.
//
// The wording is deliberately insistent about asking the user. An agent that
// guesses a timezone schedules a firewall reboot at the wrong hour and nothing
// in the transcript looks wrong.
const atParamDescription = "Defer execution to a future instant. Three accepted forms: " +
	"a relative offset (30m, 2h, 3d, 1w); RFC3339 with an explicit UTC offset or Z " +
	"(2026-05-12T03:00:00-03:00, 2026-05-12T03:00:00Z); or a bare timestamp " +
	"(2026-05-12 03:00, 2026-05-12T03:00), which REQUIRES the timezone parameter. " +
	"Scheduling is timezone-sensitive: the user's timezone, the device's local timezone " +
	"and UTC can differ, so unless the user stated the timezone explicitly, ask which one " +
	"they mean before calling with confirm=true, and state the resolved UTC time back to " +
	"them. When the target is a single device that reports its own timezone, the response " +
	"names it as device_timezone and shows the instant as scheduled_at_device_local, with a " +
	"warning when the two disagree — the device timezone is shown, never adopted, and never " +
	"consulted for an OU or org-wide target whose devices may sit in different zones. " +
	"When confirming a run you previewed, pass the preview's scheduled_at value " +
	"back as at, so the instant cannot drift — a relative offset like 30m is re-resolved " +
	"against a new \"now\" on the confirming call. Omit for an immediate run. Mutually " +
	"exclusive with schedule."

const timezoneParamDescription = "IANA timezone name (e.g. America/Sao_Paulo, Europe/Lisbon, UTC) " +
	"used to interpret a bare `at` timestamp. Required when `at` is a bare timestamp; ignored " +
	"when `at` is a relative offset or already carries an explicit UTC offset or Z. " +
	"There is no device-timezone default: a device's own configured timezone is reported back " +
	"for a single-device target (device_timezone) so you can show it to the user, but it never " +
	"fills this parameter in. Ask the user when the user's timezone, the device's and UTC differ."

// resolveRunScheduledAt turns the `at` + `timezone` parameters into an
// absolute instant, or refuses. Unlike the CLI — which may fall back to the
// configured timezone because a human typed the value and knows their own
// clock — the MCP surface refuses a bare timestamp with no timezone: the
// caller is a model that may be reasoning about the user's timezone, the
// device's, or UTC, and silently picking one is the bug this refusal
// prevents.
// westmostZone is UTC-12:00, the furthest-behind offset in use. A bare
// timestamp read there is the latest absolute instant any timezone could give
// it, which is what makes it the right yardstick for "no timezone can help".
var westmostZone = time.FixedZone("UTC-12:00", -12*60*60)

func resolveRunScheduledAt(at, timezone, schedule string) (*service.ScheduledAt, error) {
	at = strings.TrimSpace(at)
	timezone = strings.TrimSpace(timezone)
	if at == "" {
		return nil, nil
	}
	// A recurring spec has no one-time instant and its registration body
	// carries no scheduled_at, so a preview echoing one would describe a
	// firing that never happens. Refuse before the preview, not at execution.
	if strings.TrimSpace(schedule) != "" {
		return nil, &service.Error{
			Code:    service.CodeInvalidInput,
			Message: "at and schedule are mutually exclusive: at defers a single run, schedule registers a recurring spec that has no one-time instant",
		}
	}

	// timezone is consulted only where it changes the answer — a bare
	// timestamp. Validating it for an input that carries its own zone would
	// contradict the parameter's own contract ("ignored when...") and turn a
	// harmless stale value into a hard failure.
	loc := output.Location()
	if helpers.IsBareTimestamp(at) {
		if timezone == "" {
			// Never ask for a timezone that cannot rescue the input. Resolving
			// against the westernmost zone is the most favourable reading
			// available — it makes a given wall-clock the latest instant any
			// timezone could name — so a failure there is a failure under all
			// of them, whether the value is unparseable or simply past.
			if _, err := service.ResolveScheduledAt(at, westmostZone, "at"); err != nil {
				return nil, err
			}
			return nil, &service.Error{
				Code: service.CodeInvalidInput,
				Message: fmt.Sprintf(
					"at: %q carries no timezone, so the instant it names is ambiguous. Pass timezone with an IANA name (NDCLI is configured for %s), or give `at` an explicit UTC offset or Z (2026-05-12T03:00:00-03:00). Confirm with the user which timezone the time refers to before scheduling.",
					at, configuredTimezoneHint()),
			}
		}
		l, err := time.LoadLocation(timezone)
		if err != nil {
			return nil, &service.Error{
				Code:    service.CodeInvalidInput,
				Message: fmt.Sprintf("timezone: %q is not a valid IANA timezone name (expected something like America/Sao_Paulo, Europe/Lisbon or UTC)", timezone),
			}
		}
		loc = l
	}

	return service.ResolveScheduledAt(at, loc, "at")
}

// configuredTimezoneHint names NDCLI's configured display timezone for the
// error above. "Local" alone is useless as a suggestion — it is not an IANA
// name the caller can pass back — so the host's current abbreviation and
// offset are appended.
func configuredTimezoneHint() string {
	name := output.GetTimezone()
	if name != "Local" {
		return name
	}
	now := time.Now().In(output.Location())
	abbr, _ := now.Zone()
	if abbr != "" {
		return fmt.Sprintf("Local (%s, UTC%s)", abbr, now.Format("-07:00"))
	}
	return fmt.Sprintf("Local (UTC%s)", now.Format("-07:00"))
}

// previewScheduleEcho is scheduleEcho plus the instruction that keeps a
// previewed instant from drifting: the confirming call re-resolves whatever
// `at` it is given, so a relative offset previewed now lands somewhere else
// when confirmed a minute later. Passing the resolved value back pins it.
func previewScheduleEcho(resolved *service.ScheduledAt) map[string]interface{} {
	echo := scheduleEcho(resolved)
	if echo == nil {
		return nil
	}
	echo["confirm_hint"] = fmt.Sprintf(
		"To schedule exactly this instant, confirm with at=%q (timezone is not needed — the value carries its own). Re-sending a relative offset would resolve against a later \"now\".",
		resolved.RFC3339UTC())
	return echo
}

// scheduleEcho renders the resolved instant as named response fields. The
// model has to be able to restate the schedule to the user without parsing
// prose, and it has to be able to see the offset it was resolved against —
// hence the UTC form, the zoned form, the zone name, and "now".
func scheduleEcho(resolved *service.ScheduledAt) map[string]interface{} {
	if resolved == nil {
		return nil
	}
	return map[string]interface{}{
		"scheduled_at":          resolved.RFC3339UTC(),
		"scheduled_at_local":    resolved.RFC3339Zoned(),
		"scheduled_at_timezone": resolved.Zone,
		"now_utc":               time.Now().UTC().Format(time.RFC3339),
	}
}

// runResultResponse renders an executed run for both `ndcli.run.*` handlers.
// One implementation, so the generic path and the firmware-upgrade path cannot
// describe the same result differently.
func runResultResponse(result *models.RunResult, resolved *service.ScheduledAt, taskType string) (map[string]interface{}, string) {
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
	data := map[string]interface{}{
		"type":         result.Type,
		"organization": result.Organization,
		"scheduled_at": result.ScheduledAt,
		"total":        result.Total,
		"tasks":        tasks,
	}
	summary := fmt.Sprintf("%d %s task(s) created", result.Total, taskType)
	if resolved != nil {
		// Prefer the instant this client resolved over the server echo: it is
		// the value that was actually sent, and it is guaranteed UTC.
		for k, v := range scheduleEcho(resolved) {
			data[k] = v
		}
		summary = fmt.Sprintf("%d %s task(s) scheduled for %s (%s %s)",
			result.Total, taskType, resolved.RFC3339UTC(), resolved.RFC3339Zoned(), resolved.Zone)
	}
	return data, summary
}

// registerRunTools registers the `ndcli run` MCP tools — the
// LLM-facing twin of the CLI surface in cli/run.go.
func (s *Server) registerRunTools() {
	targetingProps := map[string]interface{}{
		"organization": organizationProperty(),
		"devices":      stringArrayProperty("Target device names (repeatable)"),
		"ous":          stringArrayProperty("Target OU names; expands to enabled members"),
		"org":          boolProperty("Target every enabled device in the current org"),
		"at":           stringProperty(atParamDescription),
		"timezone":     stringProperty(timezoneParamDescription),
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
	return s.firmwareUpgrade(ctx, input)
}

// firmwareUpgrade holds the validation + confirm-gated execution logic for
// ndcli.run.firmware_upgrade, split out from handleFirmwareUpgrade so it can
// be exercised directly in tests without a live authenticated Service
// (RequireAuth needs a real *auth.Manager, which can't be faked from this
// package).
func (s *Server) firmwareUpgrade(ctx context.Context, input *runInput) (*mcp.CallToolResult, error) {
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

	// Resolve the scheduling instant before anything else touches the
	// network, so an ambiguous `at` is refused without a round trip.
	resolved, err := resolveRunScheduledAt(input.At, input.Timezone, input.Schedule)
	if err != nil {
		return s.errorResult(err)
	}

	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}

	opts := service.RunOpts{
		Type:       models.TaskTypeFirmwareUpgrade,
		Devices:    input.Devices,
		OUs:        input.OUs,
		AllDevices: input.Org,
		Schedule:   input.Schedule,
	}
	if resolved != nil {
		opts.ScheduledAt = resolved.RFC3339UTC()
	}
	opts.Payload = map[string]interface{}{
		"mode":        input.Mode,
		"reboot":      reboot,
		"check_first": checkFirst,
		"dry_run":     input.DryRun,
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// The confirm gate covers both immediate execution and schedule
	// registration — planting a persistent recurring spec deserves the same
	// preview as an immediate run.
	if !input.Confirm {
		scope := runScopeDescription(input)
		action := "run firmware-upgrade on"
		if input.Schedule != "" {
			action = fmt.Sprintf("register a %s spec on schedule %q for", models.TaskTypeFirmwareUpgrade, input.Schedule)
		}
		return s.previewResultWithData(action, scope, s.addDeviceTimezoneEcho(org, input, resolved, previewScheduleEcho(resolved)))
	}

	if input.Schedule != "" {
		spec, err := s.svc.RunRegisterSpec(apiCtx, org, opts)
		if err != nil {
			return s.errorResult(err)
		}
		return s.successResult(spec, fmt.Sprintf("Registered %s spec %s on schedule %q", models.TaskTypeFirmwareUpgrade, spec.Code, spec.ScheduleName))
	}

	result, err := s.svc.Run(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}

	data, summary := runResultResponse(result, resolved, models.TaskTypeFirmwareUpgrade)
	return s.successResult(s.addDeviceTimezoneEcho(org, input, resolved, data), summary)
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
		return s.runCommand(ctx, friendly, taskType, payloadFn, input)
	}
}

// runCommand holds the confirm-gated execution logic shared by every
// ndcli.run.* tool, split out from makeRunHandler so it can be exercised
// directly in tests without a live authenticated Service (RequireAuth needs
// a real *auth.Manager, which can't be faked from this package).
func (s *Server) runCommand(ctx context.Context, friendly, taskType string, payloadFn func(*runInput) map[string]interface{}, input *runInput) (*mcp.CallToolResult, error) {
	// Resolve the scheduling instant before anything else touches the
	// network, so an ambiguous `at` is refused without a round trip.
	resolved, err := resolveRunScheduledAt(input.At, input.Timezone, input.Schedule)
	if err != nil {
		return s.errorResult(err)
	}

	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	opts := service.RunOpts{
		Type:       taskType,
		Devices:    input.Devices,
		OUs:        input.OUs,
		AllDevices: input.Org,
		Schedule:   input.Schedule,
	}
	if resolved != nil {
		opts.ScheduledAt = resolved.RFC3339UTC()
	}
	if payloadFn != nil {
		opts.Payload = payloadFn(input)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	// The confirm gate covers both immediate execution and schedule
	// registration — planting a persistent recurring spec deserves the same
	// preview as an immediate run.
	if !input.Confirm {
		scope := runScopeDescription(input)
		action := fmt.Sprintf("run %s on", friendly)
		if input.Schedule != "" {
			action = fmt.Sprintf("register a %s spec on schedule %q for", taskType, input.Schedule)
		}
		return s.previewResultWithData(action, scope, s.addDeviceTimezoneEcho(org, input, resolved, previewScheduleEcho(resolved)))
	}

	if input.Schedule != "" {
		spec, err := s.svc.RunRegisterSpec(apiCtx, org, opts)
		if err != nil {
			return s.errorResult(err)
		}
		return s.successResult(spec, fmt.Sprintf("Registered %s spec %s on schedule %q", taskType, spec.Code, spec.ScheduleName))
	}

	result, err := s.svc.Run(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}

	data, summary := runResultResponse(result, resolved, taskType)
	return s.successResult(s.addDeviceTimezoneEcho(org, input, resolved, data), summary)
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
