package service

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// TaskListOpts collects every filter NDManager's /api/v1/tasks endpoint
// accepts. Empty fields are omitted; ExpiredSet must be true for Expired to
// be sent (so callers can distinguish "no filter" from "explicitly false").
//
// Every field takes the caller's raw input: TaskList validates and normalizes
// it (see normalizeTaskFilter), so a front-end does no parsing of its own. A
// task matches when it has any listed status, any listed type and any listed
// origin, none of the excluded types, and every other filter given.
//
// The fields from ExcludeTypes on need NDManager 1.99 (RunID and RequestedBy
// 1.100). An older one ignores them, which TaskList detects from the filters
// echo and refuses with CodeServerTooOld rather than return a list they did not
// filter.
type TaskListOpts struct {
	Statuses []string
	Types    []string
	// Device is a device-name pattern (RE2), sent as device_name.
	Device        string
	Expired       bool
	ExpiredSet    bool
	CreatedAfter  string
	CreatedBefore string
	// ExcludeTypes leaves these task types out.
	ExcludeTypes []string
	// Origins keeps the tasks from these sources: MANUAL, SCHEDULE, FIRMWARE.
	Origins []string
	// Schedule is the exact name of the schedule whose specs created the tasks.
	Schedule string
	// Devices are exact device names.
	Devices []string
	// Search is plain text found in the task code or the device's name.
	Search string
	// ScheduledAfter and ScheduledBefore bound scheduled_at, in the forms
	// helpers.ResolveTimeFilter reads.
	ScheduledAfter  string
	ScheduledBefore string
	// IncludeActive also lists every PENDING, SCHEDULED and IN_PROGRESS task
	// whatever CreatedAfter and CreatedBefore say.
	IncludeActive bool
	// RunID is the run of one create request or one fire of a schedule spec.
	RunID string
	// RequestedBy is the email of the account that asked for the tasks.
	RequestedBy string
	SortBy      string
	Page        int
	PerPage     int
}

// TaskListResult mirrors the paginated task list response with resolved
// pagination defaults.
type TaskListResult struct {
	Tasks   []models.Task
	Total   int
	Page    int
	PerPage int
}

// taskFilterFields names a filter by the field a caller sets it with, where
// that differs from the key NDManager echoes it under.
var taskFilterFields = map[string]string{"q": "search", "device": "device_exact"}

// ignoredTaskFilters lists the filters a request carried (by their echo keys)
// that the answer's echo lacks. NDManager echoes every filter it applies, and
// one that predates a filter ignores it without a word, so a missing key is a
// filter that was not applied.
func ignoredTaskFilters(sent []string, echo map[string]any) []string {
	var ignored []string
	for _, key := range sent {
		if _, ok := echo[key]; ok {
			continue
		}
		if field, ok := taskFilterFields[key]; ok {
			key = field
		}
		ignored = append(ignored, key)
	}
	return ignored
}

// splitValues splits each element on commas, trims it and drops empty and
// repeated values, keeping the first occurrence's order.
func splitValues(raw []string) []string {
	var out []string
	for _, item := range raw {
		for _, v := range strings.Split(item, ",") {
			if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// normalizeTaskFilter turns raw type/status filter input into the values to
// send: each element may itself be comma-separated, is trimmed and upper-cased,
// resolved through aliases and checked against valid; duplicates collapse.
// Empty elements are skipped, and "all" — or nothing left — means no filter
// (nil).
//
// NDManager silently drops a value it does not recognise and, when none is
// left, applies no filter, so a typo would come back as the full list with
// exit 0. Refuse it here instead, for every front-end.
func normalizeTaskFilter(kind string, raw, valid []string, aliases map[string]string) ([]string, error) {
	var out []string
	all := false
	for _, item := range raw {
		for _, v := range strings.Split(item, ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			canon := strings.ToUpper(v)
			if canon == "ALL" {
				all = true
				continue
			}
			if alias, ok := aliases[canon]; ok {
				canon = alias
			}
			if !slices.Contains(valid, canon) {
				return nil, &Error{
					Code:    CodeInvalidInput,
					Message: fmt.Sprintf("unknown task %s %q — valid values: %s", kind, v, models.TaskValuesHelp(valid)),
				}
			}
			if !slices.Contains(out, canon) {
				out = append(out, canon)
			}
		}
	}
	if all {
		return nil, nil
	}
	return out, nil
}

// TaskList returns a paginated, filtered list of tasks for the organization.
func (s *Service) TaskList(ctx context.Context, org string, opts TaskListOpts) (*TaskListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 30
	}

	types, err := normalizeTaskFilter("type", opts.Types, models.TaskTypes, nil)
	if err != nil {
		return nil, err
	}
	statuses, err := normalizeTaskFilter("status", opts.Statuses, models.TaskStatuses, models.TaskStatusAliases)
	if err != nil {
		return nil, err
	}
	for _, v := range splitValues(opts.ExcludeTypes) {
		if strings.EqualFold(v, "all") {
			return nil, &Error{Code: CodeInvalidInput, Message: "exclude_type takes task types: excluding all of them would list nothing"}
		}
	}
	excluded, err := normalizeTaskFilter("type", opts.ExcludeTypes, models.TaskTypes, nil)
	if err != nil {
		return nil, err
	}
	origins, err := normalizeTaskFilter("origin", opts.Origins, models.TaskOrigins, nil)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID != "" && !models.RunIDPattern.MatchString(runID) {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid run_id %q: a run id is 8 letters or digits", runID)}
	}

	// The keys of the filters NDManager echoes only once it applies them.
	var echoed []string
	query := url.Values{}
	if org != "" {
		query.Set("organization", org)
	}
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	for _, v := range statuses {
		query.Add("status", v)
	}
	for _, v := range types {
		query.Add("type", v)
	}
	if opts.Device != "" {
		query.Set("device_name", opts.Device)
	}
	if opts.ExpiredSet {
		query.Set("expired", strconv.FormatBool(opts.Expired))
	}
	if opts.SortBy != "" {
		query.Set("sort_by", opts.SortBy)
	}

	for _, f := range [][2]string{
		{opts.CreatedAfter, "created_after"},
		{opts.CreatedBefore, "created_before"},
	} {
		if f[0] == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f[0])
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f[1], err),
				Err:     err,
			}
		}
		query.Set(f[1], parsed)
	}

	add := func(key string, values ...string) {
		if len(values) == 0 {
			return
		}
		for _, v := range values {
			query.Add(key, v)
		}
		echoed = append(echoed, key)
	}
	add("exclude_type", excluded...)
	add("origin", origins...)
	add("device", splitValues(opts.Devices)...)
	for _, f := range [][2]string{
		{"schedule", strings.TrimSpace(opts.Schedule)},
		{"q", strings.TrimSpace(opts.Search)},
		{"run_id", runID},
		{"requested_by", strings.TrimSpace(opts.RequestedBy)},
	} {
		if f[1] != "" {
			add(f[0], f[1])
		}
	}
	for _, f := range [][2]string{
		{opts.ScheduledAfter, "scheduled_after"},
		{opts.ScheduledBefore, "scheduled_before"},
	} {
		if strings.TrimSpace(f[0]) == "" {
			continue
		}
		at, err := helpers.ResolveTimeFilter(f[0], time.Now())
		if err != nil {
			return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid %s value: %v", f[1], err), Err: err}
		}
		add(f[1], at.Format(time.RFC3339))
	}
	if opts.IncludeActive {
		add("include_active", "true")
	}

	resp, err := s.api.GetValues(ctx, "/api/v1/tasks", query)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.TaskListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	if ignored := ignoredTaskFilters(echoed, result.Filters); len(ignored) > 0 {
		noun, pronoun := "filter", "it"
		if len(ignored) > 1 {
			noun, pronoun = "filters", "them"
		}
		slices.Sort(ignored)
		return nil, &Error{Code: CodeServerTooOld, Message: fmt.Sprintf(
			"this NDManager predates the task list %s %s: it did not apply %s, so no list is shown",
			noun, strings.Join(ignored, ", "), pronoun)}
	}
	return &TaskListResult{
		Tasks:   result.Items,
		Total:   result.Total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

// TaskGet returns a single task by ID. Tasks live at the platform level (not
// org-scoped on this endpoint), so org is not required.
func (s *Service) TaskGet(ctx context.Context, taskID string) (*models.Task, error) {
	if taskID == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "task id is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var task models.Task
	if err := api.ParseResponse(resp, &task); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &task, nil
}

// TaskCancel cancels a pending or scheduled task.
func (s *Service) TaskCancel(ctx context.Context, taskID string) error {
	if taskID == "" {
		return &Error{Code: CodeInvalidInput, Message: "task id is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/tasks/%s/cancel", taskID), nil)
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// RunOpts is the typed input for Service.Run. The Type uses NDDataModels
// enum strings (PING/SHUTDOWN/...). At least one of Devices/OUs/AllDevices
// must be set; the friendly-name mapping (`ping` → PING, etc.) lives in
// cli/run.go so it stays out of MCP-facing surface area.
//
// Schedule and ScheduledAt are mutually exclusive. When Schedule is set, the
// server registers a ScheduledTask spec instead of creating tasks immediately;
// use RunRegisterSpec for that path so the two response types stay separate.
type RunOpts struct {
	Type       string                 // PING, SHUTDOWN, REBOOT, RESTART, PLUGIN_INSTALL, FIRMWARE_UPGRADE
	Payload    map[string]interface{} // type-specific; PING: target+count, PLUGIN_INSTALL: target_version, FIRMWARE_UPGRADE: mode/reboot/check_first/dry_run
	Devices    []string               // repeatable
	OUs        []string               // repeatable
	AllDevices bool                   // mutually exclusive with Devices/OUs
	// ScheduledAt is an `--at`-style scheduling input: a relative offset
	// (`30m`, `2h`), an RFC3339 instant with an explicit offset or `Z`, or a
	// bare timestamp interpreted in ScheduledAtLocation. Empty = run
	// immediately. Mutually exclusive with Schedule.
	//
	// Run normalizes it to UTC RFC3339 before it reaches the wire — front-ends
	// must not send a wall-clock value with the zone dropped.
	ScheduledAt string
	// ScheduledAtLocation interprets a bare ScheduledAt timestamp. nil means
	// time.Local. Front-ends pass output.Location() (the configured NDCLI
	// timezone) or, over MCP, the caller-supplied IANA zone.
	ScheduledAtLocation *time.Location
	Schedule            string // schedule name; when set, call RunRegisterSpec instead
}

// scheduledAtSkew is the backward tolerance on a scheduling instant: clock
// drift between the client and NDManager must not reject a legitimate "now",
// but an obviously stale timestamp is a typo worth refusing.
const scheduledAtSkew = 30 * time.Second

// ScheduledAt is a resolved scheduling instant: the absolute time to send on
// the wire, the same instant rendered in the timezone that produced it, and
// the name of that timezone. Front-ends echo all three so the user can check
// the schedule against the timezone they meant.
type ScheduledAt struct {
	UTC   time.Time // the absolute instant, in UTC
	Zoned time.Time // the same instant in the timezone used to resolve it
	Zone  string    // display name of that timezone (IANA name, abbreviation, or UTC±HH:MM)
}

// RFC3339UTC is the wire form: the instant as UTC with a `Z` suffix.
func (s ScheduledAt) RFC3339UTC() string { return s.UTC.Format(time.RFC3339) }

// RFC3339Zoned renders the instant in the timezone used to resolve it.
func (s ScheduledAt) RFC3339Zoned() string { return s.Zoned.Format(time.RFC3339) }

// ResolveScheduledAt parses an `--at`-style scheduling input into an absolute
// instant, rejecting anything already in the past. loc interprets bare
// timestamps (nil = time.Local); an input carrying its own zone — a relative
// offset, or RFC3339 with an explicit offset or `Z` — ignores loc.
//
// label names the input in error messages: "--at" for the CLI flag, "at" for
// the MCP parameter. An empty input resolves to (nil, nil).
//
// This is the single normalization path for every front-end. Sending a
// wall-clock string straight through is what made MCP-scheduled tasks fire at
// the UTC reading of a local time.
func ResolveScheduledAt(at string, loc *time.Location, label string) (*ScheduledAt, error) {
	if strings.TrimSpace(at) == "" {
		return nil, nil
	}
	if loc == nil {
		loc = time.Local
	}
	zoned, err := helpers.ParseFutureTimeZoned(at, loc)
	if err != nil {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("%s: %v", label, err)}
	}
	if zoned.Before(time.Now().Add(-scheduledAtSkew)) {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("%s is in the past", label)}
	}
	return &ScheduledAt{UTC: zoned.UTC(), Zoned: zoned, Zone: scheduledZoneName(at, zoned, loc)}, nil
}

// scheduledZoneName names the zone the instant was resolved in, from the shape
// of the input rather than from the location the parse happened to attach.
//
// That distinction matters: time.Parse attaches time.Local, not a synthetic
// fixed zone, when an explicit numeric offset happens to equal this process's
// own current offset — so reading the parsed location would name the same user
// input differently depending on which host the MCP server runs on. An offset
// the user wrote is reported as that offset, always.
func scheduledZoneName(input string, t time.Time, loc *time.Location) string {
	switch {
	case helpers.IsRelativeOffset(input):
		// Anchored to now, resolved in UTC; the location is not involved.
		return "UTC"
	case helpers.HasExplicitZone(input):
		if _, offset := t.Zone(); offset == 0 {
			return "UTC"
		}
		return "UTC" + t.Format("-07:00")
	default:
		return locationDisplayName(loc, t)
	}
}

// locationDisplayName names the location a bare timestamp was read in. "Local"
// is not useful on its own, so the host's abbreviation and offset stand in.
func locationDisplayName(loc *time.Location, t time.Time) string {
	if loc == nil {
		loc = time.Local
	}
	if name := loc.String(); name != "" && name != "Local" {
		return name
	}
	if abbr, _ := t.Zone(); abbr != "" {
		return abbr
	}
	return "UTC" + t.Format("-07:00")
}

var validRunTypes = map[string]bool{
	models.TaskTypePing:            true,
	models.TaskTypeShutdown:        true,
	models.TaskTypeReboot:          true,
	models.TaskTypeRestart:         true,
	models.TaskTypePluginInstall:   true,
	models.TaskTypeFirmwareUpgrade: true,
}

// Run posts to POST /api/v1/organizations/{org}/tasks — the server-side
// fan-out endpoint. NDManager resolves devices/OUs/all, creates one task
// per resolved device, and returns the list. SCHEDULED tasks come back
// with status=SCHEDULED; immediate tasks come back PENDING.
func (s *Service) Run(ctx context.Context, org string, opts RunOpts) (*models.RunResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	taskType := strings.ToUpper(opts.Type)
	if !validRunTypes[taskType] {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid task type: %s", opts.Type),
		}
	}
	if !opts.AllDevices && len(opts.Devices) == 0 && len(opts.OUs) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "name at least one device or OU, or target every enabled device in the organization"}
	}
	if opts.AllDevices && (len(opts.Devices) > 0 || len(opts.OUs) > 0) {
		return nil, &Error{Code: CodeInvalidInput, Message: "targeting every enabled device cannot be combined with named devices or OUs"}
	}
	if opts.Schedule != "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "a one-shot run cannot carry a schedule name — use RunRegisterSpec to register a recurring spec"}
	}

	body := map[string]interface{}{
		"type": taskType,
		"targets": map[string]interface{}{
			"devices": nonNilStrings(opts.Devices),
			"ous":     nonNilStrings(opts.OUs),
			"all":     opts.AllDevices,
		},
	}
	if opts.Payload != nil && len(opts.Payload) > 0 {
		body["payload"] = opts.Payload
	}
	// Normalize here, not in the front-ends: the wire value must be an
	// absolute UTC instant no matter which surface built the opts.
	resolved, err := ResolveScheduledAt(opts.ScheduledAt, opts.ScheduledAtLocation, "scheduled_at")
	if err != nil {
		return nil, err
	}
	if resolved != nil {
		body["scheduled_at"] = resolved.RFC3339UTC()
	}

	endpoint := fmt.Sprintf("/api/v1/organizations/%s/tasks", url.PathEscape(org))
	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.RunResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// RunRegisterSpec posts to POST /api/v1/organizations/{org}/tasks with a
// "schedule" field, which instructs NDManager to register a recurring
// ScheduledTask spec instead of creating tasks immediately. The server returns
// a 201 spec descriptor rather than a task table.
func (s *Service) RunRegisterSpec(ctx context.Context, org string, opts RunOpts) (*models.ScheduledTaskRegisterResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if opts.Schedule == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "schedule name is required for spec registration"}
	}
	// A recurring spec has no single anchor instant, and the registration body
	// carries no scheduled_at at all. Accepting both would let a caller be
	// shown a one-time instant for something that will never fire at it.
	if strings.TrimSpace(opts.ScheduledAt) != "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "at and schedule are mutually exclusive: a recurring spec has no one-time scheduled instant"}
	}
	taskType := strings.ToUpper(opts.Type)
	if !validRunTypes[taskType] {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid task type: %s", opts.Type),
		}
	}
	if !opts.AllDevices && len(opts.Devices) == 0 && len(opts.OUs) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "name at least one device or OU, or target every enabled device in the organization"}
	}
	if opts.AllDevices && (len(opts.Devices) > 0 || len(opts.OUs) > 0) {
		return nil, &Error{Code: CodeInvalidInput, Message: "targeting every enabled device cannot be combined with named devices or OUs"}
	}

	body := map[string]interface{}{
		"type": taskType,
		"targets": map[string]interface{}{
			"devices": nonNilStrings(opts.Devices),
			"ous":     nonNilStrings(opts.OUs),
			"all":     opts.AllDevices,
		},
		"schedule": opts.Schedule,
	}
	if opts.Payload != nil && len(opts.Payload) > 0 {
		body["payload"] = opts.Payload
	}

	endpoint := fmt.Sprintf("/api/v1/organizations/%s/tasks", url.PathEscape(org))
	resp, err := s.api.Post(ctx, endpoint, body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.ScheduledTaskRegisterResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}

// nonNilStrings returns an empty (non-nil) slice for a nil input so JSON-encoded
// task targets serialize as [] rather than null. NDManager validates
// targets.devices / targets.ous as lists and rejects null with
// "Input should be a valid list" — this bit callers (e.g. the TUI device
// actions) that target by device only and leave OUs nil.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
