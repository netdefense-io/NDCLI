// Package registry defines the Resource abstraction that every NetDefense
// domain (devices, tasks, orgs, …) implements so the TUI can list, describe
// and act on it uniformly. Adding a new domain to the TUI is one file in
// internal/tui/resources implementing this interface plus a registration line.
//
// The package depends only on internal/service (for the typed methods every
// Resource calls) and the standard library, so it sits below both the
// resources package (which implements Resource) and the tui package (which
// renders it) without import cycles.
package registry

import (
	"context"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// Column describes one column of a Resource's list view.
type Column struct {
	Title string
	// Width is the fixed cell width in characters. 0 means "flex": the
	// column absorbs whatever horizontal space the others leave.
	Width int
}

// Row is one rendered list row. ID is the stable entity key used for
// drill-down and actions (device name, task id, …). Cells must align 1:1
// with the Resource's Columns().
type Row struct {
	ID    string
	Cells []string
}

// FormField describes one input collected in a modal before a parameterised
// action runs. Options non-empty makes it a select (cycle with ←/→); otherwise
// it is a free-text field. The collected value arrives in Execute's args map
// under Key.
type FormField struct {
	Key         string
	Label       string
	Default     string
	Placeholder string
	Required    bool
	Options     []string
	// OptionsFrom, when set, makes this a dynamic select: the app resolves the
	// option list when the form opens by calling the Resource's FormOptions with
	// this token (e.g. "template-snippets"), then populates Options from the
	// result. Mutually exclusive with a static Options slice. If the resolved
	// list is empty the app aborts the action with a "no <field> available"
	// message instead of opening an unusable form.
	OptionsFrom string
}

// Action is an operation a Resource exposes from its list view, surfaced as a
// single-key binding in the footer.
//
// Dispatch precedence in the app: Shell → Form → Destructive(confirm) → direct.
type Action struct {
	Key   string // single key that triggers it, e.g. "a"
	Label string // short footer label, e.g. "approve"
	// Destructive actions open a confirmation modal before executing.
	Destructive bool
	// Prompt overrides the default confirmation question. {id} is replaced
	// with the selected row ID. Empty falls back to a generated prompt.
	Prompt string
	// BlastRadius, when non-empty, upgrades the confirm modal to
	// type-to-confirm and shows this text as a highlighted warning. Use for
	// fleet-wide operations (approve-all, sync apply to a whole org).
	BlastRadius string
	// TargetsAll marks an action that ignores the selected row and operates
	// on the whole resource scope (e.g. approve-all). The app passes an empty
	// id to Execute and does not require a row selection.
	TargetsAll bool
	// Form, when non-empty, collects these inputs in a modal before Execute is
	// called; the values arrive in Execute's args map keyed by FormField.Key.
	// A Form action is its own gate, so it does not also open the confirm modal.
	Form []FormField
	// Shell, when non-empty, runs the local `ndcli <args>` binary in a
	// suspended subprocess (for interactive flows like connect or $EDITOR
	// edits) instead of calling Execute. Each arg has {id} and {org}
	// substituted. Shell actions do not call Execute.
	Shell []string
	// Nav, when set, makes this action a drill-in: instead of executing, the app
	// pushes a child list screen over the Resource returned by the Resource's
	// Navigate(org, id, Nav). Use for parent→children relationships too rich for
	// a single row action — a network's members/links/prefixes, a schedule's
	// tasks, an OU/template's variables. Nav actions require a selected row.
	Nav string
}

// Field is a labelled value in a describe Section.
type Field struct {
	Label string
	Value string
}

// Section is a titled block in a describe view: either Fields or free Text.
type Section struct {
	Title  string
	Fields []Field
	Text   string
}

// Resource is the capability every domain implements. All methods take
// already-resolved arguments and the shared *service.Service; none of them
// touch cobra, stdout or Bubble Tea — the tui package wraps the blocking
// calls in commands.
type Resource interface {
	// Kind is the stable identifier used in the command palette and routing
	// (e.g. "device"). Lowercase, singular.
	Kind() string
	// Title is the human heading shown in breadcrumbs and the palette
	// (e.g. "Devices").
	Title() string
	// Columns describes the list view layout.
	Columns() []Column
	// Fetch returns one page of rows plus the total count.
	Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]Row, int, error)
	// Actions returns the operations available from the list (may be empty
	// for read-only resources).
	Actions() []Action
	// Execute runs the action identified by actionKey against the row id
	// (empty for TargetsAll actions). args carries any values collected by the
	// action's Form (nil otherwise). Returns a human-readable success message
	// or an error.
	Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error)
}

// Describer is an optional capability: a Resource that can render a richer
// detail view than its list row. Resources without it fall back to showing
// their row cells as fields.
type Describer interface {
	Describe(ctx context.Context, svc *service.Service, org, id string) ([]Section, error)
}

// FormOptioner is an optional capability for a Resource whose form actions need
// option lists resolved at runtime (e.g. the snippets attachable to a template,
// or the OUs of an org). The app calls FormOptions when opening a form that has
// a FormField with OptionsFrom set, passing the action key, the selected row id
// (empty for TargetsAll actions) and the field's OptionsFrom token; the returned
// slice becomes that field's selectable options.
type FormOptioner interface {
	FormOptions(ctx context.Context, svc *service.Service, org, id, actionKey, optionsFrom string) ([]string, error)
}

// Navigator is an optional capability for a Resource whose rows drill into child
// resources. The app calls Navigate when a Nav action fires, passing the
// selected row id and the action's Nav token, and pushes a list screen over the
// returned child Resource. ok=false surfaces a "cannot open" message.
type Navigator interface {
	Navigate(org, id, nav string) (Resource, bool)
}

// Registry holds the registered resources in a stable display order.
type Registry struct {
	order  []Resource
	byKind map[string]Resource
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{byKind: map[string]Resource{}}
}

// Register adds a resource. A later registration with the same Kind replaces
// the earlier one but keeps its position in the order.
func (r *Registry) Register(res Resource) {
	if _, exists := r.byKind[res.Kind()]; !exists {
		r.order = append(r.order, res)
	}
	r.byKind[res.Kind()] = res
}

// Get returns the resource for a kind.
func (r *Registry) Get(kind string) (Resource, bool) {
	res, ok := r.byKind[kind]
	return res, ok
}

// All returns the registered resources in display order.
func (r *Registry) All() []Resource {
	out := make([]Resource, len(r.order))
	copy(out, r.order)
	return out
}

// Titles returns the resource titles in display order (for the palette).
func (r *Registry) Titles() []string {
	out := make([]string, 0, len(r.order))
	for _, res := range r.order {
		out = append(out, res.Title())
	}
	return out
}
