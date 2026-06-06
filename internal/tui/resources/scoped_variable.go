package resources

import (
	"context"
	"fmt"

	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
)

// ScopedVarResource is an editable variable list bound to a scope (org / ou /
// template / device) and entity. It backs both the palette "Variables" page
// (org scope) and the device page's "Variable Overrides" (device scope), so
// variable management lives in one place.
type ScopedVarResource struct {
	Scope  service.VariableScope
	Entity string // "" for org scope; OU/template/device name otherwise
	Name   string // display title
	KindID string // registry kind (must be unique per scope)
}

func (r ScopedVarResource) Kind() string  { return r.KindID }
func (r ScopedVarResource) Title() string { return r.Name }

func (ScopedVarResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 30},
		{Title: "SECRET", Width: 8},
		{Title: "VALUE", Width: 0},
	}
}

func (r ScopedVarResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	if perPage < 1 || perPage > 100 { // NDManager caps variable per_page at 100
		perPage = 100
	}
	res, err := svc.VariableList(ctx, r.Scope, org, r.Entity, service.VariableListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Variables))
	for _, v := range res.Variables {
		value := v.Value
		if v.Secret {
			value = "••••••"
		}
		rows = append(rows, registry.Row{
			ID:    v.Name,
			Cells: []string{v.Name, fmt.Sprintf("%t", v.Secret), value},
		})
	}
	return rows, res.Total, nil
}

func (ScopedVarResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "new", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "value", Label: "Value", Required: true},
			{Key: "secret", Label: "Secret", Options: []string{"no", "yes"}},
		}},
		{Key: "e", Label: "edit", Form: []registry.FormField{
			{Key: "value", Label: "New value", Required: true},
		}},
		{Key: "x", Label: "delete", Destructive: true, Prompt: "Delete variable {id}?"},
	}
}

func (r ScopedVarResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		name := args["name"]
		opts := service.VariableCreateOpts{Name: name, Value: args["value"], Secret: args["secret"] == "yes"}
		if _, err := svc.VariableCreate(ctx, r.Scope, org, r.Entity, opts); err != nil {
			return "", err
		}
		return "added " + name, nil
	case "e":
		val := args["value"]
		if _, err := svc.VariableSet(ctx, r.Scope, org, r.Entity, id, service.VariableSetOpts{Value: &val}); err != nil {
			return "", err
		}
		return "updated " + id, nil
	case "x":
		if err := svc.VariableDelete(ctx, r.Scope, org, r.Entity, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}
