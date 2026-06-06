package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// taskResource surfaces device tasks (the async operations created by run/sync).
type taskResource struct{}

func (taskResource) Kind() string  { return "task" }
func (taskResource) Title() string { return "Tasks" }

func (taskResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "ID", Width: 10},
		{Title: "TYPE", Width: 18},
		{Title: "STATUS", Width: 12},
		{Title: "DEVICE", Width: 22},
		{Title: "CREATED", Width: 0},
	}
}

func (taskResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.TaskList(ctx, org, service.TaskListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Tasks))
	for _, t := range res.Tasks {
		rows = append(rows, registry.Row{
			ID: t.ID,
			Cells: []string{
				uihelp.Truncate(t.ID, 8),
				t.Type,
				t.Status,
				uihelp.Default(t.DeviceName, "—"),
				ago(t.CreatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (taskResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "x", Label: "cancel", Destructive: true,
			Prompt: "Cancel task {id}?"},
	}
}

func (taskResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "x":
		if err := svc.TaskCancel(ctx, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("cancelled %s", uihelp.Truncate(id, 8)), nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (taskResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	t, err := svc.TaskGet(ctx, id)
	if err != nil {
		return nil, err
	}
	fields := []registry.Field{
		{Label: "ID", Value: t.ID},
		{Label: "Type", Value: t.Type},
		{Label: "Status", Value: t.Status},
		{Label: "Device", Value: uihelp.Default(t.DeviceName, "—")},
		{Label: "Organization", Value: t.Organization},
		{Label: "Created", Value: fullTime(t.CreatedAt)},
		{Label: "Started", Value: fullTime(t.StartedAt)},
		{Label: "Completed", Value: fullTime(t.CompletedAt)},
		{Label: "Expires", Value: fullTime(t.ExpiresAt)},
	}
	sections := []registry.Section{{Title: "Task", Fields: fields}}
	if sec, ok := jsonSection("Result", t.Message); ok {
		sections = append(sections, sec)
	}
	if sec, ok := jsonSection("Payload", t.Payload); ok {
		sections = append(sections, sec)
	}
	if t.ErrorMessage != "" {
		sections = append(sections, registry.Section{Title: "Error", Text: t.ErrorMessage})
	}
	return sections, nil
}

// jsonSection turns a possibly-JSON task field into a readable section: a flat
// JSON object becomes labelled key/value fields; nested JSON is pretty-printed;
// anything else is shown verbatim. Returns ok=false for an empty field.
func jsonSection(title, raw string) (registry.Section, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return registry.Section{}, false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := make([]registry.Field, 0, len(keys))
		for _, k := range keys {
			fields = append(fields, registry.Field{Label: humanizeKey(k), Value: jsonValue(obj[k])})
		}
		return registry.Section{Title: title, Fields: fields}, true
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(raw), "", "  "); err == nil {
		return registry.Section{Title: title, Text: pretty.String()}, true
	}
	return registry.Section{Title: title, Text: raw}, true
}

// humanizeKey turns a snake_case JSON key into a "Title case" label.
func humanizeKey(k string) string {
	k = strings.ReplaceAll(k, "_", " ")
	r := []rune(k)
	if len(r) == 0 {
		return k
	}
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}

// jsonValue renders a decoded JSON value compactly; nested values are
// re-marshalled.
func jsonValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "—"
	case bool:
		return fmt.Sprintf("%t", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case string:
		return uihelp.Default(val, "—")
	default:
		if b, err := json.Marshal(val); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", val)
	}
}
